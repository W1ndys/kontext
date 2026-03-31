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
	"github.com/w1ndys/kontext/internal/schema"
	"github.com/w1ndys/kontext/internal/ui"
	"github.com/w1ndys/kontext/templates"
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
	tracker         *ui.Tracker
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
	if fileutil.DirExists(defaultKontextDir) && fileutil.FileExists(filepath.Join(defaultKontextDir, "PROJECT_MANIFEST.json")) {
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
	tracker := ui.NewTracker()
	tracker.Start()
	defer tracker.Stop()

	// ===== 阶段 1：扫描目录树 =====
	if startStage <= 1 {
		ui.Stage("📁 阶段 1/9：扫描项目目录...")
		data.allFiles, err = fileutil.ScanDirectoryTree(defaultProjectDir, defaultScanDepth)
		if err != nil {
			logger.Error("scan directory failed", "stage", 1, "error", err)
			return fmt.Errorf("扫描项目目录失败: %w", err)
		}
		printProgress(len(data.allFiles), len(data.allFiles), "扫描文件")
		ui.Success("   发现 %d 个文件", len(data.allFiles))
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
		ui.Stage("🧠 阶段 2/9：AI 分析目录结构，识别关键文件...")

		treeStr := strings.Join(data.allFiles, "\n")
		analyzeUserMsg, err := generator.RenderTemplate(templates.InitScanAnalyzeUser, map[string]interface{}{
			"DirectoryTree": treeStr,
		})
		if err != nil {
			logger.Error("render analyze template failed", "stage", 2, "error", err)
			return fmt.Errorf("渲染文件识别模板失败: %w", err)
		}

		analyzeTask := tracker.AddTask("分析目录结构，识别关键文件")
		analyzeStart := time.Now()

		analyzed, analyzeErr := generator.AnalyzeProjectFiles(client, templates.InitScanAnalyzeSystem, analyzeUserMsg)
		analyzeFallback := false
		if analyzeErr != nil {
			analyzeTask.Fail(analyzeErr)
			analyzeFallback = true
			logger.Warn("project file analysis failed; using local fallback",
				"stage", 2,
				"error", analyzeErr,
			)
			ui.Warn("   ⚠ AI 文件识别失败，回退到本地规则识别...")
			data.analyzed = localAnalyzeFiles(data.allFiles)
		} else {
			analyzeTask.Done()
			data.analyzed = analyzed
		}

		ui.Plain("   识别到 %d 个配置文件 + %d 个关键源码文件", len(data.analyzed.ConfigFiles), len(data.analyzed.SourceFiles))
		tracker.Interject(func() {
			printFileListWithTitle("配置文件", data.analyzed.ConfigFiles, 8)
			printFileListWithTitle("关键源码", data.analyzed.SourceFiles, 10)
			fmt.Println()
		})
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
		ui.Stage("📄 阶段 3/9：读取配置/依赖文件...")
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
		fmt.Fprint(ui.Writer(), "\r\033[K")
		ui.Success("   ✓ 成功读取 %d 个配置文件", len(data.configFiles))
		tracker.Interject(func() {
			printFileList(readConfigFiles, 10)
			fmt.Println()
		})
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
		ui.Stage("📝 阶段 4/9：提取源码文件概要...")
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
		fmt.Fprint(ui.Writer(), "\r\033[K")
		ui.Success("   ✓ 提取 %d 个文件概要", len(data.fileSummaries))
		tracker.Interject(func() {
			printFileList(extractedFiles, 10)
			fmt.Println()
		})
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
		ui.Stage("🎯 阶段 5/9：AI 分析概要，选择重点文件...")
		selectUserMsg, err := generator.RenderTemplate(templates.InitScanSelectUser, map[string]interface{}{
			"FileSummaries": data.fileSummaries,
		})
		if err != nil {
			logger.Error("render key-file selection template failed", "stage", 5, "error", err)
			return fmt.Errorf("渲染重点文件选择模板失败: %w", err)
		}

		selectTask := tracker.AddTask("分析概要，筛选重点文件")
		selectStart := time.Now()

		selected, selectErr := generator.SelectKeyFiles(client, templates.InitScanSelectSystem, selectUserMsg)
		selectFallback := false
		if selectErr != nil {
			selectTask.Fail(selectErr)
			selectFallback = true
			logger.Warn("key-file selection failed; using fallback",
				"stage", 5,
				"error", selectErr,
			)
			ui.Warn("   ⚠ AI 选择失败，使用全部文件...")
			maxFiles := len(data.analyzed.SourceFiles)
			if maxFiles > 10 {
				maxFiles = 10
			}
			data.selected = &generator.SelectedFiles{KeyFiles: data.analyzed.SourceFiles[:maxFiles]}
		} else {
			selectTask.Done()
			data.selected = selected
		}
		ui.Success("   ✓ 选择 %d 个重点文件深入分析", len(data.selected.KeyFiles))
		tracker.Interject(func() {
			printFileList(data.selected.KeyFiles, 10)
			fmt.Println()
		})

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
		tracker:         tracker,
	}

	successfulContractFiles, err := executeScanStages6to9(pctx)
	if err != nil {
		logger.Error("execute scan stages 6 to 9 failed", "error", err)
		return err
	}

	totalElapsed := time.Since(totalStart).Seconds()

	tracker.Stop()
	ui.Success("\n✅ .kontext/ 初始化完成！总耗时 %.1f 秒", totalElapsed)

	fmt.Println("已创建:")
	fmt.Printf("  %s\n", filepath.Join(defaultKontextDir, "PROJECT_MANIFEST.json"))
	fmt.Printf("  %s\n", filepath.Join(defaultKontextDir, "ARCHITECTURE_MAP.json"))
	fmt.Printf("  %s\n", filepath.Join(defaultKontextDir, "CONVENTIONS.json"))

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
		ui.Stage("🤖 阶段 6/9：生成项目清单...")

		manifestTask := ctx.tracker.AddTask("生成 PROJECT_MANIFEST.json")
		manifestStart := time.Now()

		manifestUserMsg := baseUserMsg + "\n\n请根据以上项目信息，只生成 PROJECT_MANIFEST.json 文件的内容。"
		var err error
		manifestContent, err = generator.GenerateSingleJSON(client, templates.InitScanManifestSystem, manifestUserMsg)
		if err != nil {
			manifestTask.Fail(err)
			logger.Error("generate manifest failed", "stage", 6, "error", err)
			return nil, fmt.Errorf("生成 PROJECT_MANIFEST.json 失败: %w", err)
		}

		if valErr := generator.ValidateJSON(manifestContent); valErr != nil {
			manifestTask.Fail(valErr)
			logger.Error("validate manifest json failed", "stage", 6, "error", valErr)
			return nil, fmt.Errorf("生成的 PROJECT_MANIFEST.json 不合法: %w", valErr)
		}

		manifestPath := filepath.Join(kontextDir, "PROJECT_MANIFEST.json")
		if err := fileutil.WriteFile(manifestPath, []byte(manifestContent)); err != nil {
			manifestTask.Fail(err)
			logger.Error("write manifest failed", "stage", 6, "path", manifestPath, "error", err)
			return nil, fmt.Errorf("写入 PROJECT_MANIFEST.json 失败: %w", err)
		}

		manifestTask.Done()
		logger.Info("scan stage completed",
			"stage", 6,
			"stage_name", "generate_manifest",
			"path", manifestPath,
			"duration_ms", time.Since(manifestStart).Milliseconds(),
		)

		_ = cache.UpdateGeneratedFile(cp, "PROJECT_MANIFEST.json", true)
		_ = cache.UpdateCheckpointStage(cp, 6)
	}

	// 如果从阶段 7+ 恢复，需要读取已生成的 manifest
	if manifestContent == "" {
		data, err := os.ReadFile(filepath.Join(kontextDir, "PROJECT_MANIFEST.json"))
		if err != nil {
			return nil, fmt.Errorf("读取已生成的 PROJECT_MANIFEST.json 失败: %w", err)
		}
		manifestContent = string(data)
	}

	// ===== 阶段 7/9：并行生成架构与规范 =====
	if startStage <= 7 {
		ui.Stage("🏗️  阶段 7/9：生成架构与规范... (并行)")

		archUserMsg := baseUserMsg + fmt.Sprintf("\n\n## 已生成的 PROJECT_MANIFEST.json（作为参考上下文）\n\n```json\n%s\n```\n\n请根据以上信息，只生成 ARCHITECTURE_MAP.json 文件的内容。不要生成其他文件。", manifestContent)
		convUserMsg := baseUserMsg + fmt.Sprintf("\n\n## 已生成的 PROJECT_MANIFEST.json（作为参考上下文）\n\n```json\n%s\n```\n\n请根据以上信息，只生成 CONVENTIONS.json 文件的内容。不要生成其他文件。", manifestContent)

		var convContent string
		var archErr, convErr error

		archTask := ctx.tracker.AddTask("生成 ARCHITECTURE_MAP.json")
		convTask := ctx.tracker.AddTask("生成 CONVENTIONS.json")

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			archContent, archErr = generator.GenerateSingleJSON(client, templates.InitScanArchitectureSystem, archUserMsg)
			if archErr != nil {
				archTask.Fail(archErr)
			} else {
				archTask.Done()
			}
		}()

		go func() {
			defer wg.Done()
			convContent, convErr = generator.GenerateSingleJSON(client, templates.InitScanConventionsSystem, convUserMsg)
			if convErr != nil {
				convTask.Fail(convErr)
			} else {
				convTask.Done()
			}
		}()

		wg.Wait()

		if archErr != nil {
			logger.Error("generate architecture map failed", "stage", 7, "error", archErr)
			return nil, fmt.Errorf("生成 ARCHITECTURE_MAP.json 失败: %w", archErr)
		}
		if convErr != nil {
			logger.Error("generate conventions failed", "stage", 7, "error", convErr)
			return nil, fmt.Errorf("生成 CONVENTIONS.json 失败: %w", convErr)
		}

		if valErr := generator.ValidateJSON(archContent); valErr != nil {
			logger.Error("validate architecture map json failed", "stage", 7, "error", valErr)
			return nil, fmt.Errorf("生成的 ARCHITECTURE_MAP.json 不合法: %w", valErr)
		}
		if valErr := generator.ValidateJSON(convContent); valErr != nil {
			logger.Error("validate conventions json failed", "stage", 7, "error", valErr)
			return nil, fmt.Errorf("生成的 CONVENTIONS.json 不合法: %w", valErr)
		}

		archPath := filepath.Join(kontextDir, "ARCHITECTURE_MAP.json")
		if err := fileutil.WriteFile(archPath, []byte(archContent)); err != nil {
			logger.Error("write architecture map failed", "stage", 7, "path", archPath, "error", err)
			return nil, fmt.Errorf("写入 ARCHITECTURE_MAP.json 失败: %w", err)
		}

		convPath := filepath.Join(kontextDir, "CONVENTIONS.json")
		if err := fileutil.WriteFile(convPath, []byte(convContent)); err != nil {
			logger.Error("write conventions failed", "stage", 7, "path", convPath, "error", err)
			return nil, fmt.Errorf("写入 CONVENTIONS.json 失败: %w", err)
		}
		logger.Info("scan stage completed",
			"stage", 7,
			"stage_name", "generate_architecture_and_conventions",
			"architecture_path", archPath,
			"conventions_path", convPath,
		)

		_ = cache.UpdateGeneratedFile(cp, "ARCHITECTURE_MAP.json", true)
		_ = cache.UpdateGeneratedFile(cp, "CONVENTIONS.json", true)
		_ = cache.UpdateCheckpointStage(cp, 7)
	}

	// 如果从阶段 8+ 恢复，需要读取已生成的 archContent
	if archContent == "" {
		data, err := os.ReadFile(filepath.Join(kontextDir, "ARCHITECTURE_MAP.json"))
		if err != nil {
			return nil, fmt.Errorf("读取已生成的 ARCHITECTURE_MAP.json 失败: %w", err)
		}
		archContent = string(data)
	}

	// ===== 阶段 8/9：生成模块依赖关系图 =====
	modules := fileutil.ExtractModulesFromArchAndFiles(archContent, allFiles)
	if startStage <= 8 {
		if len(modules) == 0 {
			logger.Info("scan stage skipped",
				"stage", 8,
				"stage_name", "generate_dependency_graph",
				"reason", "no_modules",
			)
			ui.Plain("🔗 阶段 8/9：未检测到模块，跳过依赖关系图生成")
		} else {
			ui.Stage("🔗 阶段 8/9：生成模块依赖关系图... (%d 个模块)", len(modules))

			depGraphUserMsg := baseUserMsg + fmt.Sprintf(
				"\n\n## 已生成的 ARCHITECTURE_MAP.json（作为参考上下文）\n\n```json\n%s\n```\n\n请为以下模块生成依赖关系图：%v",
				archContent, modules,
			)

			depTask := ctx.tracker.AddTask("生成模块依赖关系图")

			depGraph, depErr := generator.GenerateDependencyGraph(client, templates.InitScanDepgraphSystem, depGraphUserMsg)

			if depErr != nil {
				depTask.Fail(depErr)
				logger.Warn("generate dependency graph failed",
					"stage", 8,
					"module_count", len(modules),
					"error", depErr,
				)
				ui.Warn("   ⚠ 依赖关系图生成失败: %v", depErr)
				ui.Warn("   将跳过依赖关系约束，继续生成模块契约...")
			} else {
				depTask.Done()
				depGraphBytes, _ := json.MarshalIndent(depGraph, "", "  ")
				depGraphJSON = string(depGraphBytes)
				logger.Info("scan stage completed",
					"stage", 8,
					"stage_name", "generate_dependency_graph",
					"module_count", len(modules),
					"dep_graph_module_count", len(depGraph.Modules),
				)
			}
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
		ui.Stage("📦 阶段 9/9：生成模块契约... (%d 个模块并行)", len(modules))

		contractContext := fmt.Sprintf(
			"\n\n## 已生成的 PROJECT_MANIFEST.json（作为参考上下文）\n\n```json\n%s\n```\n\n## 已生成的 ARCHITECTURE_MAP.json（作为参考上下文）\n\n```json\n%s\n```",
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
				fmt.Sprintf("\n\n请只为模块 `%s` 生成一个 CONTRACT.json 文件。不要生成其他模块或其他类型的文件。", moduleName), nil
		}

		moduleContractDir := filepath.Join(kontextDir, "module_contracts")
		partialPath := func(moduleName string) string {
			return filepath.Join(moduleContractDir, schema.ContractFilename(moduleName)+".partial")
		}
		finalPath := func(moduleName string) string {
			return filepath.Join(moduleContractDir, schema.ContractFilename(moduleName))
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

		// 为每个模块创建 tracker 任务
		trackerTasks := make(map[string]*ui.Task)
		for _, mod := range modules {
			trackerTasks[mod] = ctx.tracker.AddTask(fmt.Sprintf("生成 %s", schema.ContractFilename(mod)))
		}

		saveFinalContract := func(moduleName, content string) error {
			normalized, err := schema.NormalizeContractJSON(content)
			if err != nil {
				return fmt.Errorf("JSON 校验失败: %w", err)
			}
			if err := fileutil.WriteFile(finalPath(moduleName), []byte(normalized)); err != nil {
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
				ctx.tracker.Interject(func() {
					fmt.Printf("   ⚠ 写入 %s 失败: %v\n", filepath.Base(partialPath(event.ModuleName)), err)
				})
				return
			}

			state.LastSavedAt = time.Now()
			state.LastSavedLen = len(snapshot)
			partialStates[event.ModuleName] = state
		}

		onProgress := func(result generator.ModuleContractResult) {
			printMu.Lock()
			t := trackerTasks[result.ModuleName]
			printMu.Unlock()

			if result.Error != nil {
				resultMu.Lock()
				failedModules[result.ModuleName] = true
				resultMu.Unlock()
				if t != nil {
					t.Fail(result.Error)
				}
				return
			}

			if err := saveFinalContract(result.ModuleName, result.Content); err != nil {
				resultMu.Lock()
				failedModules[result.ModuleName] = true
				resultMu.Unlock()
				if t != nil {
					t.Fail(err)
				}
				return
			}

			if t != nil {
				t.DoneWithLabel(fmt.Sprintf("%s (%.1f 秒)", schema.ContractFilename(result.ModuleName), result.Duration))
			}
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

			// 为重试模块创建新的 tracker 任务
			printMu.Lock()
			for _, mod := range retryModules {
				trackerTasks[mod] = ctx.tracker.AddTask(fmt.Sprintf("重试 %s", schema.ContractFilename(mod)))
			}
			printMu.Unlock()

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
				ctx.tracker.Interject(func() {
					ui.Warn("   ⚠ %d 个模块最终生成失败", len(finalFailures))
					for _, mod := range finalFailures {
						ui.Error("      - %s", mod)
					}
				})
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
		ui.Success("   模块契约生成完成 (%.1f 秒)", step9Elapsed)

		// 清理所有残留的 partial 文件（包括失败模块留下的临时产物）
		for _, mod := range modules {
			_ = os.Remove(partialPath(mod))
		}

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
