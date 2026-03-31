package agent

import (
	"time"

	"github.com/w1ndys/kontext/internal/llm"
)

// AgentTask 描述一个 LLM 生成任务的完整声明。
type AgentTask struct {
	// ID 是任务唯一标识，如 "manifest"、"architecture"、"contract:internal/llm"。
	ID string

	// DependsOn 是依赖的任务 ID 列表。Orchestrator 保证这些任务完成后才执行当前任务。
	DependsOn []string

	// Label 是人类可读的任务描述，用于进度显示。
	Label string

	// === 默认执行路径（Orchestrator 统一处理）===

	// SystemPrompt 是 LLM 的 system prompt。
	SystemPrompt string

	// BuildUserMsg 构建 user message。resolved 是已完成的依赖任务的输出，
	// key 为任务 ID，value 为该任务生成的内容。
	BuildUserMsg func(resolved map[string]string) (string, error)

	// Validate 校验 LLM 输出内容。校验失败会触发重试（将错误追加到对话让 LLM 修正）。
	// nil 表示不校验。
	Validate func(content string) error

	// PostProcess 后处理 LLM 输出内容（如 FormatJSON、NormalizeContractJSON）。
	// 在校验通过后、写文件前执行。nil 表示不处理。
	PostProcess func(content string) (string, error)

	// OutputPath 是写入的文件路径。空字符串表示不写文件（结果仍存入 resolved map 供下游使用）。
	OutputPath string

	// MaxRetries 是 LLM 调用校验失败时的修正重试次数。0 使用默认值（3）。
	// 注意：这不是网络层重试（网络重试在 internal/llm 已有）。
	MaxRetries int

	// === 自定义执行路径 ===

	// CustomExecute 有值时跳过默认的 "BuildUserMsg → LLM → Validate → Retry" 流程，
	// 直接调用此函数。返回的 content 仍走 PostProcess 和文件写入。
	// 用于 update 的分段契约生成等复杂场景。
	CustomExecute func(client llm.Client, resolved map[string]string) (string, error)
}

// TaskResult 存储单个任务的执行结果。
type TaskResult struct {
	ID       string
	Content  string // LLM 生成的内容（校验 + 后处理后）
	Duration time.Duration
	Err      error
}

// RunResult 存储整次编排的执行结果。
type RunResult struct {
	Results  map[string]*TaskResult // ID → TaskResult
	Errors   []error                // 所有失败任务的错误
	Duration time.Duration          // 总耗时
}

// ProgressType 描述进度事件的类型。
type ProgressType int

const (
	ProgressTaskStart  ProgressType = iota // 任务开始执行
	ProgressLLMStart                       // LLM 调用开始
	ProgressLLMRetry                       // LLM 校验失败，重试
	ProgressTaskDone                       // 任务完成
	ProgressTaskFailed                     // 任务失败
)

// ProgressEvent 描述编排过程中的进度事件。
type ProgressEvent struct {
	Type    ProgressType
	TaskID  string
	Label   string
	Message string
	Index   int // 当前已完成任务数（含本次）
	Total   int // 总任务数
}
