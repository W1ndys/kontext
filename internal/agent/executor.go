package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/llm"
)

const defaultMaxRetries = 3

// taskExecutor 负责执行单个 AgentTask。
type taskExecutor struct {
	client      llm.Client
	onProgress  func(ProgressEvent)
	total       int
	completed   *int       // 指向 orchestrator 的已完成计数器
	completedMu *sync.Mutex // 保护 completed 的并发访问
}

// execute 执行单个任务，返回生成的内容。
func (te *taskExecutor) execute(task *AgentTask, resolved map[string]string) (string, error) {
	te.emitProgress(ProgressEvent{
		Type:   ProgressTaskStart,
		TaskID: task.ID,
		Label:  task.Label,
		Total:  te.total,
	})

	startTime := time.Now()

	// 选择执行路径
	var content string
	var err error
	if task.CustomExecute != nil {
		content, err = task.CustomExecute(te.client, resolved)
	} else {
		content, err = te.executeDefault(task, resolved)
	}

	if err != nil {
		return "", err
	}

	// PostProcess
	if task.PostProcess != nil {
		content, err = task.PostProcess(content)
		if err != nil {
			return "", fmt.Errorf("后处理失败: %w", err)
		}
	}

	// 写文件
	if task.OutputPath != "" {
		if err := fileutil.WriteFile(task.OutputPath, []byte(content)); err != nil {
			return "", fmt.Errorf("写入 %s 失败: %w", task.OutputPath, err)
		}
	}

	te.completedMu.Lock()
	*te.completed++
	idx := *te.completed
	te.completedMu.Unlock()

	te.emitProgress(ProgressEvent{
		Type:    ProgressTaskDone,
		TaskID:  task.ID,
		Label:   task.Label,
		Message: formatDuration(time.Since(startTime)),
		Index:   idx,
		Total:   te.total,
	})

	return content, nil
}

// executeDefault 执行默认路径：BuildUserMsg → LLM → Validate → Retry。
func (te *taskExecutor) executeDefault(task *AgentTask, resolved map[string]string) (string, error) {
	userMsg, err := task.BuildUserMsg(resolved)
	if err != nil {
		return "", fmt.Errorf("构建用户消息失败: %w", err)
	}

	messages := []llm.Message{
		{Role: "system", Content: task.SystemPrompt},
		{Role: "user", Content: userMsg},
	}

	maxRetries := task.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	te.emitProgress(ProgressEvent{
		Type:   ProgressLLMStart,
		TaskID: task.ID,
		Label:  task.Label,
		Total:  te.total,
	})

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		req := &llm.ChatRequest{Messages: messages}

		// 优先尝试 JSON Schema 结构化输出
		content, err := te.callLLM(req)
		if err != nil {
			lastErr = fmt.Errorf("调用 LLM 失败 (尝试 %d/%d): %w", attempt+1, maxRetries, err)
			continue
		}

		// 校验
		if task.Validate != nil {
			if valErr := task.Validate(content); valErr != nil {
				lastErr = valErr
				te.emitProgress(ProgressEvent{
					Type:    ProgressLLMRetry,
					TaskID:  task.ID,
					Label:   task.Label,
					Message: valErr.Error(),
					Total:   te.total,
				})
				// 追加错误到对话历史，让 LLM 修正
				messages = append(messages,
					llm.Message{Role: "assistant", Content: content},
					llm.Message{Role: "user", Content: fmt.Sprintf(
						"上一次返回的内容校验失败：%v。请修正后重新返回。", valErr,
					)},
				)
				continue
			}
		}

		return content, nil
	}

	return "", fmt.Errorf("LLM 调用在 %d 次尝试后仍未成功: %w", maxRetries, lastErr)
}

// callLLM 调用 LLM 并提取内容。优先使用结构化输出，回退到文本解析。
func (te *taskExecutor) callLLM(req *llm.ChatRequest) (string, error) {
	// 优先尝试 JSON Schema 结构化输出
	var structured singleContentResult
	if _, err := te.client.ChatStructured(req, "single_content", &structured); err == nil {
		content := strings.TrimSpace(structured.Content)
		if content != "" {
			return content, nil
		}
	}

	// 回退到普通 Chat
	resp, err := te.client.Chat(req)
	if err != nil {
		return "", err
	}

	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return "", fmt.Errorf("LLM 返回内容为空")
	}

	return content, nil
}

// singleContentResult 是 LLM 结构化输出的通用包装。
type singleContentResult struct {
	Content string `json:"content" jsonschema:"description=生成的完整 JSON 文本内容"`
}

func (te *taskExecutor) emitProgress(event ProgressEvent) {
	if te.onProgress != nil {
		te.onProgress(event)
	}
}

// formatDuration 将耗时格式化为 "Ns" 字符串。
func formatDuration(d time.Duration) string {
	seconds := max(int(d.Round(time.Second)/time.Second), 1)
	return fmt.Sprintf("%ds", seconds)
}
