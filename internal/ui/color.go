package ui

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

var (
	initOnce sync.Once
	tty      bool
	writer   io.Writer = os.Stderr

	// 样式定义
	errorStyle   lipgloss.Style
	warnStyle    lipgloss.Style
	successStyle lipgloss.Style
	infoStyle    lipgloss.Style
	stageStyle   lipgloss.Style
	dimStyle     lipgloss.Style

	// activeTracker 指向当前活跃的 Tracker，用于输出函数自动清除渲染行
	activeTracker   *Tracker
	activeTrackerMu sync.Mutex
)

// initStyles 初始化终端检测和样式，首次调用任何输出函数时自动执行。
func initStyles() {
	initOnce.Do(func() {
		f, ok := writer.(*os.File)
		if ok {
			tty = isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
		}
		if tty {
			errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
			warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
			successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
			infoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
			stageStyle = lipgloss.NewStyle().Bold(true)
			dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		}
	})
}

// setActiveTracker 注册活跃的 Tracker（由 Tracker.Start 调用）
func setActiveTracker(tr *Tracker) {
	activeTrackerMu.Lock()
	activeTracker = tr
	activeTrackerMu.Unlock()
}

// clearActiveTracker 取消活跃的 Tracker 注册（由 Tracker.Stop 调用）
func clearActiveTracker(tr *Tracker) {
	activeTrackerMu.Lock()
	if activeTracker == tr {
		activeTracker = nil
	}
	activeTrackerMu.Unlock()
}

// suspendTracker 暂停活跃 tracker 的渲染，返回恢复函数。
// 外部输出函数在打印前调用，确保 tracker 渲染行被清除。
func suspendTracker() func() {
	activeTrackerMu.Lock()
	tr := activeTracker
	activeTrackerMu.Unlock()

	if tr == nil {
		return func() {}
	}

	tr.mu.Lock()
	tr.clearRenderedLocked()
	tr.mu.Unlock()

	return func() {
		tr.mu.Lock()
		tr.lastRendered = nil // 强制下次完整重绘
		tr.renderLocked()
		tr.mu.Unlock()
	}
}

// styled 根据 TTY 状态返回带样式或原始文本
func styled(style lipgloss.Style, format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	if !tty {
		return msg
	}
	return style.Render(msg)
}

// Error 输出错误信息（红色）
func Error(format string, args ...any) {
	initStyles()
	resume := suspendTracker()
	fmt.Fprintln(writer, styled(errorStyle, format, args...))
	resume()
}

// Warn 输出警告信息（黄色）
func Warn(format string, args ...any) {
	initStyles()
	resume := suspendTracker()
	fmt.Fprintln(writer, styled(warnStyle, format, args...))
	resume()
}

// Success 输出成功信息（绿色）
func Success(format string, args ...any) {
	initStyles()
	resume := suspendTracker()
	fmt.Fprintln(writer, styled(successStyle, format, args...))
	resume()
}

// Info 输出普通信息（青色）
func Info(format string, args ...any) {
	initStyles()
	resume := suspendTracker()
	fmt.Fprintln(writer, styled(infoStyle, format, args...))
	resume()
}

// Stage 输出阶段标题（加粗）
func Stage(format string, args ...any) {
	initStyles()
	resume := suspendTracker()
	fmt.Fprintln(writer, styled(stageStyle, format, args...))
	resume()
}

// Dim 输出弱化文本（灰色），不换行
func Dim(format string, args ...any) {
	initStyles()
	fmt.Fprint(writer, styled(dimStyle, format, args...))
}

// Plain 输出无样式文本并换行
func Plain(format string, args ...any) {
	initStyles()
	resume := suspendTracker()
	fmt.Fprintf(writer, format, args...)
	fmt.Fprintln(writer)
	resume()
}

// Println 直接输出并换行
func Println(args ...any) {
	initStyles()
	resume := suspendTracker()
	fmt.Fprintln(writer, args...)
	resume()
}

// Printf 直接格式化输出，不换行
func Printf(format string, args ...any) {
	initStyles()
	fmt.Fprintf(writer, format, args...)
}

// IsTTY 返回当前输出是否为终端
func IsTTY() bool {
	initStyles()
	return tty
}

// SetWriter 设置输出目标（仅供测试使用）。
// 必须在任何输出函数调用之前设置。
func SetWriter(w io.Writer) {
	writer = w
}

// Writer 返回当前输出 writer
func Writer() io.Writer {
	initStyles()
	return writer
}
