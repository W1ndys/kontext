package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/w1ndys/kontext/internal/config"
	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/generator"
	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/internal/ui"
)

// runInteractiveInit 交互式初始化，询问用户项目描述。
func runInteractiveInit() error {
	logger := namedLogger(commandPathInit).With("mode", "interactive")
	logger.Info("interactive init started")

	// 检查是否已存在
	if fileutil.DirExists(defaultKontextDir) && fileutil.FileExists(filepath.Join(defaultKontextDir, "PROJECT_MANIFEST.json")) {
		logger.Info("existing kontext directory detected", "dir", defaultKontextDir)
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
				logger.Info("interactive init cancelled by user")
				fmt.Println("已取消。")
				return nil
			}
		} else {
			logger.Info("interactive init cancelled because overwrite prompt ended")
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
		logger.Info("static init selected from interactive flow")
		fmt.Println()
		fmt.Println("未输入描述，将使用默认模板...")
		return runStaticInitWithOverwrite()
	}

	logger.Info("ai init selected",
		"description_length", len(description),
		"description_lines", countLines(description),
	)
	return runAIInit(description)
}

// runAIInit 启动 AI 交互式初始化流程（由 runInteractiveInit 调用，已处理过目录检查）。
func runAIInit(description string) error {
	logger := namedLogger(commandPathInit).With("mode", "ai")
	logger.Info("ai init started",
		"description_length", len(description),
		"description_lines", countLines(description),
	)

	// 加载 LLM 配置
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load llm config failed", "error", err)
		return fmt.Errorf("读取 LLM 配置失败: %w", err)
	}

	if cfg.APIKey == "" {
		logger.Warn("ai init missing api key")
		return fmt.Errorf("AI 交互式初始化需要配置 API Key\n\n方式一：运行 kontext config 进行交互式配置\n方式二：设置环境变量 export KONTEXT_LLM_API_KEY=your-api-key")
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

	fmt.Printf("使用 LLM: %s (模型: %s)\n", llmCfg.BaseURL, llmCfg.Model)
	ui.Info("正在分析项目需求...")

	if err := generator.RunInteractiveInit(client, description); err != nil {
		logger.Error("ai init failed", "error", err)
		return err
	}

	logger.Info("ai init completed")
	return nil
}

// runStaticInitWithOverwrite 执行静态模板初始化（无目录检查，直接覆盖）。
func runStaticInitWithOverwrite() error {
	logger := namedLogger(commandPathInit).With("mode", "static")
	logger.Info("static init started", "dir", defaultKontextDir)

	// 创建目录结构
	dirs := []string{
		defaultKontextDir,
		filepath.Join(defaultKontextDir, "module_contracts"),
		filepath.Join(defaultKontextDir, "prompts"),
	}
	for _, d := range dirs {
		if err := fileutil.EnsureDir(d); err != nil {
			logger.Error("ensure static init directory failed", "path", d, "error", err)
			return fmt.Errorf("创建目录 %s 失败: %w", d, err)
		}
	}

	// 写入核心配置文件
	templateFiles := map[string]string{
		filepath.Join(defaultKontextDir, "PROJECT_MANIFEST.json"): defaultManifest,
		filepath.Join(defaultKontextDir, "ARCHITECTURE_MAP.json"): defaultArchitecture,
		filepath.Join(defaultKontextDir, "CONVENTIONS.json"):      defaultConventions,
	}

	for path, content := range templateFiles {
		if err := fileutil.WriteFile(path, []byte(content)); err != nil {
			logger.Error("write static template failed", "path", path, "error", err)
			return fmt.Errorf("写入 %s 失败: %w", path, err)
		}
		fmt.Printf("  已创建: %s\n", path)
	}

	// 写入默认模块契约文件
	contractFiles := map[string]string{
		filepath.Join(defaultKontextDir, "module_contracts", "example_CONTRACT.json"): defaultContract,
	}

	fmt.Println()
	fmt.Println("  模块契约:")
	for path, content := range contractFiles {
		if err := fileutil.WriteFile(path, []byte(content)); err != nil {
			logger.Error("write static contract failed", "path", path, "error", err)
			return fmt.Errorf("写入 %s 失败: %w", path, err)
		}
		fmt.Printf("    已创建: %s\n", path)
	}

	fmt.Println()
	ui.Success(".kontext/ 初始化完成！")
	fmt.Println("后续步骤：")
	fmt.Println("  1. 编辑 .kontext/PROJECT_MANIFEST.json 填写项目信息")
	fmt.Println("  2. 编辑 .kontext/ARCHITECTURE_MAP.json 填写架构信息")
	fmt.Println("  3. 编辑 .kontext/CONVENTIONS.json 填写编码规范")
	fmt.Println("  4. 为每个核心模块创建 .kontext/module_contracts/<模块名>_CONTRACT.json")
	fmt.Println("  5. 运行 'kontext validate' 校验配置是否正确")
	fmt.Println()
	fmt.Println("提示: 使用 'kontext init --scan' 可自动扫描项目源码生成完整配置")

	logger.Info("static init completed",
		"template_file_count", len(templateFiles),
		"contract_file_count", len(contractFiles),
	)

	return nil
}
