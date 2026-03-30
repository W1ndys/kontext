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
	fmt.Fprintln(writer, styled(errorStyle, format, args...))
}

// Warn 输出警告信息（黄色）
func Warn(format string, args ...any) {
	initStyles()
	fmt.Fprintln(writer, styled(warnStyle, format, args...))
}

// Success 输出成功信息（绿色）
func Success(format string, args ...any) {
	initStyles()
	fmt.Fprintln(writer, styled(successStyle, format, args...))
}

// Info 输出普通信息（青色）
func Info(format string, args ...any) {
	initStyles()
	fmt.Fprintln(writer, styled(infoStyle, format, args...))
}

// Stage 输出阶段标题（加粗）
func Stage(format string, args ...any) {
	initStyles()
	fmt.Fprintln(writer, styled(stageStyle, format, args...))
}

// Dim 输出弱化文本（灰色），不换行
func Dim(format string, args ...any) {
	initStyles()
	fmt.Fprint(writer, styled(dimStyle, format, args...))
}

// Plain 输出无样式文本并换行
func Plain(format string, args ...any) {
	initStyles()
	fmt.Fprintf(writer, format, args...)
	fmt.Fprintln(writer)
}

// Println 直接输出并换行
func Println(args ...any) {
	initStyles()
	fmt.Fprintln(writer, args...)
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
