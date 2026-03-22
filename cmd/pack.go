package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
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

		kontextDir := ".kontext"
		projectDir := "."

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
}

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

func readTaskInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(bufio.NewReader(os.Stdin))
	}
	return fileutil.ReadFile(path)
}

func cleanTaskContent(data []byte) string {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	return strings.TrimSpace(string(data))
}

func readTaskFromPrompt() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	var lines []string

	for {
		if len(lines) == 0 {
			fmt.Fprintln(os.Stderr, "请输入任务描述，输入空行结束:")
		}
		fmt.Fprint(os.Stderr, "> ")
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "" {
			if len(lines) > 0 {
				return strings.TrimSpace(strings.Join(lines, "\n")), nil
			}
			if err == io.EOF {
				return "", fmt.Errorf("任务描述不能为空")
			}
			continue
		}

		lines = append(lines, line)

		if err != nil {
			if err == io.EOF {
				return strings.TrimSpace(strings.Join(lines, "\n")), nil
			}
			return "", fmt.Errorf("读取任务描述失败: %w", err)
		}
	}
}
