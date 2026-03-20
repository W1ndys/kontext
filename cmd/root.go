package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "kontext",
	Short: "AI 原生的上下文编译器 / AI-native context compiler for AI-assisted development",
	Long: `Kontext 将项目知识编译为高质量的 Markdown Prompt 文档，供大模型直接消费，提升 AI 辅助编程的准确性和效率。

Kontext compiles project knowledge into high-quality Markdown prompt documents for LLM consumption, improving the accuracy and efficiency of AI-assisted programming.`,
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(packCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(configCmd)
}

// Execute 是 CLI 的入口函数，由 main.go 调用。
func Execute() {
	// 初始化 cobra 默认的 help 命令，并覆盖其描述为中英双语
	rootCmd.InitDefaultHelpCmd()
	for _, c := range rootCmd.Commands() {
		switch c.Name() {
		case "help":
			c.Short = "查看命令帮助 / Help about any command"
		}
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
