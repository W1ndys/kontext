package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/w1ndys/kontext/internal/config"
	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/internal/updater"
)

var (
	updateDryRun bool
	updateFile   string
	updateSince  string
	updateUI     = newUpdateProgressUI()
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
		fmt.Printf("使用 LLM: %s (模型: %s)\n", llmCfg.BaseURL, llmCfg.Model)
		client, err := llm.NewClient(llmCfg)
		if err != nil {
			logger.Error("create llm client failed", "error", err)
			return err
		}

		updateUI.Start()
		logger.Info("update execution started", "planned_actions", len(actions))
		executor := updater.NewExecutor(client, defaultKontextDir, defaultProjectDir)
		executor.SetProgressHandler(printUpdateProgress)
		updated, err := executor.Execute(report, actions)
		updateUI.Stop()
		if err != nil {
			logger.Error("update execution failed", "error", err)
			return fmt.Errorf("执行更新失败: %w", err)
		}

		logger.Info("update completed", "updated_count", len(updated))
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
	fmt.Println("=== Kontext 物料变更检测报告 ===")
	fmt.Println()

	fmt.Println("[目录结构变更]")
	if len(report.DirectoryChanges) == 0 {
		fmt.Println("  无")
	} else {
		for _, change := range report.DirectoryChanges {
			prefix := "~"
			switch change.Type {
			case "added":
				prefix = "+"
			case "removed":
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
			switch change.Type {
			case "new_module":
				prefix = "+"
			case "deleted_module":
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

// 处理更新进度事件，记录日志并更新 UI
func printUpdateProgress(event updater.ProgressEvent) {
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
			"duration_seconds", parseElapsedSeconds(event.Message),
		)
	}
	updateUI.Handle(event)
}

type updateProgressUI struct {
	mu         sync.Mutex
	states     map[string]*updateTaskState
	order      []string
	tickerStop chan struct{}
	tickerDone chan struct{}
	rendered   int
	running    bool
	lastTotal  int
}

type updateTaskState struct {
	index     int
	total     int
	target    string
	startedAt time.Time
	done      bool
	doneText  string
}

// 创建更新进度 UI 实例
func newUpdateProgressUI() *updateProgressUI {
	return &updateProgressUI{
		states: make(map[string]*updateTaskState),
	}
}

// Start 启动进度 UI 的渲染循环
func (ui *updateProgressUI) Start() {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	ui.states = make(map[string]*updateTaskState)
	ui.order = nil
	ui.rendered = 0
	ui.lastTotal = 0
	ui.tickerStop = make(chan struct{})
	ui.tickerDone = make(chan struct{})
	ui.running = true

	go ui.loop(ui.tickerStop, ui.tickerDone)
}

// Stop 停止进度 UI 的渲染循环并输出最终状态
func (ui *updateProgressUI) Stop() {
	ui.mu.Lock()
	if !ui.running {
		ui.mu.Unlock()
		return
	}
	stop := ui.tickerStop
	done := ui.tickerDone
	ui.running = false
	ui.mu.Unlock()

	close(stop)
	<-done

	ui.mu.Lock()
	ui.renderLocked()
	fmt.Println()
	ui.mu.Unlock()
}

// 后台定时刷新进度显示的循环
func (ui *updateProgressUI) loop(stop <-chan struct{}, done chan<- struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer close(done)

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			ui.mu.Lock()
			ui.renderLocked()
			ui.mu.Unlock()
		}
	}
}

// Handle 处理单个进度事件，更新任务状态并触发渲染
func (ui *updateProgressUI) Handle(event updater.ProgressEvent) {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	key := ui.taskKey(event)
	switch event.Stage {
	case updater.ProgressActionStart:
		if _, ok := ui.states[key]; !ok {
			ui.order = append(ui.order, key)
		}
		ui.states[key] = &updateTaskState{
			index:     event.Index,
			total:     event.Total,
			target:    event.Action.Target,
			startedAt: time.Now(),
		}
		ui.lastTotal = event.Total
	case updater.ProgressActionDone:
		state, ok := ui.states[key]
		if !ok {
			state = &updateTaskState{
				index:     event.Index,
				total:     event.Total,
				target:    event.Action.Target,
				startedAt: time.Now(),
			}
			ui.states[key] = state
			ui.order = append(ui.order, key)
		}
		state.done = true
		state.doneText = event.Message
		ui.lastTotal = event.Total
	}

	ui.renderLocked()
}

// 根据事件生成任务的唯一标识键
func (ui *updateProgressUI) taskKey(event updater.ProgressEvent) string {
	return fmt.Sprintf("%d:%s", event.Index, event.Action.Target)
}

// 在持锁状态下渲染所有任务行（覆盖式刷新）
func (ui *updateProgressUI) renderLocked() {
	lines := ui.linesLocked()
	if ui.rendered > 0 {
		fmt.Printf("\x1b[%dA", ui.rendered)
	}
	for i, line := range lines {
		fmt.Print("\x1b[2K\r")
		fmt.Print(line)
		if i < len(lines)-1 {
			fmt.Print("\n")
		}
	}
	ui.rendered = len(lines)
}

// 在持锁状态下生成所有任务的显示行
func (ui *updateProgressUI) linesLocked() []string {
	if len(ui.order) == 0 {
		return nil
	}

	lines := make([]string, 0, len(ui.order))
	now := time.Now()
	for _, key := range ui.order {
		state := ui.states[key]
		if state == nil {
			continue
		}
		total := state.total
		if total == 0 {
			total = ui.lastTotal
		}
		if state.done {
			lines = append(lines, fmt.Sprintf("[%d/%d] %s  更新完成(耗时%s)", state.index, total, state.target, state.doneText))
			continue
		}
		elapsed := int(now.Sub(state.startedAt).Round(time.Second) / time.Second)
		if elapsed < 1 {
			elapsed = 1
		}
		lines = append(lines, fmt.Sprintf("[%d/%d] 更新 %s 中(%ds)", state.index, total, state.target, elapsed))
	}
	return lines
}

// 从耗时字符串中解析出秒数
func parseElapsedSeconds(value string) int {
	trimmed := strings.TrimSuffix(strings.TrimSpace(value), "s")
	seconds, err := strconv.Atoi(trimmed)
	if err != nil || seconds < 1 {
		return 1
	}
	return seconds
}
