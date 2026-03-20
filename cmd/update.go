package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/w1ndys/kontext/internal/config"
	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/internal/updater"
)

var (
	updateDryRun bool
	updateFile   string
	updateSince  string
	updateYes    bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "检测并更新 .kontext/ 物料 / Detect and update .kontext materials",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateUpdateFilter(updateFile); err != nil {
			return err
		}

		report, err := updater.DetectChanges(".kontext", ".", updateSince)
		if err != nil {
			return fmt.Errorf("检测变更失败: %w", err)
		}

		actions := updater.PlanUpdates(report, normalizedUpdateFilter(updateFile))
		if updateDryRun {
			printUpdateReport(report, actions)
			return nil
		}

		if len(actions) == 0 {
			fmt.Println("未检测到需要更新的物料。")
			return nil
		}

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

		selected := make([]updater.UpdateAction, 0, len(actions))
		for _, action := range actions {
			if updateYes || confirmUpdateAction(action) {
				selected = append(selected, action)
			}
		}
		if len(selected) == 0 {
			fmt.Println("未选择任何更新项。")
			return nil
		}

		executor := updater.NewExecutor(client, ".kontext", ".")
		updated, err := executor.Execute(report, selected)
		if err != nil {
			return fmt.Errorf("执行更新失败: %w", err)
		}

		fmt.Println("已更新以下物料：")
		for _, path := range updated {
			fmt.Printf("  %s\n", path)
		}
		return nil
	},
}

func init() {
	updateCmd.Flags().BoolVar(&updateDryRun, "dry-run", false, "只检测并打印变更报告 / Detect only without modifying files")
	updateCmd.Flags().StringVar(&updateFile, "file", "", "仅更新指定物料：manifest|architecture|contracts|all")
	updateCmd.Flags().StringVar(&updateSince, "since", "", "只分析指定 commit 之后的变更 / Analyze changes since commit")
	updateCmd.Flags().BoolVarP(&updateYes, "yes", "y", false, "跳过确认，直接执行更新 / Apply updates without confirmation")
}

func validateUpdateFilter(filter string) error {
	switch normalizedUpdateFilter(filter) {
	case "", "all", "manifest", "architecture", "contracts":
		return nil
	default:
		return fmt.Errorf("--file 仅支持 manifest、architecture、contracts、all")
	}
}

func normalizedUpdateFilter(filter string) string {
	return strings.ToLower(strings.TrimSpace(filter))
}

func confirmUpdateAction(action updater.UpdateAction) bool {
	fmt.Printf("即将更新 %s，原因：%s。是否继续？[y/N] ", action.Target, action.Reason)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "y" || answer == "yes"
}

func printUpdateReport(report *updater.ChangeReport, actions []updater.UpdateAction) {
	fmt.Println("=== Kontext 物料变更检测报告 ===")
	fmt.Println()

	fmt.Println("[目录结构变更]")
	if len(report.DirectoryChanges) == 0 {
		fmt.Println("  无")
	} else {
		for _, change := range report.DirectoryChanges {
			prefix := "~"
			if change.Type == "added" {
				prefix = "+"
			} else if change.Type == "removed" {
				prefix = "-"
			}
			fmt.Printf("  %s %s\n", prefix, change.Path)
		}
	}

	fmt.Println()
	fmt.Println("[模块契约变更]")
	if len(report.ContractChanges) == 0 {
		fmt.Println("  无")
	} else {
		for _, change := range report.ContractChanges {
			prefix := "~"
			if change.Type == "new_module" {
				prefix = "+"
			} else if change.Type == "deleted_module" {
				prefix = "-"
			}
			fmt.Printf("  %s %s  (%s)\n", prefix, change.Module, change.Details)
		}
	}

	if len(report.ManifestReasons) > 0 {
		fmt.Println()
		fmt.Println("[Manifest 信号]")
		for _, reason := range report.ManifestReasons {
			fmt.Printf("  - %s\n", reason)
		}
	}

	fmt.Println()
	fmt.Println("[建议更新]")
	if len(actions) == 0 {
		fmt.Println("  无")
		return
	}
	for i, action := range actions {
		fmt.Printf("  %d. %s  - %s\n", i+1, action.Target, action.Reason)
	}
}
