package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/w1ndys/kontext/internal/config"
	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/internal/packer"
)

var (
	packFromFile string
	packNoRefine bool
	packOutput   string
)

var packCmd = &cobra.Command{
	Use:   `pack "<任务描述/task description>"`,
	Short: "将项目上下文打包为 Markdown Prompt 文档 / Pack project context into a structured Markdown prompt document",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := namedLogger(commandPathPack)

		task, source, err := resolvePackTask(args)
		if err != nil {
			logger.Error("resolve pack task failed",
				"error", err,
				"from_file", packFromFile,
				"arg_count", len(args),
			)
			return err
		}
		logger.Info("pack started",
			"task_source", source,
			"task_length", len(task),
			"task_lines", countLines(task),
			"disable_refine", packNoRefine,
		)

		kontextDir := defaultKontextDir
		projectDir := defaultProjectDir

		cfg, err := config.Load()
		if err != nil {
			logger.Error("load llm config failed", "error", err)
			return fmt.Errorf("加载 LLM 配置失败: %w", err)
		}

		llmCfg := cfg.ToLLMConfig()
		logger.Info("llm config loaded",
			"base_url", llmCfg.BaseURL,
			"model", llmCfg.Model,
		)
		fmt.Printf("使用 LLM: %s (模型: %s)\n", llmCfg.BaseURL, llmCfg.Model)

		client, err := llm.NewClient(llmCfg)
		if err != nil {
			logger.Error("create llm client failed", "error", err)
			return err
		}

		engine := packer.NewEngine(client, kontextDir, projectDir)
		engine.DisableRefine = packNoRefine
		engine.OutputPath = packOutput
		engine.OnProgress = func(stage, total int, msg string) {
			fmt.Fprintf(os.Stderr, "[%d/%d] %s\n", stage, total, msg)
		}

		fmt.Fprintf(os.Stderr, "正在为任务打包上下文...\n")
		outPath, err := engine.Pack(task)
		if err != nil {
			logger.Error("pack failed",
				"error", err,
				"task_source", source,
			)
			return fmt.Errorf("打包失败: %w", err)
		}

		logger.Info("pack completed",
			"output_path", outPath,
			"task_source", source,
		)
		fmt.Printf("Prompt 文档已保存至: %s\n", outPath)
		return nil
	},
}

func init() {
	packCmd.Flags().StringVarP(&packFromFile, "from-file", "f", "", "从文件读取任务描述 / Read task description from file")
	packCmd.Flags().BoolVar(&packNoRefine, "no-refine", false, "跳过 LLM 精筛，只使用关键词匹配 / Skip LLM-based context refinement")
	packCmd.Flags().StringVarP(&packOutput, "output", "o", "", "指定输出文件路径 / Specify output file path")
}

// 解析任务输入来源（参数/文件/stdin/交互式），返回任务文本和来源标识
func resolvePackTask(args []string) (string, string, error) {
	if packFromFile != "" && len(args) > 0 {
		return "", "", fmt.Errorf("--from-file 与位置参数互斥，请只提供一种任务输入方式")
	}

	if packFromFile == "" && len(args) == 0 {
		task, err := readTaskFromPrompt()
		if err != nil {
			return "", "", err
		}
		return task, "prompt", nil
	}

	if packFromFile == "" {
		task := strings.TrimSpace(args[0])
		if task == "" {
			task, err := readTaskFromPrompt()
			if err != nil {
				return "", "", err
			}
			return task, "prompt", nil
		}
		return task, "arg", nil
	}

	data, err := readTaskInput(packFromFile)
	if err != nil {
		return "", "", fmt.Errorf("读取任务文件失败: %w", err)
	}
	task := cleanTaskContent(data)
	if task == "" {
		return "", "", fmt.Errorf("任务文件内容为空")
	}

	source := "file"
	if packFromFile == "-" {
		source = "stdin"
	}
	return task, source, nil
}

// 从文件路径或 stdin 读取任务内容
func readTaskInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(bufio.NewReader(os.Stdin))
	}
	return fileutil.ReadFile(path)
}

// 清理任务文本（去除 BOM 和首尾空白）
func cleanTaskContent(data []byte) string {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	return strings.TrimSpace(string(data))
}

// 通过临时文件和 $EDITOR 交互式获取任务描述
func readTaskFromPrompt() (string, error) {
	tmpFile, err := os.CreateTemp(".", ".kontext_task_*.md")
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// 写入提示注释
	hint := "<!-- 请在此处输入任务描述，保存并关闭编辑器即可。本注释行会被自动忽略。 -->\n"
	if _, err := tmpFile.WriteString(hint); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("写入临时文件失败: %w", err)
	}
	tmpFile.Close()

	// 检测编辑器：$EDITOR > code --wait > vi
	editor, editorArgs := detectEditor()

	fmt.Fprintf(os.Stderr, "正在打开编辑器 (%s)，请输入任务描述...\n", editor)

	cmdArgs := append(editorArgs, tmpPath)
	cmd := exec.Command(editor, cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("编辑器退出异常: %w", err)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("读取临时文件失败: %w", err)
	}

	// 去除 HTML 注释行
	content := stripHTMLComments(string(data))
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("任务描述不能为空")
	}

	return content, nil
}

// stripHTMLComments 去除文本中的 HTML 注释（<!-- ... -->）
func stripHTMLComments(s string) string {
	re := regexp.MustCompile(`(?s)<!--.*?-->`)
	return re.ReplaceAllString(s, "")
}

// detectEditor 检测可用的编辑器，返回命令名和额外参数。
// 优先级：$EDITOR > code --wait > vi (Unix) / notepad (Windows)
func detectEditor() (string, []string) {
	// 优先检测 VS Code
	if path, err := exec.LookPath("code"); err == nil {
		return path, []string{"--wait"}
	}
	// 用户自定义编辑器
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor, nil
	}
	// 按系统降级
	if runtime.GOOS == "windows" {
		return "notepad", nil
	}
	return "vi", nil
}
