package cmd

import (
	"log/slog"
	"strings"

	"github.com/spf13/cobra"
	"github.com/w1ndys/kontext/internal/logging"
)

const (
	commandPathConfig     = "kontext config"
	commandPathConfigSet  = "kontext config set"
	commandPathConfigGet  = "kontext config get"
	commandPathConfigList = "kontext config list"
	commandPathInit       = "kontext init"
	commandPathPack       = "kontext pack"
	commandPathUpdate     = "kontext update"
	commandPathValidate   = "kontext validate"
)

// namedLogger 根据命令路径字符串获取命名日志器
func namedLogger(commandPath string) *slog.Logger {
	return logging.CommandLogger(commandPath)
}

// 判断是否跳过 help/complete 等命令的生命周期日志
func shouldSkipCommandLifecycleLog(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}

	switch cmd.Name() {
	case "help", "__complete", "__completeNoDesc":
		return true
	}

	return false
}

// 计算文本行数
func countLines(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

// 判断配置键是否为敏感信息（如 api_key）
func isSensitiveConfigKey(key string) bool {
	return strings.EqualFold(strings.TrimSpace(key), "llm.api_key")
}
