package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/w1ndys/kontext/internal/config"
	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/generator"
	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/templates"
)

var scanFlag bool

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

---

Initialize the .kontext/ directory and write configuration files.

Interactive initialization (default):
  kontext init
  - Enter project description for AI interactive generation
  - Press Enter directly to use static templates

Auto-scan project source code and generate:
  kontext init --scan`,
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

	// ===== 阶段 1：扫描目录树 =====
	fmt.Println("📁 阶段 1/6：扫描项目目录...")
	projectDir := "."
	allFiles, err := fileutil.ScanDirectoryTree(projectDir, 5)
	if err != nil {
		return fmt.Errorf("扫描项目目录失败: %w", err)
	}
	printProgress(len(allFiles), len(allFiles), "扫描文件")
	fmt.Printf("\n   发现 %d 个文件\n\n", len(allFiles))

	// ===== 阶段 2：LLM 智能识别关键文件 =====
	fmt.Println("🧠 阶段 2/6：AI 分析目录结构，识别关键文件...")

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

	// ===== 阶段 3：读取配置/依赖文件 =====
	fmt.Println("📄 阶段 3/6：读取配置/依赖文件...")
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

	// ===== 阶段 4：提取源码概要 =====
	fmt.Println("📝 阶段 4/6：提取源码文件概要...")
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

	// ===== 阶段 5：LLM 选择重点文件 =====
	fmt.Println("🎯 阶段 5/6：AI 分析概要，选择重点文件...")
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

	// ===== 阶段 6：读取重点文件 + LLM 生成配置 =====
	fmt.Println("🤖 阶段 6/6：读取重点文件并生成配置...")
	fmt.Println("   （此步骤可能需要 30~60 秒，请耐心等待）")

	// 读取重点文件全文（最多 200 行）
	keyFileContents := make(map[string]string)
	var readKeyFiles []string
	for _, f := range selected.KeyFiles {
		content, readErr := readFirstNLines(filepath.Join(projectDir, f), 200)
		if readErr == nil {
			keyFileContents[f] = content
			readKeyFiles = append(readKeyFiles, f)
		}
	}
	fmt.Printf("   读取 %d 个重点文件内容\n", len(readKeyFiles))
	printFileList(readKeyFiles, 8)

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

	// 渲染 prompt
	userMsg, err := generator.RenderTemplate(templates.InitScanUser, map[string]interface{}{
		"DirectoryTree":      treeStr,
		"ConfigFiles":        configFiles,
		"KeyFileContents":    keyFileContents,
		"OtherFileSummaries": otherSummaries,
	})
	if err != nil {
		return fmt.Errorf("渲染扫描模板失败: %w", err)
	}

	// 启动加载动画
	done3 := make(chan struct{})
	genStart := time.Now()
	go spinnerAnimation(done3, genStart, []string{"分析项目结构", "识别技术栈", "生成配置文件", "校验输出格式"})

	generated, err := generator.GenerateStructuredYAML(client, templates.InitScanSystem, userMsg)
	close(done3)
	if err != nil {
		fmt.Println()
		return err
	}

	genElapsed := time.Since(genStart).Seconds()
	fmt.Printf("\r   ✓ LLM 生成完成 (耗时 %.1f 秒)\n\n", genElapsed)

	// 校验并写入
	return generator.WriteGeneratedYAML(generated)
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
