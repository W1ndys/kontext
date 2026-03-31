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
	updateForce   bool
	updateTargets []string
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "检测并更新 .kontext/ 物料 / Detect and update .kontext materials",
	Long: `检测代码与 .kontext 物料之间的偏差并执行更新。

默认模式：自动检测变更并更新受影响的物料。

标志：
  --force, -f    强制更新所有物料，无视变更检测
  --target, -t   指定更新目标（可多次使用）

目标格式：
  architecture           架构图
  manifest               项目清单
  contract:<模块路径>    模块契约（如 contract:internal/config）

示例：
  kontext update                              # 自动检测并更新
  kontext update --force                      # 强制更新所有物料
  kontext update -t contract:internal/config  # 只更新 config 模块契约
  kontext update -t architecture -t manifest  # 只更新架构图和清单`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUpdate()
	},
}

func init() {
	updateCmd.Flags().BoolVarP(&updateForce, "force", "f", false, "强制更新所有物料 / Force update all materials")
	updateCmd.Flags().StringArrayVarP(&updateTargets, "target", "t", nil, "指定更新目标 / Specify update target (repeatable)")
}

func runUpdate() error {
	logger := namedLogger(commandPathUpdate)
	logger.Info("update started", "force", updateForce, "targets", updateTargets)

	report, err := updater.DetectChanges(defaultKontextDir, defaultProjectDir)
	if err != nil {
		logger.Error("detect changes failed", "error", err)
		return fmt.Errorf("检测变更失败: %w", err)
	}
	logger.Info("change detection completed",
		"directory_changes", len(report.DirectoryChanges),
		"contract_changes", len(report.ContractChanges),
		"manifest_reasons", len(report.ManifestReasons),
	)

	var actions []updater.UpdateAction

	switch {
	case len(updateTargets) > 0:
		// 指定目标更新
		for _, target := range updateTargets {
			targetActions, err := updater.PlanTargetUpdate(target)
			if err != nil {
				return err
			}
			actions = append(actions, targetActions...)
		}
	case updateForce:
		// 强制更新所有
		actions = updater.PlanForceUpdateAll(report)
	default:
		// 默认：检测变更
		actions = updater.PlanUpdates(report)
	}

	logger.Info("update plan created", "planned_actions", len(actions))

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
