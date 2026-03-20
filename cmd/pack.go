package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
		task, hint, err := resolvePackTask(args)
		if err != nil {
			return err
		}

		kontextDir := ".kontext"
		projectDir := "."

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("加载 LLM 配置失败: %w", err)
		}

		llmCfg := cfg.ToLLMConfig()
		fmt.Printf("使用 LLM: %s (模型: %s)\n", llmCfg.BaseURL, llmCfg.Model)

		client, err := llm.NewClient(llmCfg)
		if err != nil {
			return err
		}

		engine := packer.NewEngine(client, kontextDir, projectDir)
		engine.DisableRefine = packNoRefine
		engine.FilenameHint = hint
		engine.OnProgress = func(stage, total int, msg string) {
			fmt.Fprintf(os.Stderr, "[%d/%d] %s\n", stage, total, msg)
		}

		fmt.Fprintf(os.Stderr, "正在为任务打包上下文...\n")
		outPath, err := engine.Pack(task)
		if err != nil {
			return fmt.Errorf("打包失败: %w", err)
		}

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
		return "", "", fmt.Errorf("请提供任务描述，或使用 --from-file 指定任务文件")
	}

	if packFromFile == "" {
		task := strings.TrimSpace(args[0])
		if task == "" {
			return "", "", fmt.Errorf("任务描述不能为空")
		}
		return task, "", nil
	}

	data, err := readTaskInput(packFromFile)
	if err != nil {
		return "", "", fmt.Errorf("读取任务文件失败: %w", err)
	}
	task := cleanTaskContent(data)
	if task == "" {
		return "", "", fmt.Errorf("任务文件内容为空")
	}

	hint := strings.TrimSuffix(filepath.Base(packFromFile), filepath.Ext(packFromFile))
	if packFromFile == "-" {
		hint = "stdin"
	}
	return task, hint, nil
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
