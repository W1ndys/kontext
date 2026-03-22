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

func commandLogger(cmd *cobra.Command) *slog.Logger {
	if cmd == nil {
		return logging.Default()
	}
	return logging.CommandLogger(cmd.CommandPath())
}

func namedLogger(commandPath string) *slog.Logger {
	return logging.CommandLogger(commandPath)
}

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

func countLines(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

func isSensitiveConfigKey(key string) bool {
	return strings.EqualFold(strings.TrimSpace(key), "llm.api_key")
}
