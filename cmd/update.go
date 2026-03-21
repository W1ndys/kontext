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

		printPlannedUpdates(actions)
		if !confirmPlannedUpdates() {
			fmt.Println("已取消更新。")
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

		fmt.Println("开始执行更新... / Applying updates...")
		executor := updater.NewExecutor(client, ".kontext", ".")
		executor.SetProgressHandler(printUpdateProgress)
		updated, err := executor.Execute(report, actions)
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

func printPlannedUpdates(actions []updater.UpdateAction) {
	fmt.Println("即将更新以下物料 / Planned updates:")
	for i, action := range actions {
		fmt.Printf("  %d. %s - %s\n", i+1, action.Target, action.Reason)
	}
	fmt.Println()
}

func confirmPlannedUpdates() bool {
	fmt.Print("是否继续执行更新？[y/N] ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "y" || answer == "yes"
}

func printUpdateProgress(event updater.ProgressEvent) {
	switch event.Stage {
	case updater.ProgressActionStart:
		fmt.Printf("[%d/%d] 更新 %s...\n", event.Index, event.Total, event.Action.Target)
		fmt.Printf("  目标文件: %s\n", event.TargetPath)
		fmt.Printf("  原因: %s\n", event.Action.Reason)
	case updater.ProgressLLMStart:
		fmt.Println("  正在调用 LLM 生成更新内容...")
	case updater.ProgressStructuredFallback:
		fmt.Printf("  结构化输出失败，回退到传统 JSON 模式: %s\n", event.Message)
	case updater.ProgressYAMLRetry:
		fmt.Printf("  返回内容不是合法 YAML，正在请求模型修正: %s\n", event.Message)
	case updater.ProgressActionDone:
		fmt.Println("  已完成")
	}
}
