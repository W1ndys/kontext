package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	scanFlag   bool
	freshFlag  bool
	resumeFlag bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化 .kontext/ 目录 / Initialize .kontext/ directory",
	Long: `初始化 .kontext/ 目录并写入配置文件。

交互式初始化（默认）：
  kontext init
  - 输入项目描述启动 AI 交互式生成
  - 直接回车使用静态模板

自动扫描项目源码并生成：
  kontext init --scan
  kontext init --scan --fresh    # 忽略缓存，强制从头开始
  kontext init --scan --resume   # 强制使用缓存继续（不询问）

---

Initialize the .kontext/ directory and write configuration files.

Interactive initialization (default):
  kontext init
  - Enter project description for AI interactive generation
  - Press Enter directly to use static templates

Auto-scan project source code and generate:
  kontext init --scan
  kontext init --scan --fresh    # Ignore cache, start from scratch
  kontext init --scan --resume   # Force resume from cache (no prompt)`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// flag 有效性校验
		if !scanFlag && (freshFlag || resumeFlag) {
			return fmt.Errorf("--fresh 和 --resume 仅在 --scan 模式下有效")
		}
		if scanFlag {
			return runScanInit()
		}
		return runInteractiveInit()
	},
}

func init() {
	initCmd.Flags().BoolVar(&scanFlag, "scan", false, "自动扫描项目源码生成配置 / Auto-scan project source code to generate config")
	initCmd.Flags().BoolVar(&freshFlag, "fresh", false, "忽略缓存，强制从头开始 / Ignore cache, start from scratch")
	initCmd.Flags().BoolVar(&resumeFlag, "resume", false, "强制使用缓存继续 / Force resume from cache")
}
