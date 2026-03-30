package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/w1ndys/kontext/internal/config"
	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/internal/ui"
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
		logger := namedLogger(commandPathUpdate)
		logger.Info("update started",
			"dry_run", updateDryRun,
			"file_filter", normalizedUpdateFilter(updateFile),
			"since", updateSince,
		)

		if err := validateUpdateFilter(updateFile); err != nil {
			logger.Warn("invalid update filter", "error", err)
			return err
		}

		report, err := updater.DetectChanges(defaultKontextDir, defaultProjectDir, updateSince)
		if err != nil {
			logger.Error("detect changes failed", "error", err)
			return fmt.Errorf("检测变更失败: %w", err)
		}
		logger.Info("change detection completed",
			"directory_changes", len(report.DirectoryChanges),
			"contract_changes", len(report.ContractChanges),
			"manifest_reasons", len(report.ManifestReasons),
			"affected_modules", len(report.AffectedModules),
		)

		actions := updater.PlanUpdates(report, normalizedUpdateFilter(updateFile))
		logger.Info("update plan created", "planned_actions", len(actions))
		if updateDryRun {
			logger.Info("update dry run completed", "planned_actions", len(actions))
			printUpdateReport(report, actions)
			return nil
		}

		if len(actions) == 0 {
			logger.Info("update skipped because no actions were planned")
			fmt.Println("未检测到需要更新的物料。")
			return nil
		}

		printPlannedUpdates(actions)
		if !confirmPlannedUpdates() {
			logger.Info("update cancelled by user", "planned_actions", len(actions))
			fmt.Println("已取消更新。")
			return nil
		}

		cfg, err := config.Load()
		if err != nil {
			logger.Error("load llm config failed", "error", err)
			return fmt.Errorf("加载 LLM 配置失败: %w", err)
		}

		llmCfg := cfg.ToLLMConfig()
		logger.Info("llm config loaded",
			"base_url", llmCfg.BaseURL,
			"model", llmCfg.Model,
			"planned_actions", len(actions),
		)
		ui.Info("使用 LLM: %s (模型: %s)", llmCfg.BaseURL, llmCfg.Model)
		client, err := llm.NewClient(llmCfg)
		if err != nil {
			logger.Error("create llm client failed", "error", err)
			return err
		}

		tracker := ui.NewTracker()
		tracker.Start()
		updateTasks := make(map[string]*ui.Task)

		logger.Info("update execution started", "planned_actions", len(actions))
		executor := updater.NewExecutor(client, defaultKontextDir, defaultProjectDir)
		executor.SetProgressHandler(func(event updater.ProgressEvent) {
			logUpdateProgress(event)
			key := fmt.Sprintf("%d:%s", event.Index, event.Action.Target)
			switch event.Stage {
			case updater.ProgressActionStart:
				task := tracker.AddTask(fmt.Sprintf("[%d/%d] 更新 %s", event.Index, event.Total, event.Action.Target))
				updateTasks[key] = task
			case updater.ProgressActionDone:
				if task, ok := updateTasks[key]; ok {
					task.Done()
				}
			}
		})
		updated, err := executor.Execute(report, actions)
		tracker.Stop()
		if err != nil {
			logger.Error("update execution failed", "error", err)
			return fmt.Errorf("执行更新失败: %w", err)
		}

		logger.Info("update completed", "updated_count", len(updated))
		ui.Success("已更新以下物料：")
		for _, path := range updated {
			ui.Plain("  %s", path)
		}
		return nil
	},
}

func init() {
	updateCmd.Flags().BoolVar(&updateDryRun, "dry-run", false, "只检测并打印变更报告 / Detect only without modifying files")
	updateCmd.Flags().StringVar(&updateFile, "file", "", "仅更新指定物料：manifest|architecture|contracts|all")
	updateCmd.Flags().StringVar(&updateSince, "since", "", "只分析指定 commit 之后的变更 / Analyze changes since commit")
}

// 校验 --file 参数值是否合法
func validateUpdateFilter(filter string) error {
	switch normalizedUpdateFilter(filter) {
	case "", "all", "manifest", "architecture", "contracts":
		return nil
	default:
		return fmt.Errorf("--file 仅支持 manifest、architecture、contracts、all")
	}
}

// 将 filter 参数统一为小写并去除空白
func normalizedUpdateFilter(filter string) string {
	return strings.ToLower(strings.TrimSpace(filter))
}

// 打印 dry-run 模式下的完整变更检测报告
func printUpdateReport(report *updater.ChangeReport, actions []updater.UpdateAction) {
	ui.Stage("=== Kontext 物料变更检测报告 ===")
	fmt.Println()

	ui.Stage("[目录结构变更]")
	if len(report.DirectoryChanges) == 0 {
		fmt.Println("  无")
	} else {
		for _, change := range report.DirectoryChanges {
			switch change.Type {
			case "added":
				ui.Success("  + %s", change.Path)
			case "removed":
				ui.Error("  - %s", change.Path)
			default:
				ui.Warn("  ~ %s", change.Path)
			}
		}
	}

	fmt.Println()
	ui.Stage("[模块契约变更]")
	if len(report.ContractChanges) == 0 {
		fmt.Println("  无")
	} else {
		for _, change := range report.ContractChanges {
			switch change.Type {
			case "new_module":
				ui.Success("  + %s  (%s)", change.Module, change.Details)
			case "deleted_module":
				ui.Error("  - %s  (%s)", change.Module, change.Details)
			default:
				ui.Warn("  ~ %s  (%s)", change.Module, change.Details)
			}
		}
	}

	if len(report.ManifestReasons) > 0 {
		fmt.Println()
		ui.Stage("[Manifest 信号]")
		for _, reason := range report.ManifestReasons {
			fmt.Printf("  - %s\n", reason)
		}
	}

	fmt.Println()
	ui.Stage("[建议更新]")
	if len(actions) == 0 {
		fmt.Println("  无")
		return
	}
	for i, action := range actions {
		fmt.Printf("  %d. %s  - %s\n", i+1, action.Target, action.Reason)
	}
}

// 打印即将执行的更新动作列表
func printPlannedUpdates(actions []updater.UpdateAction) {
	fmt.Println("即将更新以下物料 / Planned updates:")
	for i, action := range actions {
		fmt.Printf("  %d. %s - %s\n", i+1, action.Target, action.Reason)
	}
	fmt.Println()
}

// 提示用户确认是否继续执行更新
func confirmPlannedUpdates() bool {
	fmt.Print("是否继续执行更新？[y/N] ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "y" || answer == "yes"
}

// logUpdateProgress 记录更新进度到结构化日志
func logUpdateProgress(event updater.ProgressEvent) {
	logger := namedLogger(commandPathUpdate)
	switch event.Stage {
	case updater.ProgressActionStart:
		logger.Debug("update action started",
			"target", event.Action.Target,
			"index", event.Index,
			"total", event.Total,
			"module", event.Action.Module,
			"change_type", event.Action.ChangeType,
		)
	case updater.ProgressActionDone:
		logger.Debug("update action completed",
			"target", event.Action.Target,
			"index", event.Index,
			"total", event.Total,
			"target_path", event.TargetPath,
		)
	}
}

