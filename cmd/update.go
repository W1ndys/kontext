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
	updateLogMu  sync.Mutex
	updateUI     = newUpdateProgressUI()
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

		updateUI.Start()
		executor := updater.NewExecutor(client, ".kontext", ".")
		executor.SetProgressHandler(printUpdateProgress)
		updated, err := executor.Execute(report, actions)
		updateUI.Stop()
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

func newUpdateProgressUI() *updateProgressUI {
	return &updateProgressUI{
		states: make(map[string]*updateTaskState),
	}
}

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

func (ui *updateProgressUI) taskKey(event updater.ProgressEvent) string {
	return fmt.Sprintf("%d:%s", event.Index, event.Action.Target)
}

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

func parseElapsedSeconds(value string) int {
	trimmed := strings.TrimSuffix(strings.TrimSpace(value), "s")
	seconds, err := strconv.Atoi(trimmed)
	if err != nil || seconds < 1 {
		return 1
	}
	return seconds
}
