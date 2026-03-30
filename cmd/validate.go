package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/w1ndys/kontext/internal/schema"
	"github.com/w1ndys/kontext/internal/ui"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "校验 .kontext/ 目录下的 YAML 配置文件 / Validate YAML config files in .kontext/ directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := namedLogger(commandPathValidate)
		kontextDir := defaultKontextDir
		logger.Info("validate started", "dir", kontextDir)

		errs := schema.ValidateBundle(kontextDir)
		if len(errs) == 0 {
			logger.Info("validate succeeded", "dir", kontextDir)
			ui.Success("所有 .kontext/ 配置文件校验通过。")
			return nil
		}

		logger.Warn("validate failed",
			"dir", kontextDir,
			"error_count", len(errs),
		)
		ui.Error("发现 %d 个校验错误：", len(errs))
		for i, err := range errs {
			ui.Error("  %d. %s", i+1, err)
		}
		return fmt.Errorf("校验失败，共 %d 个错误", len(errs))
	},
}
