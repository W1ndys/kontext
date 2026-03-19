package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "kontext",
	Short: "AI 原生的上下文编译器，面向 AI 协作开发",
	Long:  "Kontext 将项目知识编译为高质量的 Markdown Prompt 文档，供大模型直接消费，提升 AI 辅助编程的准确性和效率。",
}

func init() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(packCmd)
	rootCmd.AddCommand(configCmd)
}

// Execute 是 CLI 的入口函数，由 main.go 调用。
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
