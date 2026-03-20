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

	"github.com/spf13/cobra"
	"github.com/w1ndys/kontext/internal/cache"
	"github.com/w1ndys/kontext/internal/config"
	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/generator"
	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/templates"
	"go.yaml.in/yaml/v4"
)

var (
	scanFlag   bool
	freshFlag  bool
	resumeFlag bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化 .kontext/ 目录 / Initialize .kontext/ directory",
	Long: `初始化 .kontext/ 目录并写入配置文件。

交互式初始化（默认）：
  kontext init
  - 输入项目描述启动 AI 交互式生成
  - 直接回车使用静态模板

自动扫描项目源码并生成：
  kontext init --scan
  kontext init --scan --fresh    # 忽略缓存，强制从头开始
  kontext init --scan --resume   # 强制使用缓存继续（不询问）

---

Initialize the .kontext/ directory and write configuration files.

Interactive initialization (default):
  kontext init
  - Enter project description for AI interactive generation
  - Press Enter directly to use static templates

Auto-scan project source code and generate:
  kontext init --scan
  kontext init --scan --fresh    # Ignore cache, start from scratch
  kontext init --scan --resume   # Force resume from cache (no prompt)`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if scanFlag {
			return runScanInit()
		}
		return runInteractiveInit()
	},
}

func init() {
	initCmd.Flags().BoolVar(&scanFlag, "scan", false, "自动扫描项目源码生成配置 / Auto-scan project source code to generate config")
	initCmd.Flags().BoolVar(&freshFlag, "fresh", false, "忽略缓存，强制从头开始 / Ignore cache, start from scratch")
	initCmd.Flags().BoolVar(&resumeFlag, "resume", false, "强制使用缓存继续 / Force resume from cache")
}

// runInteractiveInit 交互式初始化，询问用户项目描述。
func runInteractiveInit() error {
	kontextDir := ".kontext"

	// 检查是否已存在
	if fileutil.DirExists(kontextDir) && fileutil.FileExists(filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml")) {
		fmt.Println(".kontext/ 已存在。")
		fmt.Println()
		fmt.Println("如需重新生成，可使用以下方式：")
		fmt.Println("  kontext init --scan  - 自动扫描项目源码生成（会提示是否覆盖）")
		fmt.Println()
		fmt.Print("是否覆盖现有配置？[y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
			if answer != "y" && answer != "yes" {
				fmt.Println("已取消。")
				return nil
			}
		} else {
			return nil
		}
	}

	fmt.Println("Kontext 项目初始化")
	fmt.Println(strings.Repeat("-", 40))
	fmt.Println()
	fmt.Println("请输入项目描述，AI 将根据描述生成配置文件。")
	fmt.Println("（直接回车将使用默认模板）")
	fmt.Println()
	fmt.Print("项目描述: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	description := strings.TrimSpace(input)

	if description == "" {
		fmt.Println()
		fmt.Println("未输入描述，将使用默认模板...")
		return runStaticInitWithOverwrite()
	}

	return runAIInit(description)
}

// runAIInit 启动 AI 交互式初始化流程（由 runInteractiveInit 调用，已处理过目录检查）。
func runAIInit(description string) error {
	// 加载 LLM 配置
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("读取 LLM 配置失败: %w", err)
	}

	if cfg.APIKey == "" {
		return fmt.Errorf("AI 交互式初始化需要配置 API Key\n\n方式一：运行 kontext config 进行交互式配置\n方式二：设置环境变量 export KONTEXT_LLM_API_KEY=your-api-key")
	}

	llmCfg := cfg.ToLLMConfig()
	client, err := llm.NewClient(llmCfg)
	if err != nil {
		return fmt.Errorf("创建 LLM 客户端失败: %w", err)
	}

	fmt.Printf("使用 LLM: %s (模型: %s)\n", llmCfg.BaseURL, llmCfg.Model)
	fmt.Println("正在分析项目需求...")

	return generator.RunInteractiveInit(client, description)
}

// runScanInit 自动扫描项目源码并调用 LLM 生成 .kontext 配置。
func runScanInit() error {
	kontextDir := ".kontext"
	projectDir := "."
	scanDepth := 5

	// ===== 缓存检测与恢复 =====
	if freshFlag {
		// --fresh: 强制清除缓存
		_ = cache.ClearCache()
	} else {
		// 检查是否存在有效缓存
		valid, cp, err := cache.IsCheckpointValid(projectDir, scanDepth)
		if err != nil {
			fmt.Printf("   ⚠ 检查缓存失败: %v，将从头开始\n", err)
		} else if valid && cp != nil && cp.CurrentStage > 1 {
			// 存在有效缓存
			if resumeFlag {
				// --resume: 直接继续
				return runScanInitFromCheckpoint(cp)
			}
			// 询问用户
			fmt.Printf("发现未完成的扫描任务（已完成阶段 1-%d），是否继续？[Y/n] ", cp.CurrentStage-1)
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if answer == "" || answer == "y" || answer == "yes" {
					return runScanInitFromCheckpoint(cp)
				}
			}
			// 用户选择不继续，清除缓存从头开始
			_ = cache.ClearCache()
		}
	}

	// 检查是否已存在
	if fileutil.DirExists(kontextDir) && fileutil.FileExists(filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml")) {
		fmt.Print(".kontext/ 已存在，是否覆盖？[y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
			if answer != "y" && answer != "yes" {
				fmt.Println("已取消。")
				return nil
			}
		} else {
			return nil
		}
	}

	return runScanInitFresh()
}

// runScanInitFresh 从头开始执行扫描初始化流程，并在每个阶段保存缓存。
func runScanInitFresh() error {
	kontextDir := ".kontext"
	projectDir := "."
	scanDepth := 5

	// 初始化检查点
	projectHash, _ := cache.ComputeProjectHash(projectDir, scanDepth)
	cp := cache.NewCheckpoint(projectHash)
	_ = cache.SaveCheckpoint(cp)

	// 加载 LLM 配置
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("读取 LLM 配置失败: %w", err)
	}
	if cfg.APIKey == "" {
		return fmt.Errorf("扫描模式需要配置 API Key\n\n方式一：运行 kontext config 进行交互式配置\n方式二：设置环境变量 export KONTEXT_LLM_API_KEY=your-api-key")
	}

	llmCfg := cfg.ToLLMConfig()
	client, err := llm.NewClient(llmCfg)
	if err != nil {
		return fmt.Errorf("创建 LLM 客户端失败: %w", err)
	}

	fmt.Printf("使用 LLM: %s (模型: %s)\n\n", llmCfg.BaseURL, llmCfg.Model)

	totalStart := time.Now()

	// ===== 阶段 1：扫描目录树 =====
	fmt.Println("📁 阶段 1/9：扫描项目目录...")
	allFiles, err := fileutil.ScanDirectoryTree(projectDir, scanDepth)
	if err != nil {
		return fmt.Errorf("扫描项目目录失败: %w", err)
	}
	printProgress(len(allFiles), len(allFiles), "扫描文件")
	fmt.Printf("\n   发现 %d 个文件\n\n", len(allFiles))

	// 保存阶段 1 结果
	_ = cache.SaveStageResult(1, allFiles)
	_ = cache.UpdateCheckpointStage(cp, 1)

	// ===== 阶段 2：LLM 智能识别关键文件 =====
	fmt.Println("🧠 阶段 2/9：AI 分析目录结构，识别关键文件...")

	treeStr := strings.Join(allFiles, "\n")
	analyzeUserMsg, err := generator.RenderTemplate(templates.InitScanAnalyzeUser, map[string]interface{}{
		"DirectoryTree": treeStr,
	})
	if err != nil {
		return fmt.Errorf("渲染文件识别模板失败: %w", err)
	}

	// 启动加载动画
	done := make(chan struct{})
	analyzeStart := time.Now()
	go spinnerAnimation(done, analyzeStart, []string{"分析目录结构", "识别配置文件", "筛选核心源码"})

	analyzed, err := generator.AnalyzeProjectFiles(client, templates.InitScanAnalyzeSystem, analyzeUserMsg)
	close(done)
	if err != nil {
		fmt.Println()
		fmt.Println("   ⚠ AI 文件识别失败，回退到本地规则识别...")
		analyzed = localAnalyzeFiles(allFiles)
	} else {
		analyzeElapsed := time.Since(analyzeStart).Seconds()
		fmt.Printf("\r   ✓ AI 识别完成 (耗时 %.1f 秒)\n", analyzeElapsed)
	}

	fmt.Printf("   识别到 %d 个配置文件 + %d 个关键源码文件\n", len(analyzed.ConfigFiles), len(analyzed.SourceFiles))
	printFileListWithTitle("配置文件", analyzed.ConfigFiles, 8)
	printFileListWithTitle("关键源码", analyzed.SourceFiles, 10)
	fmt.Println()

	// 保存阶段 2 结果
	_ = cache.SaveStageResult(2, analyzed)
	_ = cache.UpdateCheckpointStage(cp, 2)

	// ===== 阶段 3：读取配置/依赖文件 =====
	fmt.Println("📄 阶段 3/9：读取配置/依赖文件...")
	configFiles := make(map[string]string)
	var readConfigFiles []string
	for i, f := range analyzed.ConfigFiles {
		printProgressWithFile(i+1, len(analyzed.ConfigFiles), "读取配置", f)
		fullPath := filepath.Join(projectDir, f)
		data, readErr := os.ReadFile(fullPath)
		if readErr == nil {
			configFiles[f] = string(data)
			readConfigFiles = append(readConfigFiles, f)
		}
	}
	clearLine()
	fmt.Printf("   ✓ 成功读取 %d 个配置文件\n", len(configFiles))
	printFileList(readConfigFiles, 10)
	fmt.Println()

	// 保存阶段 3 结果
	_ = cache.SaveStageResult(3, configFiles)
	_ = cache.UpdateCheckpointStage(cp, 3)

	// ===== 阶段 4：提取源码概要 =====
	fmt.Println("📝 阶段 4/9：提取源码文件概要...")
	fileSummaries := make(map[string]string)
	var extractedFiles []string
	for i, f := range analyzed.SourceFiles {
		printProgressWithFile(i+1, len(analyzed.SourceFiles), "提取概要", f)
		summary, extractErr := fileutil.ExtractFileSummary(filepath.Join(projectDir, f))
		if extractErr == nil {
			fileSummaries[f] = summary
			extractedFiles = append(extractedFiles, f)
		}
	}
	clearLine()
	fmt.Printf("   ✓ 提取 %d 个文件概要\n", len(fileSummaries))
	printFileList(extractedFiles, 10)
	fmt.Println()

	// 保存阶段 4 结果
	_ = cache.SaveStageResult(4, fileSummaries)
	_ = cache.UpdateCheckpointStage(cp, 4)

	// ===== 阶段 5：LLM 选择重点文件 =====
	fmt.Println("🎯 阶段 5/9：AI 分析概要，选择重点文件...")
	selectUserMsg, err := generator.RenderTemplate(templates.InitScanSelectUser, map[string]interface{}{
		"FileSummaries": fileSummaries,
	})
	if err != nil {
		return fmt.Errorf("渲染重点文件选择模板失败: %w", err)
	}

	done2 := make(chan struct{})
	selectStart := time.Now()
	go spinnerAnimation(done2, selectStart, []string{"分析函数签名", "评估文件重要性", "筛选重点文件"})

	selected, err := generator.SelectKeyFiles(client, templates.InitScanSelectSystem, selectUserMsg)
	close(done2)
	if err != nil {
		fmt.Println()
		fmt.Println("   ⚠ AI 选择失败，使用全部文件...")
		maxFiles := len(analyzed.SourceFiles)
		if maxFiles > 10 {
			maxFiles = 10
		}
		selected = &generator.SelectedFiles{KeyFiles: analyzed.SourceFiles[:maxFiles]}
	} else {
		selectElapsed := time.Since(selectStart).Seconds()
		fmt.Printf("\r   ✓ AI 选择完成 (耗时 %.1f 秒)\n", selectElapsed)
	}
	fmt.Printf("   ✓ 选择 %d 个重点文件深入分析\n", len(selected.KeyFiles))
	printFileList(selected.KeyFiles, 10)
	fmt.Println()

	// ===== 准备：读取重点文件内容，构建公共上下文 =====
	keyFileContents := make(map[string]string)
	var readKeyFiles []string
	for _, f := range selected.KeyFiles {
		content, readErr := readFirstNLines(filepath.Join(projectDir, f), 200)
		if readErr == nil {
			keyFileContents[f] = content
			readKeyFiles = append(readKeyFiles, f)
		}
	}

	// 保存阶段 5 结果（包含 selected 和 keyFileContents）
	_ = cache.SaveStageResult(5, selected)
	_ = cache.SaveStageResultPart(5, 1, keyFileContents)
	_ = cache.UpdateCheckpointStage(cp, 5)

	// 其他文件只保留概要
	otherSummaries := make(map[string]string)
	keySet := make(map[string]bool)
	for _, f := range selected.KeyFiles {
		keySet[f] = true
	}
	for f, summary := range fileSummaries {
		if !keySet[f] {
			otherSummaries[f] = summary
		}
	}

	// 构建公共用户消息（包含项目源码信息）
	baseUserMsg, err := generator.RenderTemplate(templates.InitScanUser, map[string]interface{}{
		"DirectoryTree":      treeStr,
		"ConfigFiles":        configFiles,
		"KeyFileContents":    keyFileContents,
		"OtherFileSummaries": otherSummaries,
	})
	if err != nil {
		return fmt.Errorf("渲染扫描模板失败: %w", err)
	}

	// 执行阶段 6-9
	pctx := &scanPipelineContext{
		client:          client,
		cp:              cp,
		kontextDir:      kontextDir,
		allFiles:        allFiles,
		configFiles:     configFiles,
		fileSummaries:   fileSummaries,
		keyFileContents: keyFileContents,
		otherSummaries:  otherSummaries,
		treeStr:         treeStr,
		baseUserMsg:     baseUserMsg,
		startStage:      6,
	}

	successfulContractFiles, err := executeScanStages6to9(pctx)
	if err != nil {
		return err
	}

	totalElapsed := time.Since(totalStart).Seconds()

	fmt.Printf("\n✅ .kontext/ 初始化完成！总耗时 %.1f 秒\n\n", totalElapsed)

	fmt.Println("已创建:")
	fmt.Printf("  %s\n", filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml"))
	fmt.Printf("  %s\n", filepath.Join(kontextDir, "ARCHITECTURE_MAP.yaml"))
	fmt.Printf("  %s\n", filepath.Join(kontextDir, "CONVENTIONS.yaml"))

	// 列出已创建的模块契约
	for _, path := range successfulContractFiles {
		fmt.Printf("  %s\n", path)
	}

	return nil
}

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

	// 创建 .kontext 目录结构
	dirs := []string{
		kontextDir,
		filepath.Join(kontextDir, "module_contracts"),
		filepath.Join(kontextDir, "prompts"),
	}
	for _, d := range dirs {
		if err := fileutil.EnsureDir(d); err != nil {
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
			fmt.Println()
			return nil, fmt.Errorf("生成 PROJECT_MANIFEST.yaml 失败: %w", err)
		}

		if valErr := generator.ValidateYAML(manifestContent); valErr != nil {
			return nil, fmt.Errorf("生成的 PROJECT_MANIFEST.yaml 不合法: %w", valErr)
		}

		manifestPath := filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml")
		if err := fileutil.WriteFile(manifestPath, []byte(manifestContent)); err != nil {
			return nil, fmt.Errorf("写入 PROJECT_MANIFEST.yaml 失败: %w", err)
		}

		manifestElapsed := time.Since(manifestStart).Seconds()
		fmt.Printf("\r   ✓ PROJECT_MANIFEST.yaml (%.1f 秒)\n\n", manifestElapsed)

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
			return nil, fmt.Errorf("生成 ARCHITECTURE_MAP.yaml 失败: %w", archErr)
		}
		if convErr != nil {
			return nil, fmt.Errorf("生成 CONVENTIONS.yaml 失败: %w", convErr)
		}

		if valErr := generator.ValidateYAML(archContent); valErr != nil {
			return nil, fmt.Errorf("生成的 ARCHITECTURE_MAP.yaml 不合法: %w", valErr)
		}
		if valErr := generator.ValidateYAML(convContent); valErr != nil {
			return nil, fmt.Errorf("生成的 CONVENTIONS.yaml 不合法: %w", valErr)
		}

		archPath := filepath.Join(kontextDir, "ARCHITECTURE_MAP.yaml")
		if err := fileutil.WriteFile(archPath, []byte(archContent)); err != nil {
			return nil, fmt.Errorf("写入 ARCHITECTURE_MAP.yaml 失败: %w", err)
		}
		fmt.Printf("   ✓ ARCHITECTURE_MAP.yaml (%.1f 秒)\n", archElapsed)

		convPath := filepath.Join(kontextDir, "CONVENTIONS.yaml")
		if err := fileutil.WriteFile(convPath, []byte(convContent)); err != nil {
			return nil, fmt.Errorf("写入 CONVENTIONS.yaml 失败: %w", err)
		}
		fmt.Printf("   ✓ CONVENTIONS.yaml (%.1f 秒)\n\n", convElapsed)

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
				fmt.Println()
				fmt.Printf("   ⚠ 依赖关系图生成失败: %v\n", depErr)
				fmt.Println("   将跳过依赖关系约束，继续生成模块契约...")
			} else {
				depGraphElapsed := time.Since(depGraphStart).Seconds()
				fmt.Printf("\r   ✓ 识别 %d 个模块的依赖关系 (%.1f 秒)\n", len(depGraph.Modules), depGraphElapsed)

				depGraphBytes, _ := json.MarshalIndent(depGraph, "", "  ")
				depGraphJSON = string(depGraphBytes)
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
	if len(modules) == 0 {
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
			var finalFailures []string
			for mod := range failedModules {
				if _, ok := successfulContracts[mod]; !ok {
					finalFailures = append(finalFailures, mod)
				}
			}
			sort.Strings(finalFailures)
			resultMu.Unlock()

			if len(finalFailures) > 0 {
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
	}

	// 保存阶段 9 检查点
	_ = cache.UpdateCheckpointStage(cp, 9)

	return successfulContractFiles, nil
}

// runScanInitFromCheckpoint 从缓存检查点恢复执行扫描初始化流程。
func runScanInitFromCheckpoint(cp *cache.Checkpoint) error {
	kontextDir := ".kontext"
	startStage := cp.CurrentStage

	fmt.Printf("\n从阶段 %d 继续...\n\n", startStage)

	// 加载 LLM 配置
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("读取 LLM 配置失败: %w", err)
	}
	if cfg.APIKey == "" {
		return fmt.Errorf("扫描模式需要配置 API Key\n\n方式一：运行 kontext config 进行交互式配置\n方式二：设置环境变量 export KONTEXT_LLM_API_KEY=your-api-key")
	}

	llmCfg := cfg.ToLLMConfig()
	client, err := llm.NewClient(llmCfg)
	if err != nil {
		return fmt.Errorf("创建 LLM 客户端失败: %w", err)
	}

	fmt.Printf("使用 LLM: %s (模型: %s)\n\n", llmCfg.BaseURL, llmCfg.Model)

	totalStart := time.Now()

	// 加载已完成阶段的缓存数据
	var allFiles []string
	var analyzed generator.AnalyzedFiles
	var configFiles map[string]string
	var fileSummaries map[string]string
	var selected generator.SelectedFiles
	var keyFileContents map[string]string

	if startStage > 1 {
		if err := cache.LoadStageResult(1, &allFiles); err != nil {
			return fmt.Errorf("加载阶段 1 缓存失败: %w", err)
		}
		fmt.Printf("   ✓ 已从缓存加载阶段 1 结果 (%d 个文件)\n", len(allFiles))
	}
	if startStage > 2 {
		if err := cache.LoadStageResult(2, &analyzed); err != nil {
			return fmt.Errorf("加载阶段 2 缓存失败: %w", err)
		}
		fmt.Printf("   ✓ 已从缓存加载阶段 2 结果 (%d 配置 + %d 源码)\n", len(analyzed.ConfigFiles), len(analyzed.SourceFiles))
	}
	if startStage > 3 {
		if err := cache.LoadStageResult(3, &configFiles); err != nil {
			return fmt.Errorf("加载阶段 3 缓存失败: %w", err)
		}
		fmt.Printf("   ✓ 已从缓存加载阶段 3 结果 (%d 个配置文件)\n", len(configFiles))
	}
	if startStage > 4 {
		if err := cache.LoadStageResult(4, &fileSummaries); err != nil {
			return fmt.Errorf("加载阶段 4 缓存失败: %w", err)
		}
		fmt.Printf("   ✓ 已从缓存加载阶段 4 结果 (%d 个文件概要)\n", len(fileSummaries))
	}
	if startStage > 5 {
		if err := cache.LoadStageResult(5, &selected); err != nil {
			return fmt.Errorf("加载阶段 5 缓存失败: %w", err)
		}
		// 加载重点文件内容
		if err := cache.LoadStageResultPart(5, 1, &keyFileContents); err != nil {
			return fmt.Errorf("加载阶段 5 附属数据缓存失败: %w", err)
		}
		fmt.Printf("   ✓ 已从缓存加载阶段 5 结果 (%d 个重点文件)\n", len(selected.KeyFiles))
	}

	// 加载阶段 8 的依赖图（如果存在）
	var depGraphJSON string
	if startStage > 8 {
		_ = cache.LoadStageResult(8, &depGraphJSON)
	}

	fmt.Println()

	// 如果恢复点在阶段 1-5，需要从该阶段重新执行
	if startStage <= 5 {
		return runScanInitResumeEarlyStages(client, cp, startStage, allFiles, &analyzed, configFiles, fileSummaries, &selected, keyFileContents)
	}

	// 恢复点在阶段 6+，需要重建派生数据
	treeStr := strings.Join(allFiles, "\n")

	// 构建 otherSummaries
	otherSummaries := make(map[string]string)
	keySet := make(map[string]bool)
	for _, f := range selected.KeyFiles {
		keySet[f] = true
	}
	for f, summary := range fileSummaries {
		if !keySet[f] {
			otherSummaries[f] = summary
		}
	}

	// 构建公共用户消息
	baseUserMsg, err := generator.RenderTemplate(templates.InitScanUser, map[string]interface{}{
		"DirectoryTree":      treeStr,
		"ConfigFiles":        configFiles,
		"KeyFileContents":    keyFileContents,
		"OtherFileSummaries": otherSummaries,
	})
	if err != nil {
		return fmt.Errorf("渲染扫描模板失败: %w", err)
	}

	// 执行阶段 6-9
	pctx := &scanPipelineContext{
		client:          client,
		cp:              cp,
		kontextDir:      kontextDir,
		allFiles:        allFiles,
		configFiles:     configFiles,
		fileSummaries:   fileSummaries,
		keyFileContents: keyFileContents,
		otherSummaries:  otherSummaries,
		treeStr:         treeStr,
		baseUserMsg:     baseUserMsg,
		startStage:      startStage,
		depGraphJSON:    depGraphJSON,
	}

	successfulContractFiles, err := executeScanStages6to9(pctx)
	if err != nil {
		return err
	}

	totalElapsed := time.Since(totalStart).Seconds()

	fmt.Printf("\n✅ .kontext/ 初始化完成！总耗时 %.1f 秒\n\n", totalElapsed)

	fmt.Println("已创建:")
	fmt.Printf("  %s\n", filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml"))
	fmt.Printf("  %s\n", filepath.Join(kontextDir, "ARCHITECTURE_MAP.yaml"))
	fmt.Printf("  %s\n", filepath.Join(kontextDir, "CONVENTIONS.yaml"))

	for _, path := range successfulContractFiles {
		fmt.Printf("  %s\n", path)
	}

	return nil
}

// runScanInitResumeEarlyStages 从阶段 1-5 的中断点恢复执行。
// 重新执行中断的阶段及后续所有阶段。
func runScanInitResumeEarlyStages(
	client llm.Client,
	cp *cache.Checkpoint,
	startStage int,
	allFiles []string,
	analyzed *generator.AnalyzedFiles,
	configFiles map[string]string,
	fileSummaries map[string]string,
	selected *generator.SelectedFiles,
	keyFileContents map[string]string,
) error {
	kontextDir := ".kontext"
	projectDir := "."
	scanDepth := 5

	totalStart := time.Now()

	// 阶段 1：扫描目录树
	if startStage <= 1 {
		fmt.Println("📁 阶段 1/9：扫描项目目录...")
		var err error
		allFiles, err = fileutil.ScanDirectoryTree(projectDir, scanDepth)
		if err != nil {
			return fmt.Errorf("扫描项目目录失败: %w", err)
		}
		printProgress(len(allFiles), len(allFiles), "扫描文件")
		fmt.Printf("\n   发现 %d 个文件\n\n", len(allFiles))

		_ = cache.SaveStageResult(1, allFiles)
		_ = cache.UpdateCheckpointStage(cp, 1)
	}

	// 阶段 2：AI 分析目录结构
	if startStage <= 2 {
		fmt.Println("🧠 阶段 2/9：AI 分析目录结构，识别关键文件...")

		treeStr := strings.Join(allFiles, "\n")
		analyzeUserMsg, err := generator.RenderTemplate(templates.InitScanAnalyzeUser, map[string]interface{}{
			"DirectoryTree": treeStr,
		})
		if err != nil {
			return fmt.Errorf("渲染文件识别模板失败: %w", err)
		}

		done := make(chan struct{})
		analyzeStart := time.Now()
		go spinnerAnimation(done, analyzeStart, []string{"分析目录结构", "识别配置文件", "筛选核心源码"})

		analyzedPtr, analyzeErr := generator.AnalyzeProjectFiles(client, templates.InitScanAnalyzeSystem, analyzeUserMsg)
		close(done)
		if analyzeErr != nil {
			fmt.Println()
			fmt.Println("   ⚠ AI 文件识别失败，回退到本地规则识别...")
			analyzed = localAnalyzeFiles(allFiles)
		} else {
			analyzeElapsed := time.Since(analyzeStart).Seconds()
			fmt.Printf("\r   ✓ AI 识别完成 (耗时 %.1f 秒)\n", analyzeElapsed)
			analyzed = analyzedPtr
		}

		fmt.Printf("   识别到 %d 个配置文件 + %d 个关键源码文件\n", len(analyzed.ConfigFiles), len(analyzed.SourceFiles))
		printFileListWithTitle("配置文件", analyzed.ConfigFiles, 8)
		printFileListWithTitle("关键源码", analyzed.SourceFiles, 10)
		fmt.Println()

		_ = cache.SaveStageResult(2, analyzed)
		_ = cache.UpdateCheckpointStage(cp, 2)
	}

	// 阶段 3：读取配置文件
	if startStage <= 3 {
		fmt.Println("📄 阶段 3/9：读取配置/依赖文件...")
		configFiles = make(map[string]string)
		var readConfigFiles []string
		for i, f := range analyzed.ConfigFiles {
			printProgressWithFile(i+1, len(analyzed.ConfigFiles), "读取配置", f)
			fullPath := filepath.Join(projectDir, f)
			data, readErr := os.ReadFile(fullPath)
			if readErr == nil {
				configFiles[f] = string(data)
				readConfigFiles = append(readConfigFiles, f)
			}
		}
		clearLine()
		fmt.Printf("   ✓ 成功读取 %d 个配置文件\n", len(configFiles))
		printFileList(readConfigFiles, 10)
		fmt.Println()

		_ = cache.SaveStageResult(3, configFiles)
		_ = cache.UpdateCheckpointStage(cp, 3)
	}

	// 阶段 4：提取源码概要
	if startStage <= 4 {
		fmt.Println("📝 阶段 4/9：提取源码文件概要...")
		fileSummaries = make(map[string]string)
		var extractedFiles []string
		for i, f := range analyzed.SourceFiles {
			printProgressWithFile(i+1, len(analyzed.SourceFiles), "提取概要", f)
			summary, extractErr := fileutil.ExtractFileSummary(filepath.Join(projectDir, f))
			if extractErr == nil {
				fileSummaries[f] = summary
				extractedFiles = append(extractedFiles, f)
			}
		}
		clearLine()
		fmt.Printf("   ✓ 提取 %d 个文件概要\n", len(fileSummaries))
		printFileList(extractedFiles, 10)
		fmt.Println()

		_ = cache.SaveStageResult(4, fileSummaries)
		_ = cache.UpdateCheckpointStage(cp, 4)
	}

	// 阶段 5：AI 选择重点文件
	if startStage <= 5 {
		fmt.Println("🎯 阶段 5/9：AI 分析概要，选择重点文件...")
		selectUserMsg, err := generator.RenderTemplate(templates.InitScanSelectUser, map[string]interface{}{
			"FileSummaries": fileSummaries,
		})
		if err != nil {
			return fmt.Errorf("渲染重点文件选择模板失败: %w", err)
		}

		done2 := make(chan struct{})
		selectStart := time.Now()
		go spinnerAnimation(done2, selectStart, []string{"分析函数签名", "评估文件重要性", "筛选重点文件"})

		selectedPtr, selectErr := generator.SelectKeyFiles(client, templates.InitScanSelectSystem, selectUserMsg)
		close(done2)
		if selectErr != nil {
			fmt.Println()
			fmt.Println("   ⚠ AI 选择失败，使用全部文件...")
			maxFiles := len(analyzed.SourceFiles)
			if maxFiles > 10 {
				maxFiles = 10
			}
			selected = &generator.SelectedFiles{KeyFiles: analyzed.SourceFiles[:maxFiles]}
		} else {
			selectElapsed := time.Since(selectStart).Seconds()
			fmt.Printf("\r   ✓ AI 选择完成 (耗时 %.1f 秒)\n", selectElapsed)
			selected = selectedPtr
		}
		fmt.Printf("   ✓ 选择 %d 个重点文件深入分析\n", len(selected.KeyFiles))
		printFileList(selected.KeyFiles, 10)
		fmt.Println()

		keyFileContents = make(map[string]string)
		for _, f := range selected.KeyFiles {
			content, readErr := readFirstNLines(filepath.Join(projectDir, f), 200)
			if readErr == nil {
				keyFileContents[f] = content
			}
		}

		_ = cache.SaveStageResult(5, selected)
		_ = cache.SaveStageResultPart(5, 1, keyFileContents)
		_ = cache.UpdateCheckpointStage(cp, 5)
	}

	// 构建派生数据
	treeStr := strings.Join(allFiles, "\n")

	otherSummaries := make(map[string]string)
	keySet := make(map[string]bool)
	for _, f := range selected.KeyFiles {
		keySet[f] = true
	}
	for f, summary := range fileSummaries {
		if !keySet[f] {
			otherSummaries[f] = summary
		}
	}

	baseUserMsg, err := generator.RenderTemplate(templates.InitScanUser, map[string]interface{}{
		"DirectoryTree":      treeStr,
		"ConfigFiles":        configFiles,
		"KeyFileContents":    keyFileContents,
		"OtherFileSummaries": otherSummaries,
	})
	if err != nil {
		return fmt.Errorf("渲染扫描模板失败: %w", err)
	}

	// 执行阶段 6-9
	pctx := &scanPipelineContext{
		client:          client,
		cp:              cp,
		kontextDir:      kontextDir,
		allFiles:        allFiles,
		configFiles:     configFiles,
		fileSummaries:   fileSummaries,
		keyFileContents: keyFileContents,
		otherSummaries:  otherSummaries,
		treeStr:         treeStr,
		baseUserMsg:     baseUserMsg,
		startStage:      6,
	}

	successfulContractFiles, execErr := executeScanStages6to9(pctx)
	if execErr != nil {
		return execErr
	}

	totalElapsed := time.Since(totalStart).Seconds()

	fmt.Printf("\n✅ .kontext/ 初始化完成！总耗时 %.1f 秒\n\n", totalElapsed)

	fmt.Println("已创建:")
	fmt.Printf("  %s\n", filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml"))
	fmt.Printf("  %s\n", filepath.Join(kontextDir, "ARCHITECTURE_MAP.yaml"))
	fmt.Printf("  %s\n", filepath.Join(kontextDir, "CONVENTIONS.yaml"))

	for _, path := range successfulContractFiles {
		fmt.Printf("  %s\n", path)
	}

	return nil
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
				// "internal/config" → "config"
				// "cmd" → "cmd"
				// "templates" → "templates"
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
// "internal/config" → "config"
// "cmd" → "cmd"
// "pkg/utils" → "utils"
func normalizeModuleName(pkg string) string {
	pkg = strings.TrimSpace(pkg)
	pkg = filepath.ToSlash(pkg)
	parts := strings.Split(pkg, "/")

	// 排除无意义的目录
	excluded := map[string]bool{"testdata": true, "vendor": true}

	// 取最后一级有意义的名称
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

		// internal/ 下的子目录作为模块
		if parts[0] == "internal" && len(parts) >= 2 {
			moduleSet[parts[1]] = true
		}

		// cmd/ 目录作为模块
		if parts[0] == "cmd" {
			moduleSet["cmd"] = true
		}

		// pkg/ 下的子目录作为模块
		if parts[0] == "pkg" && len(parts) >= 2 {
			moduleSet[parts[1]] = true
		}

		// templates/ 目录作为模块
		if parts[0] == "templates" {
			moduleSet["templates"] = true
		}
	}

	// 排除不需要的目录
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
		// 错误格式: "模块 xxx: ..."
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

// spinnerAnimation 显示旋转加载动画，phases 为轮换展示的阶段文案。
func spinnerAnimation(done <-chan struct{}, startTime time.Time, phases []string) {
	dots := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			clearLine()
			return
		case <-ticker.C:
			elapsed := time.Since(startTime).Seconds()
			phase := phases[int(elapsed/15)%len(phases)]
			fmt.Printf("\r   %s %s... (%.0f秒)", dots[i%len(dots)], phase, elapsed)
			i++
		}
	}
}

// localAnalyzeFiles 使用本地规则识别文件（作为 LLM 识别的回退方案）。
func localAnalyzeFiles(allFiles []string) *generator.AnalyzedFiles {
	configFileNames := map[string]bool{
		"go.mod": true, "go.sum": true, "package.json": true, "tsconfig.json": true,
		"Cargo.toml": true, "pyproject.toml": true, "requirements.txt": true,
		"pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
		"Makefile": true, "Dockerfile": true, "docker-compose.yml": true,
		"docker-compose.yaml": true, ".gitignore": true, "CMakeLists.txt": true,
		".eslintrc.json": true, ".prettierrc": true, "webpack.config.js": true,
		"vite.config.ts": true, "vite.config.js": true,
	}

	result := &generator.AnalyzedFiles{}
	configSet := make(map[string]bool)

	for _, f := range allFiles {
		base := filepath.Base(f)
		if configFileNames[base] {
			result.ConfigFiles = append(result.ConfigFiles, f)
			configSet[f] = true
		}
	}

	for _, f := range allFiles {
		if configSet[f] {
			continue
		}
		if isSourceFile(f) {
			result.SourceFiles = append(result.SourceFiles, f)
		}
		if len(result.SourceFiles) >= 30 {
			break
		}
	}

	return result
}

// printProgress 打印进度条
func printProgress(current, total int, label string) {
	width := 30
	percent := float64(current) / float64(total)
	filled := int(percent * float64(width))

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	fmt.Printf("\r   [%s] %3.0f%% %s", bar, percent*100, label)
}

// printProgressWithFile 打印带文件名的进度条
func printProgressWithFile(current, total int, label, filename string) {
	width := 20
	percent := float64(current) / float64(total)
	filled := int(percent * float64(width))

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)

	// 截断过长的文件名
	displayName := filename
	if len(displayName) > 35 {
		displayName = "..." + displayName[len(displayName)-32:]
	}

	fmt.Printf("\r   [%s] %3.0f%% %s: %-35s", bar, percent*100, label, displayName)
}

// clearLine 清除当前行
func clearLine() {
	fmt.Print("\r\033[K")
}

// printFileList 打印文件列表（带缩进和树形结构）
// maxShow 为最多显示的文件数，超过则显示省略提示
func printFileList(files []string, maxShow int) {
	if len(files) == 0 {
		return
	}

	showCount := len(files)
	if maxShow > 0 && showCount > maxShow {
		showCount = maxShow
	}

	for i := 0; i < showCount; i++ {
		prefix := "├──"
		if i == showCount-1 && (maxShow <= 0 || len(files) <= maxShow) {
			prefix = "└──"
		}
		fmt.Printf("      %s %s\n", prefix, files[i])
	}

	if maxShow > 0 && len(files) > maxShow {
		fmt.Printf("      └── ... 等 %d 个文件\n", len(files)-maxShow)
	}
}

// printFileListWithTitle 打印带标题的文件列表
func printFileListWithTitle(title string, files []string, maxShow int) {
	if len(files) == 0 {
		return
	}
	fmt.Printf("      %s (%d 个):\n", title, len(files))
	for i, f := range files {
		if maxShow > 0 && i >= maxShow {
			fmt.Printf("         ... 等 %d 个文件\n", len(files)-maxShow)
			break
		}
		fmt.Printf("         • %s\n", f)
	}
}

// readFirstNLines 读取文件的前 n 行
func readFirstNLines(path string, n int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() && len(lines) < n {
		lines = append(lines, scanner.Text())
	}

	return strings.Join(lines, "\n"), nil
}

// isSourceFile 判断文件是否为源码文件。
func isSourceFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	sourceExts := map[string]bool{
		".go": true, ".py": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
		".java": true, ".kt": true, ".rs": true, ".c": true, ".cpp": true, ".h": true,
		".cs": true, ".rb": true, ".php": true, ".swift": true, ".m": true,
		".scala": true, ".dart": true, ".lua": true, ".sh": true, ".bash": true,
		".vue": true, ".svelte": true,
	}
	return sourceExts[ext]
}

// runStaticInit 执行原有的静态模板初始化（带目录检查）。
func runStaticInit() error {
	kontextDir := ".kontext"

	if fileutil.DirExists(kontextDir) && fileutil.FileExists(filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml")) {
		fmt.Println(".kontext/ 已存在，跳过初始化。")
		fmt.Println()
		fmt.Println("如需重新生成，可使用以下方式：")
		fmt.Println("  kontext init        - 交互式初始化（会提示是否覆盖）")
		fmt.Println("  kontext init --scan - 自动扫描项目源码生成（会提示是否覆盖）")
		return nil
	}

	return runStaticInitWithOverwrite()
}

// runStaticInitWithOverwrite 执行静态模板初始化（无目录检查，直接覆盖）。
func runStaticInitWithOverwrite() error {
	kontextDir := ".kontext"

	// 创建目录结构
	dirs := []string{
		kontextDir,
		filepath.Join(kontextDir, "module_contracts"),
		filepath.Join(kontextDir, "prompts"),
	}
	for _, d := range dirs {
		if err := fileutil.EnsureDir(d); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", d, err)
		}
	}

	// 写入核心配置文件
	templateFiles := map[string]string{
		filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml"): defaultManifest,
		filepath.Join(kontextDir, "ARCHITECTURE_MAP.yaml"): defaultArchitecture,
		filepath.Join(kontextDir, "CONVENTIONS.yaml"):      defaultConventions,
	}

	for path, content := range templateFiles {
		if err := fileutil.WriteFile(path, []byte(content)); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", path, err)
		}
		fmt.Printf("  已创建: %s\n", path)
	}

	// 写入默认模块契约文件
	contractFiles := map[string]string{
		filepath.Join(kontextDir, "module_contracts", "example_CONTRACT.yaml"): defaultContract,
	}

	fmt.Println()
	fmt.Println("  模块契约:")
	for path, content := range contractFiles {
		if err := fileutil.WriteFile(path, []byte(content)); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", path, err)
		}
		fmt.Printf("    已创建: %s\n", path)
	}

	fmt.Println("\n.kontext/ 初始化完成！")
	fmt.Println("后续步骤：")
	fmt.Println("  1. 编辑 .kontext/PROJECT_MANIFEST.yaml 填写项目信息")
	fmt.Println("  2. 编辑 .kontext/ARCHITECTURE_MAP.yaml 填写架构信息")
	fmt.Println("  3. 编辑 .kontext/CONVENTIONS.yaml 填写编码规范")
	fmt.Println("  4. 为每个核心模块创建 .kontext/module_contracts/<模块名>_CONTRACT.yaml")
	fmt.Println("  5. 运行 'kontext validate' 校验配置是否正确")
	fmt.Println()
	fmt.Println("提示: 使用 'kontext init --scan' 可自动扫描项目源码生成完整配置")

	return nil
}

const defaultManifest = `# .kontext/PROJECT_MANIFEST.yaml
# 用途：AI 开发助手的首要上下文文件，建立项目全局理解

project:
  name: "my-project"
  one_line: "用一句话描述你的项目"
  type: "web_app"  # 可选: cli_tool, web_app, library, microservice

  business_context: |
    在这里描述项目的业务背景和目标。
    它解决什么问题？用户是谁？

  core_flows:
    - name: "主流程"
      steps: "步骤 1 → 步骤 2 → 步骤 3"
      entry_point: "cmd/main.go"

tech_stack:
  language: "Go 1.21+"
  # 在这里添加你的技术栈详情
  key_decisions:
    - decision: "关键架构决策"
      reason: "做出此决策的原因"
      constraint: "此决策带来的约束"

scale:
  estimated_files: "10-50"
  modules: "3"
  phase: "development"

status:
  completed_modules: []
  in_progress: []
  not_started: []
`

const defaultArchitecture = `# .kontext/ARCHITECTURE_MAP.yaml
# 用途：定义项目的分层架构和架构规则

layers:
  - name: "CLI 层"
    description: "命令行界面与用户交互"
    packages:
      - "cmd"

  - name: "核心层"
    description: "核心业务逻辑"
    packages:
      - "internal/core"

  - name: "基础设施层"
    description: "外部集成与工具库"
    packages:
      - "internal/infra"

rules:
  - rule: "CLI 层不得包含业务逻辑"
    reason: "关注点分离"

  - rule: "核心层不得依赖基础设施层"
    reason: "保持核心逻辑可移植和可测试"
`

const defaultConventions = `# .kontext/CONVENTIONS.yaml
# 用途：定义编码规范和 AI 协作规则

coding:
  - rule: "使用有描述性的变量名"
    example: "userCount 而不是 n"
  - rule: "函数体不超过 50 行"
    reason: "保持可读性"

error_handling:
  - rule: "错误必须包装上下文信息"
    example: 'fmt.Errorf("执行 X 操作: %w", err)'

forbidden:
  - rule: "禁止全局可变状态"
    reason: "会导致测试困难和竞态条件"

ai_rules:
  - rule: "修改代码前必须先阅读已有代码"
    reason: "先理解上下文再做变更"
  - rule: "严格遵守模块契约中定义的边界"
    reason: "维护架构完整性"
`

const defaultContract = `# .kontext/module_contracts/example_CONTRACT.yaml
# 用途：定义单个模块的职责边界和依赖关系
# 使用方法：为每个核心模块复制此模板，重命名为 <模块名>_CONTRACT.yaml

module:
  name: "example"
  path: "internal/example/"
  purpose: |
    在这里描述模块的核心职责。
    它负责哪些功能？解决什么问题？

owns:
  - "该模块负责的功能点 1"
  - "该模块负责的功能点 2"

not_responsible_for:
  - "该模块明确不负责的功能"
  - "应由其他模块处理的功能"

depends_on:
  - module: "其他模块名"
    reason: "为什么依赖这个模块"

public_interface:
  - name: "ExampleFunc"
    signature: "func ExampleFunc(param string) (Result, error)"
    description: "函数功能描述"

modification_rules:
  - rule: "修改该模块时必须遵守的规则"
    reason: "原因说明"
`
