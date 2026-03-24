package cmd

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// manualInputModelName 是手动输入选项的特殊标记。
const manualInputModelName = "__manual_input__"

// modelItem 实现 list.Item 接口
type modelItem struct {
	name      string
	isCurrent bool
	isManual  bool // 是否为"手动输入"选项
}

func (i modelItem) Title() string {
	if i.isManual {
		return "✏️  手动输入模型名称..."
	}
	if i.isCurrent {
		return i.name + " (当前)"
	}
	return i.name
}
func (i modelItem) Description() string { return "" }
func (i modelItem) FilterValue() string {
	if i.isManual {
		return "手动输入 manual input"
	}
	return i.name
}

// modelSelector 是 Bubble Tea 的 Model
type modelSelector struct {
	list     list.Model
	selected string
	quitting bool
}

func (m modelSelector) Init() tea.Cmd {
	return nil
}

func (m modelSelector) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if item, ok := m.list.SelectedItem().(modelItem); ok {
				m.selected = item.name
			}
			m.quitting = true
			return m, tea.Quit
		case "q", "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		m.list.SetHeight(msg.Height - 2)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m modelSelector) View() string {
	if m.quitting {
		return ""
	}
	return "\n" + m.list.View()
}

var listTitleStyle = lipgloss.NewStyle().MarginLeft(2).Bold(true)

// runModelSelector 启动交互式模型选择器，返回选中的模型名。
// 如果用户按 ESC/q 取消则返回空字符串。
// 如果用户选择手动输入则返回 manualInputModelName。
func runModelSelector(models []string, currentModel string) (string, error) {
	// +1 是为了末尾的"手动输入"选项
	items := make([]list.Item, len(models)+1)

	// 找到当前模型并置顶
	currentIdx := -1
	for i, m := range models {
		if m == currentModel {
			currentIdx = i
			break
		}
	}

	idx := 0
	if currentIdx >= 0 {
		items[0] = modelItem{name: models[currentIdx], isCurrent: true}
		idx = 1
	}
	for i, m := range models {
		if i == currentIdx {
			continue
		}
		items[idx] = modelItem{name: m, isCurrent: false}
		idx++
	}

	// 在末尾添加"手动输入"选项
	items[idx] = modelItem{name: manualInputModelName, isManual: true}

	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false

	l := list.New(items, delegate, 60, 15)
	l.Title = "选择模型（↑↓ 移动，/ 搜索，Enter 确认，Esc 跳过）"
	l.Styles.Title = listTitleStyle
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(true)

	m := modelSelector{list: l}
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	result := finalModel.(modelSelector)
	return result.selected, nil
}
