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
	Use:   "init [描述/description]",
	Short: "初始化 .kontext/ 目录（可选 AI 交互式生成） / Initialize .kontext/ directory (optional AI interactive generation)",
	Long: `初始化 .kontext/ 目录并写入配置文件。

无参数时写入静态模板：
  kontext init

提供项目描述时启动 AI 交互式初始化：
  kontext init "我想做一个博客系统"

自动扫描项目源码并生成：
  kontext init --scan

---

Initialize the .kontext/ directory and write configuration files.

Without arguments, write static templates:
  kontext init

With a project description, start AI interactive initialization:
  kontext init "I want to build a blog system"

Auto-scan project source code and generate:
  kontext init --scan`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if scanFlag {
			return runScanInit()
		}
		if len(args) == 1 {
			return runAIInit(args[0])
		}
		return runStaticInit()
	},
}

func init() {
	initCmd.Flags().BoolVar(&scanFlag, "scan", false, "自动扫描项目源码生成配置 / Auto-scan project source code to generate config")
}

// runAIInit 启动 AI 交互式初始化流程。
func runAIInit(description string) error {
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
	fmt.Println("📁 阶段 1/4：扫描项目目录...")
	projectDir := "."
	allFiles, err := fileutil.ScanDirectoryTree(projectDir, 5)
	if err != nil {
		return fmt.Errorf("扫描项目目录失败: %w", err)
	}
	printProgress(len(allFiles), len(allFiles), "扫描文件")
	fmt.Printf("   发现 %d 个文件\n\n", len(allFiles))

	// ===== 阶段 2：识别并读取配置文件 =====
	fmt.Println("📄 阶段 2/4：识别配置/依赖文件...")
	configFileNames := map[string]bool{
		"go.mod": true, "go.sum": false, "package.json": true, "tsconfig.json": true,
		"Cargo.toml": true, "pyproject.toml": true, "requirements.txt": true,
		"pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
		"Makefile": true, "Dockerfile": true, "docker-compose.yml": true,
		"docker-compose.yaml": true, ".gitignore": true, "CMakeLists.txt": true,
	}
	configFiles := make(map[string]string)
	configCount := 0
	for _, f := range allFiles {
		base := filepath.Base(f)
		if configFileNames[base] {
			configCount++
		}
	}
	processed := 0
	for _, f := range allFiles {
		base := filepath.Base(f)
		if configFileNames[base] {
			processed++
			printProgressWithFile(processed, configCount, "读取配置", f)
			fullPath := filepath.Join(projectDir, f)
			data, readErr := os.ReadFile(fullPath)
			if readErr == nil {
				configFiles[f] = string(data)
			}
		}
	}
	clearLine()
	fmt.Printf("   识别到 %d 个配置/依赖文件\n\n", len(configFiles))

	// ===== 阶段 3：读取源码文件 =====
	fmt.Println("💻 阶段 3/4：读取源码文件...")
	var sourceFiles []string
	for _, f := range allFiles {
		if _, isConfig := configFiles[f]; isConfig {
			continue
		}
		if isSourceFile(f) {
			sourceFiles = append(sourceFiles, f)
		}
	}
	if len(sourceFiles) > 30 {
		sourceFiles = sourceFiles[:30]
	}

	snippets := make(map[string]string, len(sourceFiles))
	for i, f := range sourceFiles {
		printProgressWithFile(i+1, len(sourceFiles), "读取源码", f)
		fullPath := filepath.Join(projectDir, f)
		content, err := readFirstNLines(fullPath, 50)
		if err == nil {
			snippets[f] = content
		}
	}
	clearLine()
	fmt.Printf("   读取 %d 个源码文件片段\n\n", len(snippets))

	// ===== 阶段 4：调用 LLM 生成 =====
	fmt.Println("🤖 阶段 4/4：调用 LLM 分析并生成配置...")
	fmt.Println("   （此步骤可能需要 30~60 秒，请耐心等待）")

	// 渲染 prompt
	treeStr := strings.Join(allFiles, "\n")
	userMsg, err := generator.RenderTemplate(templates.InitScanUser, map[string]interface{}{
		"DirectoryTree": treeStr,
		"ConfigFiles":   configFiles,
		"CodeSnippets":  snippets,
	})
	if err != nil {
		return fmt.Errorf("渲染扫描模板失败: %w", err)
	}

	// 启动加载动画
	done := make(chan struct{})
	startTime := time.Now()
	go func() {
		dots := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		phases := []string{"分析项目结构", "识别技术栈", "生成配置文件", "校验输出格式"}
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
	}()

	generated, err := generator.GenerateStructuredYAML(client, templates.InitScanSystem, userMsg)
	close(done)
	if err != nil {
		fmt.Println()
		return err
	}

	elapsed := time.Since(startTime).Seconds()
	fmt.Printf("\r   ✓ LLM 生成完成 (耗时 %.1f 秒)\n\n", elapsed)

	// 校验并写入
	return generator.WriteGeneratedYAML(generated)
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

// runStaticInit 执行原有的静态模板初始化。
func runStaticInit() error {
	kontextDir := ".kontext"

	if fileutil.DirExists(kontextDir) && fileutil.FileExists(filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml")) {
		fmt.Println(".kontext/ 已存在，跳过初始化。")
		fmt.Println()
		fmt.Println("如需重新生成，可使用以下方式（会提示是否覆盖）：")
		fmt.Println("  kontext init \"项目描述\"  - AI 交互式生成")
		fmt.Println("  kontext init --scan      - 自动扫描项目源码生成")
		return nil
	}

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

	// 写入默认模板文件
	templateFiles := map[string]string{
		filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml"): defaultManifest,
		filepath.Join(kontextDir, "ARCHITECTURE_MAP.yaml"): defaultArchitecture,
		filepath.Join(kontextDir, "CONVENTIONS.yaml"):      defaultConventions,
	}

	for path, content := range templateFiles {
		if fileutil.FileExists(path) {
			fmt.Printf("  跳过: %s (已存在)\n", path)
			continue
		}
		if err := fileutil.WriteFile(path, []byte(content)); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", path, err)
		}
		fmt.Printf("  已创建: %s\n", path)
	}

	fmt.Println("\n.kontext/ 初始化完成！")
	fmt.Println("后续步骤：")
	fmt.Println("  1. 编辑 .kontext/PROJECT_MANIFEST.yaml 填写项目信息")
	fmt.Println("  2. 编辑 .kontext/ARCHITECTURE_MAP.yaml 填写架构信息")
	fmt.Println("  3. 编辑 .kontext/CONVENTIONS.yaml 填写编码规范")
	fmt.Println("  4. 运行 'kontext validate' 校验配置是否正确")

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
