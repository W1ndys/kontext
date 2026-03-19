package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/w1ndys/kontext/internal/schema"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "校验 .kontext/ 目录下的 YAML 配置文件",
	RunE: func(cmd *cobra.Command, args []string) error {
		kontextDir := ".kontext"

		errs := schema.ValidateBundle(kontextDir)
		if len(errs) == 0 {
			fmt.Println("所有 .kontext/ 配置文件校验通过。")
			return nil
		}

		fmt.Printf("发现 %d 个校验错误：\n", len(errs))
		for i, err := range errs {
			fmt.Printf("  %d. %s\n", i+1, err)
		}
		return fmt.Errorf("校验失败，共 %d 个错误", len(errs))
	},
}
