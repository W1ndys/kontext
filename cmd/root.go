package cmd

import (
	"log/slog"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
	"github.com/w1ndys/kontext/internal/logging"
	"github.com/w1ndys/kontext/internal/ui"
)

// Version 在构建时通过 ldflags 注入，默认值为 dev。
// 若未通过 ldflags 注入，则尝试从 Go 模块的构建信息中读取版本号（支持 go install 场景）。
var Version = "dev"

func init() {
	if Version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			Version = info.Main.Version
		}
	}
}

const (
	defaultKontextDir = ".kontext"
	defaultProjectDir = "."
)

var (
	logLevel  string
	logFormat string
)

var rootCmd = &cobra.Command{
	Use:   "kontext",
	Short: "AI 原生的上下文编译器 / AI-native context compiler for AI-assisted development",
	Long: `Kontext 将项目知识编译为高质量的 Markdown Prompt 文档，供大模型直接消费，提升 AI 辅助编程的准确性和效率。

Kontext compiles project knowledge into high-quality Markdown prompt documents for LLM consumption, improving the accuracy and efficiency of AI-assisted programming.`,
	Version: Version,
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		level := logLevel
		if !cmd.Flags().Changed("log-level") {
			if envLevel := os.Getenv(logging.EnvLogLevel); envLevel != "" {
				level = envLevel
			}
		}

		format := logFormat
		if !cmd.Flags().Changed("log-format") {
			if envFormat := os.Getenv(logging.EnvLogFormat); envFormat != "" {
				format = envFormat
			}
		}

		logger, err := logging.Init(logging.Options{
			Level:  level,
			Format: format,
		})
		if err != nil {
			return err
		}

		if shouldSkipCommandLifecycleLog(cmd) {
			return nil
		}

		logger.Info("command started",
			"command", cmd.CommandPath(),
			"arg_count", len(args),
			"log_file", logging.CurrentLogFilePath(),
		)
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", logging.DefaultLevel, "日志级别 debug|info|warn|error / Log level")
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", logging.DefaultFormat, "日志格式 text|json / Log format")

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
		slog.Error("command failed", "error", err)
		ui.Error("%v", err)
		os.Exit(1)
	}
}
