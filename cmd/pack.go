package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/w1ndys/kontext/internal/config"
	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/internal/packer"
)

var packCmd = &cobra.Command{
	Use:   `pack "<任务描述>"`,
	Short: "将项目上下文打包为结构化的 Markdown Prompt 文档",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		task := args[0]
		kontextDir := ".kontext"
		projectDir := "."

		// 加载 LLM 配置
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("加载 LLM 配置失败: %w", err)
		}

		llmCfg := cfg.ToLLMConfig()
		fmt.Printf("使用 LLM: %s (模型: %s)\n", llmCfg.BaseURL, llmCfg.Model)

		// 创建 LLM 客户端
		client, err := llm.NewClient(llmCfg)
		if err != nil {
			return err
		}

		// 创建并运行 Pack 引擎
		engine := packer.NewEngine(client, kontextDir, projectDir)

		fmt.Printf("正在为任务打包上下文: %s\n", task)
		outPath, err := engine.Pack(task)
		if err != nil {
			return fmt.Errorf("打包失败: %w", err)
		}

		fmt.Printf("\nPrompt 文档已保存至: %s\n", outPath)
		return nil
	},
}
