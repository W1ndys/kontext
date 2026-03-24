package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/w1ndys/kontext/internal/cache"
	"github.com/w1ndys/kontext/internal/config"
	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/generator"
	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/templates"
	"go.yaml.in/yaml/v4"
)

const defaultScanDepth = 5

// scanPipelineContext 封装阶段 6-9 所需的所有上下文数据。
type scanPipelineContext struct {
	client          llm.Client
	cp              *cache.Checkpoint
	kontextDir      string
	allFiles        []string
	configFiles     map[string]string
	fileSummaries   map[string]string
	keyFileContents map[string]string
	otherSummaries  map[string]string
	treeStr         string
	baseUserMsg     string
	startStage      int
	depGraphJSON    string // 仅当从阶段 9 恢复时需要预加载
}

// scanStageData 保存阶段 1-5 的中间数据，供 fresh 与 resume 共用。
type scanStageData struct {
	allFiles        []string
	analyzed        *generator.AnalyzedFiles
	configFiles     map[string]string
	fileSummaries   map[string]string
	selected        *generator.SelectedFiles
	keyFileContents map[string]string
}

// runScanInit 自动扫描项目源码并调用 LLM 生成 .kontext 配置。
func runScanInit() error {
	logger := namedLogger(commandPathInit).With("mode", "scan")
	logger.Info("scan init requested",
		"fresh", freshFlag,
		"resume", resumeFlag,
	)

	// ===== 缓存检测与恢复 =====
	if freshFlag {
		// --fresh: 强制清除缓存
		if err := cache.ClearCache(); err != nil {
			logger.Warn("clear cache failed", "error", err)
		} else {
			logger.Info("scan cache cleared for fresh run")
		}
	} else {
		// 检查是否存在有效缓存
		valid, cp, err := cache.IsCheckpointValid(defaultProjectDir, defaultScanDepth)
		if err != nil {
			logger.Warn("checkpoint validation failed", "error", err)
			fmt.Printf("   ⚠ 检查缓存失败: %v，将从头开始\n", err)
		} else if valid && cp != nil && cp.CurrentStage > 1 {
			logger.Info("checkpoint detected",
				"resume_stage", cp.CurrentStage,
				"completed_stage_count", len(cp.CompletedStages),
			)
			// 存在有效缓存
			if resumeFlag {
				// --resume: 直接继续
				logger.Info("resuming scan from checkpoint due to flag", "resume_stage", cp.CurrentStage)
				return runScanInitFromCheckpoint(cp)
			}
			// 询问用户
			fmt.Printf("发现未完成的扫描任务（已完成阶段 1-%d），是否继续？[Y/n] ", cp.CurrentStage-1)
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if answer == "" || answer == "y" || answer == "yes" {
					logger.Info("resuming scan from checkpoint after confirmation", "resume_stage", cp.CurrentStage)
					return runScanInitFromCheckpoint(cp)
				}
			}
			// 用户选择不继续，清除缓存从头开始
			if err := cache.ClearCache(); err != nil {
				logger.Warn("clear cache after declined resume failed", "error", err)
			} else {
				logger.Info("checkpoint discarded; starting fresh scan")
			}
		}
	}

	// 检查是否已存在
	if fileutil.DirExists(defaultKontextDir) && fileutil.FileExists(filepath.Join(defaultKontextDir, "PROJECT_MANIFEST.yaml")) {
		logger.Info("existing kontext directory detected", "dir", defaultKontextDir)
		fmt.Print(".kontext/ 已存在，是否覆盖？[y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
			if answer != "y" && answer != "yes" {
				logger.Info("scan init cancelled by user")
				fmt.Println("已取消。")
				return nil
			}
		} else {
			logger.Info("scan init cancelled because overwrite prompt ended")
			return nil
		}
	}

	logger.Info("starting fresh scan init")

	// 初始化检查点
	projectHash, hashErr := cache.ComputeProjectHash(defaultProjectDir, defaultScanDepth)
	if hashErr != nil {
		logger.Warn("compute project hash failed", "error", hashErr)
	}
	cp := cache.NewCheckpoint(projectHash)
	if err := cache.SaveCheckpoint(cp); err != nil {
		logger.Warn("save initial checkpoint failed", "error", err)
	}

	return runScanPipeline(cp, 1, &scanStageData{})
}

// runScanInitFromCheckpoint 从缓存检查点恢复执行扫描初始化流程。
func runScanInitFromCheckpoint(cp *cache.Checkpoint) error {
	startStage := cp.CurrentStage
	logger := namedLogger(commandPathInit).With(
		"mode", "scan",
		"resume", true,
		"resume_stage", startStage,
	)
	logger.Info("scan init resumed from checkpoint",
		"completed_stage_count", len(cp.CompletedStages),
	)

	fmt.Printf("\n从阶段 %d 继续...\n\n", startStage)

	// 加载已完成阶段的缓存数据
	data := &scanStageData{}

	if startStage > 1 {
		if err := cache.LoadStageResult(1, &data.allFiles); err != nil {
			logger.Error("load stage 1 cache failed", "error", err)
			return fmt.Errorf("加载阶段 1 缓存失败: %w", err)
		}
		fmt.Printf("   ✓ 已从缓存加载阶段 1 结果 (%d 个文件)\n", len(data.allFiles))
	}
	if startStage > 2 {
		data.analyzed = &generator.AnalyzedFiles{}
		if err := cache.LoadStageResult(2, data.analyzed); err != nil {
			logger.Error("load stage 2 cache failed", "error", err)
			return fmt.Errorf("加载阶段 2 缓存失败: %w", err)
		}
		fmt.Printf("   ✓ 已从缓存加载阶段 2 结果 (%d 配置 + %d 源码)\n", len(data.analyzed.ConfigFiles), len(data.analyzed.SourceFiles))
	}
	if startStage > 3 {
		if err := cache.LoadStageResult(3, &data.configFiles); err != nil {
			logger.Error("load stage 3 cache failed", "error", err)
			return fmt.Errorf("加载阶段 3 缓存失败: %w", err)
		}
		fmt.Printf("   ✓ 已从缓存加载阶段 3 结果 (%d 个配置文件)\n", len(data.configFiles))
	}
	if startStage > 4 {
		if err := cache.LoadStageResult(4, &data.fileSummaries); err != nil {
			logger.Error("load stage 4 cache failed", "error", err)
			return fmt.Errorf("加载阶段 4 缓存失败: %w", err)
		}
		fmt.Printf("   ✓ 已从缓存加载阶段 4 结果 (%d 个文件概要)\n", len(data.fileSummaries))
	}
	if startStage > 5 {
		data.selected = &generator.SelectedFiles{}
		if err := cache.LoadStageResult(5, data.selected); err != nil {
			logger.Error("load stage 5 cache failed", "error", err)
			return fmt.Errorf("加载阶段 5 缓存失败: %w", err)
		}
		if err := cache.LoadStageResultPart(5, 1, &data.keyFileContents); err != nil {
			logger.Error("load stage 5 key-file cache failed", "error", err)
			return fmt.Errorf("加载阶段 5 附属数据缓存失败: %w", err)
		}
		fmt.Printf("   ✓ 已从缓存加载阶段 5 结果 (%d 个重点文件)\n", len(data.selected.KeyFiles))
	}

	logger.Info("checkpoint data loaded",
		"all_file_count", len(data.allFiles),
		"config_file_count", len(data.configFiles),
		"summary_file_count", len(data.fileSummaries),
		"key_file_count", len(data.selected.KeyFiles),
	)

	fmt.Println()

	return runScanPipeline(cp, startStage, data)
}

// runScanPipeline 执行完整的 scan 流水线（阶段 1-9），通过 startStage 控制起始阶段。
// 对于 fresh 执行，startStage=1 且 data 为空；对于 resume，startStage>1 且 data 包含缓存数据。
func runScanPipeline(cp *cache.Checkpoint, startStage int, data *scanStageData) error {
	logger := namedLogger(commandPathInit).With(
		"mode", "scan",
		"resume", startStage > 1,
		"start_stage", startStage,
	)

	// 加载 LLM 配置
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load llm config failed", "error", err)
		return fmt.Errorf("读取 LLM 配置失败: %w", err)
	}
	if cfg.APIKey == "" {
		logger.Warn("scan init missing api key")
		return fmt.Errorf("扫描模式需要配置 API Key\n\n方式一：运行 kontext config 进行交互式配置\n方式二：设置环境变量 export KONTEXT_LLM_API_KEY=your-api-key")
	}

	llmCfg := cfg.ToLLMConfig()
	logger.Info("llm config loaded",
		"base_url", llmCfg.BaseURL,
		"model", llmCfg.Model,
	)
	client, err := llm.NewClient(llmCfg)
	if err != nil {
		logger.Error("create llm client failed", "error", err)
		return fmt.Errorf("创建 LLM 客户端失败: %w", err)
	}

	fmt.Printf("使用 LLM: %s (模型: %s)\n\n", llmCfg.BaseURL, llmCfg.Model)

	totalStart := time.Now()

	// ===== 阶段 1：扫描目录树 =====
	if startStage <= 1 {
		fmt.Println("📁 阶段 1/9：扫描项目目录...")
		data.allFiles, err = fileutil.ScanDirectoryTree(defaultProjectDir, defaultScanDepth)
		if err != nil {
			logger.Error("scan directory failed", "stage", 1, "error", err)
			return fmt.Errorf("扫描项目目录失败: %w", err)
		}
		printProgress(len(data.allFiles), len(data.allFiles), "扫描文件")
		fmt.Printf("\n   发现 %d 个文件\n\n", len(data.allFiles))
		logger.Info("scan stage completed",
			"stage", 1,
			"stage_name", "scan_directory",
			"file_count", len(data.allFiles),
		)

		_ = cache.SaveStageResult(1, data.allFiles)
		_ = cache.UpdateCheckpointStage(cp, 1)
	}

	// ===== 阶段 2：LLM 智能识别关键文件 =====
	if startStage <= 2 {
		fmt.Println("🧠 阶段 2/9：AI 分析目录结构，识别关键文件...")

		treeStr := strings.Join(data.allFiles, "\n")
		analyzeUserMsg, err := generator.RenderTemplate(templates.InitScanAnalyzeUser, map[string]interface{}{
			"DirectoryTree": treeStr,
		})
		if err != nil {
			logger.Error("render analyze template failed", "stage", 2, "error", err)
			return fmt.Errorf("渲染文件识别模板失败: %w", err)
		}

		done := make(chan struct{})
		analyzeStart := time.Now()
		go spinnerAnimation(done, analyzeStart, []string{"分析目录结构", "识别配置文件", "筛选核心源码"})

		analyzed, analyzeErr := generator.AnalyzeProjectFiles(client, templates.InitScanAnalyzeSystem, analyzeUserMsg)
		close(done)
		analyzeFallback := false
		if analyzeErr != nil {
			analyzeFallback = true
			logger.Warn("project file analysis failed; using local fallback",
				"stage", 2,
				"error", analyzeErr,
			)
			fmt.Println()
			fmt.Println("   ⚠ AI 文件识别失败，回退到本地规则识别...")
			data.analyzed = localAnalyzeFiles(data.allFiles)
		} else {
			analyzeElapsed := time.Since(analyzeStart).Seconds()
			fmt.Printf("\r   ✓ AI 识别完成 (耗时 %.1f 秒)\n", analyzeElapsed)
			data.analyzed = analyzed
		}

		fmt.Printf("   识别到 %d 个配置文件 + %d 个关键源码文件\n", len(data.analyzed.ConfigFiles), len(data.analyzed.SourceFiles))
		printFileListWithTitle("配置文件", data.analyzed.ConfigFiles, 8)
		printFileListWithTitle("关键源码", data.analyzed.SourceFiles, 10)
		fmt.Println()
		logger.Info("scan stage completed",
			"stage", 2,
			"stage_name", "analyze_project_files",
			"config_file_count", len(data.analyzed.ConfigFiles),
			"source_file_count", len(data.analyzed.SourceFiles),
			"fallback", analyzeFallback,
			"duration_ms", time.Since(analyzeStart).Milliseconds(),
		)

		_ = cache.SaveStageResult(2, data.analyzed)
		_ = cache.UpdateCheckpointStage(cp, 2)
	}

	// ===== 阶段 3：读取配置/依赖文件 =====
	if startStage <= 3 {
		fmt.Println("📄 阶段 3/9：读取配置/依赖文件...")
		data.configFiles = make(map[string]string)
		var readConfigFiles []string
		for i, f := range data.analyzed.ConfigFiles {
			printProgressWithFile(i+1, len(data.analyzed.ConfigFiles), "读取配置", f)
			fullPath := filepath.Join(defaultProjectDir, f)
			fileData, readErr := os.ReadFile(fullPath)
			if readErr == nil {
				data.configFiles[f] = string(fileData)
				readConfigFiles = append(readConfigFiles, f)
			}
		}
		clearLine()
		fmt.Printf("   ✓ 成功读取 %d 个配置文件\n", len(data.configFiles))
		printFileList(readConfigFiles, 10)
		fmt.Println()
		logger.Info("scan stage completed",
			"stage", 3,
			"stage_name", "read_config_files",
			"config_file_count", len(data.configFiles),
		)

		_ = cache.SaveStageResult(3, data.configFiles)
		_ = cache.UpdateCheckpointStage(cp, 3)
	}

	// ===== 阶段 4：提取源码概要 =====
	if startStage <= 4 {
		fmt.Println("📝 阶段 4/9：提取源码文件概要...")
		data.fileSummaries = make(map[string]string)
		var extractedFiles []string
		for i, f := range data.analyzed.SourceFiles {
			printProgressWithFile(i+1, len(data.analyzed.SourceFiles), "提取概要", f)
			summary, extractErr := fileutil.ExtractFileSummary(filepath.Join(defaultProjectDir, f))
			if extractErr == nil {
				data.fileSummaries[f] = summary
				extractedFiles = append(extractedFiles, f)
			}
		}
		clearLine()
		fmt.Printf("   ✓ 提取 %d 个文件概要\n", len(data.fileSummaries))
		printFileList(extractedFiles, 10)
		fmt.Println()
		logger.Info("scan stage completed",
			"stage", 4,
			"stage_name", "extract_file_summaries",
			"summary_file_count", len(data.fileSummaries),
		)

		_ = cache.SaveStageResult(4, data.fileSummaries)
		_ = cache.UpdateCheckpointStage(cp, 4)
	}

	// ===== 阶段 5：LLM 选择重点文件 =====
	if startStage <= 5 {
		fmt.Println("🎯 阶段 5/9：AI 分析概要，选择重点文件...")
		selectUserMsg, err := generator.RenderTemplate(templates.InitScanSelectUser, map[string]interface{}{
			"FileSummaries": data.fileSummaries,
		})
		if err != nil {
			logger.Error("render key-file selection template failed", "stage", 5, "error", err)
			return fmt.Errorf("渲染重点文件选择模板失败: %w", err)
		}

		done2 := make(chan struct{})
		selectStart := time.Now()
		go spinnerAnimation(done2, selectStart, []string{"分析函数签名", "评估文件重要性", "筛选重点文件"})

		selected, selectErr := generator.SelectKeyFiles(client, templates.InitScanSelectSystem, selectUserMsg)
		close(done2)
		selectFallback := false
		if selectErr != nil {
			selectFallback = true
			logger.Warn("key-file selection failed; using fallback",
				"stage", 5,
				"error", selectErr,
			)
			fmt.Println()
			fmt.Println("   ⚠ AI 选择失败，使用全部文件...")
			maxFiles := len(data.analyzed.SourceFiles)
			if maxFiles > 10 {
				maxFiles = 10
			}
			data.selected = &generator.SelectedFiles{KeyFiles: data.analyzed.SourceFiles[:maxFiles]}
		} else {
			selectElapsed := time.Since(selectStart).Seconds()
			fmt.Printf("\r   ✓ AI 选择完成 (耗时 %.1f 秒)\n", selectElapsed)
			data.selected = selected
		}
		fmt.Printf("   ✓ 选择 %d 个重点文件深入分析\n", len(data.selected.KeyFiles))
		printFileList(data.selected.KeyFiles, 10)
		fmt.Println()

		// 读取重点文件内容
		data.keyFileContents = make(map[string]string)
		for _, f := range data.selected.KeyFiles {
			content, readErr := readFirstNLines(filepath.Join(defaultProjectDir, f), 200)
			if readErr == nil {
				data.keyFileContents[f] = content
			}
		}
		logger.Info("scan stage completed",
			"stage", 5,
			"stage_name", "select_key_files",
			"key_file_count", len(data.selected.KeyFiles),
			"read_key_file_count", len(data.keyFileContents),
			"fallback", selectFallback,
			"duration_ms", time.Since(selectStart).Milliseconds(),
		)

		_ = cache.SaveStageResult(5, data.selected)
		_ = cache.SaveStageResultPart(5, 1, data.keyFileContents)
		_ = cache.UpdateCheckpointStage(cp, 5)
	}

	// 构建派生数据
	treeStr := strings.Join(data.allFiles, "\n")

	otherSummaries := make(map[string]string)
	keySet := make(map[string]bool)
	for _, f := range data.selected.KeyFiles {
		keySet[f] = true
	}
	for f, summary := range data.fileSummaries {
		if !keySet[f] {
			otherSummaries[f] = summary
		}
	}

	baseUserMsg, err := generator.RenderTemplate(templates.InitScanUser, map[string]interface{}{
		"DirectoryTree":      treeStr,
		"ConfigFiles":        data.configFiles,
		"KeyFileContents":    data.keyFileContents,
		"OtherFileSummaries": otherSummaries,
	})
	if err != nil {
		logger.Error("render scan template failed", "error", err)
		return fmt.Errorf("渲染扫描模板失败: %w", err)
	}

	// 加载阶段 8 的依赖图（如果从 stage 9 恢复）
	var depGraphJSON string
	if startStage > 8 {
		_ = cache.LoadStageResult(8, &depGraphJSON)
	}

	// 执行阶段 6-9
	stage6Start := 6
	if startStage > 6 {
		stage6Start = startStage
	}

	pctx := &scanPipelineContext{
		client:          client,
		cp:              cp,
		kontextDir:      defaultKontextDir,
		allFiles:        data.allFiles,
		configFiles:     data.configFiles,
		fileSummaries:   data.fileSummaries,
		keyFileContents: data.keyFileContents,
		otherSummaries:  otherSummaries,
		treeStr:         treeStr,
		baseUserMsg:     baseUserMsg,
		startStage:      stage6Start,
		depGraphJSON:    depGraphJSON,
	}

	successfulContractFiles, err := executeScanStages6to9(pctx)
	if err != nil {
		logger.Error("execute scan stages 6 to 9 failed", "error", err)
		return err
	}

	totalElapsed := time.Since(totalStart).Seconds()

	fmt.Printf("\n✅ .kontext/ 初始化完成！总耗时 %.1f 秒\n\n", totalElapsed)

	fmt.Println("已创建:")
	fmt.Printf("  %s\n", filepath.Join(defaultKontextDir, "PROJECT_MANIFEST.yaml"))
	fmt.Printf("  %s\n", filepath.Join(defaultKontextDir, "ARCHITECTURE_MAP.yaml"))
	fmt.Printf("  %s\n", filepath.Join(defaultKontextDir, "CONVENTIONS.yaml"))

	for _, path := range successfulContractFiles {
		fmt.Printf("  %s\n", path)
	}

	logger.Info("scan init completed",
		"resume", startStage > 1,
		"duration_ms", time.Since(totalStart).Milliseconds(),
		"generated_contract_count", len(successfulContractFiles),
	)

	return nil
}

// executeScanStages6to9 执行阶段 6-9 的生成逻辑。
// startStage 指定从哪个阶段开始（6、7、8 或 9）。
func executeScanStages6to9(ctx *scanPipelineContext) ([]string, error) {
	kontextDir := ctx.kontextDir
	client := ctx.client
	cp := ctx.cp
	allFiles := ctx.allFiles
	configFiles := ctx.configFiles
	keyFileContents := ctx.keyFileContents
	otherSummaries := ctx.otherSummaries
	treeStr := ctx.treeStr
	baseUserMsg := ctx.baseUserMsg
	startStage := ctx.startStage
	logger := namedLogger(commandPathInit).With(
		"mode", "scan",
		"pipeline_start_stage", startStage,
	)

	// 创建 .kontext 目录结构
	dirs := []string{
		kontextDir,
		filepath.Join(kontextDir, "module_contracts"),
		filepath.Join(kontextDir, "prompts"),
	}
	for _, d := range dirs {
		if err := fileutil.EnsureDir(d); err != nil {
			logger.Error("ensure kontext directory failed", "path", d, "error", err)
			return nil, fmt.Errorf("创建目录 %s 失败: %w", d, err)
		}
	}

	var manifestContent, archContent string
	depGraphJSON := ctx.depGraphJSON

	// ===== 阶段 6/9：生成项目清单 (PROJECT_MANIFEST) =====
	if startStage <= 6 {
		fmt.Println("🤖 阶段 6/9：生成项目清单...")
		manifestStart := time.Now()

		done3 := make(chan struct{})
		go spinnerAnimation(done3, manifestStart, []string{"分析项目信息", "生成项目清单"})

		manifestUserMsg := baseUserMsg + "\n\n请根据以上项目信息，只生成 PROJECT_MANIFEST.yaml 文件的内容。"
		var err error
		manifestContent, err = generator.GenerateSingleYAML(client, templates.InitScanManifestSystem, manifestUserMsg)
		close(done3)
		if err != nil {
			logger.Error("generate manifest failed", "stage", 6, "error", err)
			fmt.Println()
			return nil, fmt.Errorf("生成 PROJECT_MANIFEST.yaml 失败: %w", err)
		}

		if valErr := generator.ValidateYAML(manifestContent); valErr != nil {
			logger.Error("validate manifest yaml failed", "stage", 6, "error", valErr)
			return nil, fmt.Errorf("生成的 PROJECT_MANIFEST.yaml 不合法: %w", valErr)
		}

		manifestPath := filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml")
		if err := fileutil.WriteFile(manifestPath, []byte(manifestContent)); err != nil {
			logger.Error("write manifest failed", "stage", 6, "path", manifestPath, "error", err)
			return nil, fmt.Errorf("写入 PROJECT_MANIFEST.yaml 失败: %w", err)
		}

		manifestElapsed := time.Since(manifestStart).Seconds()
		fmt.Printf("\r   ✓ PROJECT_MANIFEST.yaml (%.1f 秒)\n\n", manifestElapsed)
		logger.Info("scan stage completed",
			"stage", 6,
			"stage_name", "generate_manifest",
			"path", manifestPath,
			"duration_ms", time.Since(manifestStart).Milliseconds(),
		)

		_ = cache.UpdateGeneratedFile(cp, "PROJECT_MANIFEST.yaml", true)
		_ = cache.UpdateCheckpointStage(cp, 6)
	}

	// 如果从阶段 7+ 恢复，需要读取已生成的 manifest
	if manifestContent == "" {
		data, err := os.ReadFile(filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml"))
		if err != nil {
			return nil, fmt.Errorf("读取已生成的 PROJECT_MANIFEST.yaml 失败: %w", err)
		}
		manifestContent = string(data)
	}

	// ===== 阶段 7/9：并行生成架构与规范 =====
	if startStage <= 7 {
		fmt.Println("🏗️  阶段 7/9：生成架构与规范... (并行)")

		archUserMsg := baseUserMsg + fmt.Sprintf("\n\n## 已生成的 PROJECT_MANIFEST.yaml（作为参考上下文）\n\n```yaml\n%s\n```\n\n请根据以上信息，只生成 ARCHITECTURE_MAP.yaml 文件的内容。不要生成其他文件。", manifestContent)
		convUserMsg := baseUserMsg + fmt.Sprintf("\n\n## 已生成的 PROJECT_MANIFEST.yaml（作为参考上下文）\n\n```yaml\n%s\n```\n\n请根据以上信息，只生成 CONVENTIONS.yaml 文件的内容。不要生成其他文件。", manifestContent)

		var convContent string
		var archErr, convErr error
		var archElapsed, convElapsed float64

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			start := time.Now()
			archContent, archErr = generator.GenerateSingleYAML(client, templates.InitScanArchitectureSystem, archUserMsg)
			archElapsed = time.Since(start).Seconds()
		}()

		go func() {
			defer wg.Done()
			start := time.Now()
			convContent, convErr = generator.GenerateSingleYAML(client, templates.InitScanConventionsSystem, convUserMsg)
			convElapsed = time.Since(start).Seconds()
		}()

		done4 := make(chan struct{})
		step7Start := time.Now()
		go spinnerAnimation(done4, step7Start, []string{"生成架构图", "生成编码规范"})
		wg.Wait()
		close(done4)

		if archErr != nil {
			logger.Error("generate architecture map failed", "stage", 7, "error", archErr)
			return nil, fmt.Errorf("生成 ARCHITECTURE_MAP.yaml 失败: %w", archErr)
		}
		if convErr != nil {
			logger.Error("generate conventions failed", "stage", 7, "error", convErr)
			return nil, fmt.Errorf("生成 CONVENTIONS.yaml 失败: %w", convErr)
		}

		if valErr := generator.ValidateYAML(archContent); valErr != nil {
			logger.Error("validate architecture map yaml failed", "stage", 7, "error", valErr)
			return nil, fmt.Errorf("生成的 ARCHITECTURE_MAP.yaml 不合法: %w", valErr)
		}
		if valErr := generator.ValidateYAML(convContent); valErr != nil {
			logger.Error("validate conventions yaml failed", "stage", 7, "error", valErr)
			return nil, fmt.Errorf("生成的 CONVENTIONS.yaml 不合法: %w", valErr)
		}

		archPath := filepath.Join(kontextDir, "ARCHITECTURE_MAP.yaml")
		if err := fileutil.WriteFile(archPath, []byte(archContent)); err != nil {
			logger.Error("write architecture map failed", "stage", 7, "path", archPath, "error", err)
			return nil, fmt.Errorf("写入 ARCHITECTURE_MAP.yaml 失败: %w", err)
		}
		fmt.Printf("   ✓ ARCHITECTURE_MAP.yaml (%.1f 秒)\n", archElapsed)

		convPath := filepath.Join(kontextDir, "CONVENTIONS.yaml")
		if err := fileutil.WriteFile(convPath, []byte(convContent)); err != nil {
			logger.Error("write conventions failed", "stage", 7, "path", convPath, "error", err)
			return nil, fmt.Errorf("写入 CONVENTIONS.yaml 失败: %w", err)
		}
		fmt.Printf("   ✓ CONVENTIONS.yaml (%.1f 秒)\n\n", convElapsed)
		logger.Info("scan stage completed",
			"stage", 7,
			"stage_name", "generate_architecture_and_conventions",
			"architecture_path", archPath,
			"conventions_path", convPath,
			"architecture_duration_ms", int64(archElapsed*1000),
			"conventions_duration_ms", int64(convElapsed*1000),
		)

		_ = cache.UpdateGeneratedFile(cp, "ARCHITECTURE_MAP.yaml", true)
		_ = cache.UpdateGeneratedFile(cp, "CONVENTIONS.yaml", true)
		_ = cache.UpdateCheckpointStage(cp, 7)
	}

	// 如果从阶段 8+ 恢复，需要读取已生成的 archContent
	if archContent == "" {
		data, err := os.ReadFile(filepath.Join(kontextDir, "ARCHITECTURE_MAP.yaml"))
		if err != nil {
			return nil, fmt.Errorf("读取已生成的 ARCHITECTURE_MAP.yaml 失败: %w", err)
		}
		archContent = string(data)
	}

	// ===== 阶段 8/9：生成模块依赖关系图 =====
	modules := extractModulesFromArch(archContent, allFiles)
	if startStage <= 8 {
		if len(modules) == 0 {
			logger.Info("scan stage skipped",
				"stage", 8,
				"stage_name", "generate_dependency_graph",
				"reason", "no_modules",
			)
			fmt.Println("🔗 阶段 8/9：未检测到模块，跳过依赖关系图生成")
		} else {
			fmt.Printf("🔗 阶段 8/9：生成模块依赖关系图... (%d 个模块)\n", len(modules))

			depGraphUserMsg := baseUserMsg + fmt.Sprintf(
				"\n\n## 已生成的 ARCHITECTURE_MAP.yaml（作为参考上下文）\n\n```yaml\n%s\n```\n\n请为以下模块生成依赖关系图：%v",
				archContent, modules,
			)

			done6 := make(chan struct{})
			depGraphStart := time.Now()
			go spinnerAnimation(done6, depGraphStart, []string{"分析模块依赖", "构建依赖关系图"})

			depGraph, depErr := generator.GenerateDependencyGraph(client, templates.InitScanDepgraphSystem, depGraphUserMsg)
			close(done6)

			if depErr != nil {
				logger.Warn("generate dependency graph failed",
					"stage", 8,
					"module_count", len(modules),
					"error", depErr,
				)
				fmt.Println()
				fmt.Printf("   ⚠ 依赖关系图生成失败: %v\n", depErr)
				fmt.Println("   将跳过依赖关系约束，继续生成模块契约...")
			} else {
				depGraphElapsed := time.Since(depGraphStart).Seconds()
				fmt.Printf("\r   ✓ 识别 %d 个模块的依赖关系 (%.1f 秒)\n", len(depGraph.Modules), depGraphElapsed)

				depGraphBytes, _ := json.MarshalIndent(depGraph, "", "  ")
				depGraphJSON = string(depGraphBytes)
				logger.Info("scan stage completed",
					"stage", 8,
					"stage_name", "generate_dependency_graph",
					"module_count", len(modules),
					"dep_graph_module_count", len(depGraph.Modules),
					"duration_ms", time.Since(depGraphStart).Milliseconds(),
				)
			}
			fmt.Println()
		}

		if depGraphJSON != "" {
			_ = cache.SaveStageResult(8, depGraphJSON)
		}
		_ = cache.UpdateCheckpointStage(cp, 8)
	}

	// ===== 阶段 9/9：并行生成模块契约 =====
	var successfulContractFiles []string
	var retryCount int
	var finalFailures []string
	if len(modules) == 0 {
		logger.Info("scan stage skipped",
			"stage", 9,
			"stage_name", "generate_module_contracts",
			"reason", "no_modules",
		)
		fmt.Println("📦 阶段 9/9：未检测到模块，跳过模块契约生成")
	} else {
		fmt.Printf("📦 阶段 9/9：生成模块契约... (%d 个模块并行)\n", len(modules))

		contractContext := fmt.Sprintf(
			"\n\n## 已生成的 PROJECT_MANIFEST.yaml（作为参考上下文）\n\n```yaml\n%s\n```\n\n## 已生成的 ARCHITECTURE_MAP.yaml（作为参考上下文）\n\n```yaml\n%s\n```",
			manifestContent, archContent,
		)

		if depGraphJSON != "" {
			contractContext += fmt.Sprintf(
				"\n\n## 模块依赖关系图（必须遵守）\n\n```json\n%s\n```\n\n请确保生成的 CONTRACT 中的 depends_on 与上述依赖图保持一致。",
				depGraphJSON,
			)
		}

		userMsgGenerator := func(moduleName string) (string, error) {
			moduleKeyFiles := generator.FilterFilesByModule(keyFileContents, moduleName)
			moduleSummaries := generator.FilterFilesByModule(otherSummaries, moduleName)

			moduleUserMsg, err := generator.RenderTemplate(templates.InitScanUser, map[string]interface{}{
				"DirectoryTree":      treeStr,
				"ConfigFiles":        configFiles,
				"KeyFileContents":    moduleKeyFiles,
				"OtherFileSummaries": moduleSummaries,
			})
			if err != nil {
				return "", err
			}

			return moduleUserMsg + contractContext +
				fmt.Sprintf("\n\n请只为模块 `%s` 生成一个 CONTRACT.yaml 文件。不要生成其他模块或其他类型的文件。", moduleName), nil
		}

		moduleContractDir := filepath.Join(kontextDir, "module_contracts")
		partialPath := func(moduleName string) string {
			return filepath.Join(moduleContractDir, fmt.Sprintf("%s_CONTRACT.yaml.partial", moduleName))
		}
		finalPath := func(moduleName string) string {
			return filepath.Join(moduleContractDir, fmt.Sprintf("%s_CONTRACT.yaml", moduleName))
		}

		type partialSnapshotState struct {
			Attempt      int
			LastSavedAt  time.Time
			LastSavedLen int
		}

		const (
			partialSaveInterval = 500 * time.Millisecond
			partialSaveMinDelta = 256
		)

		var printMu sync.Mutex
		var partialMu sync.Mutex
		var resultMu sync.Mutex
		partialStates := make(map[string]partialSnapshotState)
		successfulContracts := make(map[string]string)
		failedModules := make(map[string]bool)

		saveFinalContract := func(moduleName, content string) error {
			if err := generator.ValidateYAML(content); err != nil {
				return fmt.Errorf("YAML 校验失败: %w", err)
			}
			if err := fileutil.WriteFile(finalPath(moduleName), []byte(content)); err != nil {
				return fmt.Errorf("写入正式文件失败: %w", err)
			}
			_ = os.Remove(partialPath(moduleName))

			resultMu.Lock()
			successfulContracts[moduleName] = finalPath(moduleName)
			delete(failedModules, moduleName)
			resultMu.Unlock()
			return nil
		}

		onStream := func(event generator.ModuleContractStreamEvent) {
			snapshot := event.Accumulated
			if event.Done && strings.TrimSpace(event.FinalContent) != "" {
				snapshot = event.FinalContent
			}
			if strings.TrimSpace(snapshot) == "" {
				return
			}

			partialMu.Lock()
			defer partialMu.Unlock()

			state := partialStates[event.ModuleName]
			if state.Attempt != event.Attempt {
				state = partialSnapshotState{Attempt: event.Attempt}
			}

			shouldSave := event.Done || event.Error != nil
			if !shouldSave {
				growth := len(snapshot) - state.LastSavedLen
				if state.LastSavedLen > 0 && growth < partialSaveMinDelta && time.Since(state.LastSavedAt) < partialSaveInterval {
					partialStates[event.ModuleName] = state
					return
				}
			}

			if err := fileutil.WriteFile(partialPath(event.ModuleName), []byte(snapshot)); err != nil {
				printMu.Lock()
				fmt.Printf("   ⚠ 写入 %s 失败: %v\n", filepath.Base(partialPath(event.ModuleName)), err)
				printMu.Unlock()
				return
			}

			state.LastSavedAt = time.Now()
			state.LastSavedLen = len(snapshot)
			partialStates[event.ModuleName] = state
		}

		onProgress := func(result generator.ModuleContractResult) {
			printMu.Lock()
			defer printMu.Unlock()

			if result.Error != nil {
				resultMu.Lock()
				failedModules[result.ModuleName] = true
				resultMu.Unlock()
				fmt.Printf("   ✗ %s_CONTRACT.yaml 失败: %v\n", result.ModuleName, result.Error)
				return
			}

			if err := saveFinalContract(result.ModuleName, result.Content); err != nil {
				resultMu.Lock()
				failedModules[result.ModuleName] = true
				resultMu.Unlock()
				fmt.Printf("   ✗ %s_CONTRACT.yaml 失败: %v\n", result.ModuleName, err)
				return
			}

			fmt.Printf("   ✓ %s_CONTRACT.yaml (%.1f 秒)\n", result.ModuleName, result.Duration)
		}

		step9Start := time.Now()

		_, contractErrors := generator.GenerateModuleContracts(
			client,
			templates.InitScanContractSystem,
			modules,
			userMsgGenerator,
			3,
			onStream,
			onProgress,
		)

		resultMu.Lock()
		for _, mod := range extractFailedModuleNames(contractErrors) {
			failedModules[mod] = true
		}
		var retryModules []string
		for mod := range failedModules {
			retryModules = append(retryModules, mod)
		}
		sort.Strings(retryModules)
		resultMu.Unlock()

		if len(retryModules) > 0 {
			retryCount = len(retryModules)
			fmt.Printf("\n   ⚠ %d 个模块失败，正在重试...\n", len(retryModules))

			_, retryErrors := generator.GenerateModuleContracts(
				client,
				templates.InitScanContractSystem,
				retryModules,
				userMsgGenerator,
				2,
				onStream,
				onProgress,
			)

			resultMu.Lock()
			for _, mod := range extractFailedModuleNames(retryErrors) {
				failedModules[mod] = true
			}
			computedFinalFailures := make([]string, 0, len(failedModules))
			for mod := range failedModules {
				if _, ok := successfulContracts[mod]; !ok {
					computedFinalFailures = append(computedFinalFailures, mod)
				}
			}
			sort.Strings(computedFinalFailures)
			finalFailures = computedFinalFailures
			resultMu.Unlock()

			if len(finalFailures) > 0 {
				logger.Warn("module contract generation completed with failures",
					"stage", 9,
					"failed_count", len(finalFailures),
				)
				fmt.Printf("   ⚠ %d 个模块最终生成失败\n", len(finalFailures))
				for _, mod := range finalFailures {
					fmt.Printf("      - %s\n", mod)
				}
			}
		}

		resultMu.Lock()
		var successModules []string
		for mod := range successfulContracts {
			successModules = append(successModules, mod)
		}
		sort.Strings(successModules)
		successfulContractFiles = make([]string, 0, len(successModules))
		for _, mod := range successModules {
			successfulContractFiles = append(successfulContractFiles, successfulContracts[mod])
		}
		resultMu.Unlock()

		step9Elapsed := time.Since(step9Start).Seconds()
		fmt.Printf("\n   模块契约生成完成 (%.1f 秒)\n", step9Elapsed)
		logger.Info("scan stage completed",
			"stage", 9,
			"stage_name", "generate_module_contracts",
			"module_count", len(modules),
			"successful_contract_count", len(successfulContractFiles),
			"retry_count", retryCount,
			"failed_count", len(finalFailures),
			"duration_ms", time.Since(step9Start).Milliseconds(),
		)
	}

	// 保存阶段 9 检查点
	_ = cache.UpdateCheckpointStage(cp, 9)

	return successfulContractFiles, nil
}

// archLayer 定义 ARCHITECTURE_MAP 中的层级结构
type archLayer struct {
	Name     string   `yaml:"name"`
	Packages []string `yaml:"packages"`
}

// archMap 定义 ARCHITECTURE_MAP 的解析结构
type archMap struct {
	Layers []archLayer `yaml:"layers"`
}

// extractModulesFromArch 从 ARCHITECTURE_MAP 的 YAML 内容中解析模块列表。
// 优先解析 ARCHITECTURE_MAP 的 layers.packages，解析失败时回退到目录规则扫描。
func extractModulesFromArch(archContent string, allFiles []string) []string {
	// 优先从 ARCHITECTURE_MAP 解析
	var arch archMap
	if err := yaml.Unmarshal([]byte(archContent), &arch); err == nil && len(arch.Layers) > 0 {
		moduleSet := make(map[string]bool)
		for _, layer := range arch.Layers {
			for _, pkg := range layer.Packages {
				name := normalizeModuleName(pkg)
				if name != "" {
					moduleSet[name] = true
				}
			}
		}

		if len(moduleSet) > 0 {
			var modules []string
			for mod := range moduleSet {
				modules = append(modules, mod)
			}
			sort.Strings(modules)
			return modules
		}
	}

	// 解析失败时回退到目录规则扫描
	return fallbackExtractModules(allFiles)
}

// normalizeModuleName 将包路径规范化为模块名。
func normalizeModuleName(pkg string) string {
	pkg = strings.TrimSpace(pkg)
	pkg = filepath.ToSlash(pkg)
	parts := strings.Split(pkg, "/")

	excluded := map[string]bool{"testdata": true, "vendor": true}

	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" && !excluded[parts[i]] {
			return parts[i]
		}
	}
	return ""
}

// fallbackExtractModules 使用目录规则扫描提取模块列表（作为回退方案）。
func fallbackExtractModules(allFiles []string) []string {
	moduleSet := make(map[string]bool)

	for _, f := range allFiles {
		parts := strings.Split(filepath.ToSlash(f), "/")
		if len(parts) < 2 {
			continue
		}

		if parts[0] == "internal" && len(parts) >= 2 {
			moduleSet[parts[1]] = true
		}
		if parts[0] == "cmd" {
			moduleSet["cmd"] = true
		}
		if parts[0] == "pkg" && len(parts) >= 2 {
			moduleSet[parts[1]] = true
		}
		if parts[0] == "templates" {
			moduleSet["templates"] = true
		}
	}

	delete(moduleSet, "testdata")
	delete(moduleSet, "vendor")

	var modules []string
	for mod := range moduleSet {
		modules = append(modules, mod)
	}
	sort.Strings(modules)

	return modules
}

// extractFailedModuleNames 从错误列表中提取失败的模块名。
func extractFailedModuleNames(errors []error) []string {
	var names []string
	for _, e := range errors {
		msg := e.Error()
		if strings.HasPrefix(msg, "模块 ") {
			parts := strings.SplitN(msg[len("模块 "):], ":", 2)
			if len(parts) > 0 {
				names = append(names, strings.TrimSpace(parts[0]))
			}
		}
	}
	return names
}
