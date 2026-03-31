package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// taskState 内部任务状态
type taskState struct {
	id        int
	label     string
	startedAt time.Time
	done      bool
	failed    bool
	errMsg    string
	elapsed   time.Duration
	doneLabel string // 完成时的自定义文本，为空时使用 label
}

// Task 代表一个正在进行的任务的句柄
type Task struct {
	id      int
	tracker *Tracker
}

// Done 标记任务完成，停止计时器
func (t *Task) Done() {
	t.tracker.finishTask(t.id, false, "")
}

// DoneWithLabel 标记任务完成，使用自定义完成文本
func (t *Task) DoneWithLabel(label string) {
	t.tracker.finishTaskWithLabel(t.id, label)
}

// Fail 标记任务失败
func (t *Task) Fail(err error) {
	t.tracker.finishTask(t.id, true, err.Error())
}

// Update 更新任务的显示文本
func (t *Task) Update(label string) {
	t.tracker.updateTask(t.id, label)
}

// Tracker 管理一组进度任务的显示刷新
type Tracker struct {
	mu            sync.Mutex
	tasks         []*taskState
	tickerStop    chan struct{}
	tickerDone    chan struct{}
	running       bool
	nextID        int
	frameIndex    int
	renderedLines int
	lastRendered  []string // 每行上次渲染的内容，用于差量更新
}

// NewTracker 创建进度追踪器
func NewTracker() *Tracker {
	initStyles()
	return &Tracker{}
}

// Start 启动后台刷新循环
func (tr *Tracker) Start() {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	if tr.running {
		return
	}
	tr.tickerStop = make(chan struct{})
	tr.tickerDone = make(chan struct{})
	tr.running = true

	setActiveTracker(tr)

	go tr.loop()
}

// Stop 停止刷新并输出最终状态
func (tr *Tracker) Stop() {
	tr.mu.Lock()
	if !tr.running {
		tr.mu.Unlock()
		return
	}
	stop := tr.tickerStop
	done := tr.tickerDone
	tr.running = false
	tr.mu.Unlock()

	close(stop)
	<-done

	clearActiveTracker(tr)

	tr.mu.Lock()
	tr.renderLocked()
	if tr.renderedLines > 0 {
		fmt.Fprintln(writer)
	}
	tr.renderedLines = 0
	tr.lastRendered = nil
	tr.mu.Unlock()
}

// AddTask 添加一个新任务，返回任务句柄
func (tr *Tracker) AddTask(label string) *Task {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	id := tr.nextID
	tr.nextID++

	state := &taskState{
		id:        id,
		label:     label,
		startedAt: time.Now(),
	}
	tr.tasks = append(tr.tasks, state)

	if !tty {
		fmt.Fprintf(writer, "   %s...\n", label)
	}

	return &Task{id: id, tracker: tr}
}

// Interject 暂停渲染，执行 fn（用于在进度行间插入消息），然后恢复渲染
func (tr *Tracker) Interject(fn func()) {
	tr.mu.Lock()
	tr.clearRenderedLocked()
	tr.mu.Unlock()

	fn()

	tr.mu.Lock()
	tr.renderLocked()
	tr.mu.Unlock()
}

func (tr *Tracker) finishTask(id int, failed bool, errMsg string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	state := tr.findTask(id)
	if state == nil || state.done {
		return
	}

	state.done = true
	state.failed = failed
	state.errMsg = errMsg
	state.elapsed = time.Since(state.startedAt)

	if !tty {
		if failed {
			fmt.Fprintf(writer, "   ✗ %s: %s\n", state.label, errMsg)
		} else {
			label := state.label
			if state.doneLabel != "" {
				label = state.doneLabel
			}
			fmt.Fprintf(writer, "   ✓ %s（%.1f 秒）\n", label, state.elapsed.Seconds())
		}
	}

	tr.renderLocked()
}

func (tr *Tracker) finishTaskWithLabel(id int, label string) {
	tr.mu.Lock()
	state := tr.findTask(id)
	if state != nil {
		state.doneLabel = label
	}
	tr.mu.Unlock()

	tr.finishTask(id, false, "")
}

func (tr *Tracker) updateTask(id int, label string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	state := tr.findTask(id)
	if state != nil && !state.done {
		state.label = label
	}
}

func (tr *Tracker) findTask(id int) *taskState {
	for _, t := range tr.tasks {
		if t.id == id {
			return t
		}
	}
	return nil
}

func (tr *Tracker) loop() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	defer close(tr.tickerDone)

	for {
		select {
		case <-tr.tickerStop:
			return
		case <-ticker.C:
			tr.mu.Lock()
			tr.frameIndex++
			tr.renderLocked()
			tr.mu.Unlock()
		}
	}
}

// clearRenderedLocked 清除已渲染的行（持锁状态调用）
func (tr *Tracker) clearRenderedLocked() {
	if !tty || tr.renderedLines == 0 {
		return
	}
	// 将光标移到已渲染内容的起始位置
	if tr.renderedLines > 1 {
		fmt.Fprintf(writer, "\x1b[%dA", tr.renderedLines-1)
	}
	// 清除每一行
	for i := 0; i < tr.renderedLines; i++ {
		fmt.Fprint(writer, "\x1b[2K")
		if i < tr.renderedLines-1 {
			fmt.Fprint(writer, "\x1b[1B")
		}
	}
	// 回到起始位置
	if tr.renderedLines > 1 {
		fmt.Fprintf(writer, "\x1b[%dA", tr.renderedLines-1)
	}
	fmt.Fprint(writer, "\r")
	tr.renderedLines = 0
}

// renderLocked 渲染所有可见任务（持锁状态调用）
// 采用差量更新策略：仅重绘内容发生变化的行，避免闪烁。
func (tr *Tracker) renderLocked() {
	if !tty {
		return
	}

	visible := tr.visibleTasks()
	if len(visible) == 0 {
		return
	}

	now := time.Now()
	lines := make([]string, 0, len(visible))

	for _, task := range visible {
		var line string
		if task.done {
			label := task.label
			if task.doneLabel != "" {
				label = task.doneLabel
			}
			if task.failed {
				line = fmt.Sprintf("   %s %s: %s",
					styled(errorStyle, "✗"),
					label,
					task.errMsg)
			} else {
				line = fmt.Sprintf("   %s %s%s",
					styled(successStyle, "✓"),
					label,
					styled(dimStyle, "（%.1f 秒）", task.elapsed.Seconds()))
			}
		} else {
			elapsed := int(now.Sub(task.startedAt).Seconds())
			frame := spinnerFrames[tr.frameIndex%len(spinnerFrames)]
			line = fmt.Sprintf("   %s %s%s",
				styled(infoStyle, "%s", frame),
				task.label,
				styled(dimStyle, "（%d 秒）", elapsed))
		}
		lines = append(lines, line)
	}

	// 如果行数发生变化，走全量清除+重绘路径
	if tr.renderedLines != len(lines) || tr.lastRendered == nil {
		tr.clearRenderedLocked()
		output := strings.Join(lines, "\n")
		fmt.Fprint(writer, output)
		tr.renderedLines = len(lines)
		tr.lastRendered = lines
		return
	}

	// 行数不变，逐行差量更新：光标移到第一行，逐行比对
	if tr.renderedLines > 1 {
		fmt.Fprintf(writer, "\x1b[%dA", tr.renderedLines-1)
	}
	fmt.Fprint(writer, "\r")

	for i, line := range lines {
		if i < len(tr.lastRendered) && tr.lastRendered[i] == line {
			// 该行未变化，跳过
			if i < len(lines)-1 {
				fmt.Fprint(writer, "\x1b[1B")
			}
			continue
		}
		// 该行有变化，清除后重写
		fmt.Fprint(writer, "\x1b[2K")
		fmt.Fprint(writer, line)
		if i < len(lines)-1 {
			fmt.Fprint(writer, "\x1b[1B\r")
		}
	}

	tr.lastRendered = lines
}

// visibleTasks 返回需要渲染的任务列表。
// 只要还有未完成的任务，就显示全部任务（含已完成的），
// 确保快速完成的任务也能在进度列表中可见。
// 所有任务都完成时，只保留最后一个供最终渲染。
func (tr *Tracker) visibleTasks() []*taskState {
	if len(tr.tasks) == 0 {
		return nil
	}

	// 检查是否还有未完成的任务
	hasActive := false
	for _, t := range tr.tasks {
		if !t.done {
			hasActive = true
			break
		}
	}

	if !hasActive {
		// 所有任务都完成了，只显示最后一个
		return tr.tasks[len(tr.tasks)-1:]
	}

	// 还有活跃任务时，显示全部任务
	return tr.tasks
}
