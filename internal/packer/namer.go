package packer

import (
	"fmt"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/templates"
)

const maxFilenameSuggestionRunes = 24

type filenameSuggestion struct {
	Title string `json:"title"`
}

type filenameSuggestionTemplateData struct {
	Task string
}

// GenerateFilenameSuggestion 使用 LLM 生成适合作为文件名基础的短标题。
func GenerateFilenameSuggestion(client llm.Client, task string, onRetry func(attempt int, err error, backoff time.Duration)) (string, error) {
	if client == nil {
		return "", fmt.Errorf("LLM 客户端未初始化")
	}

	userPrompt, err := renderFilenameSuggestionUserPrompt(filenameSuggestionTemplateData{Task: task})
	if err != nil {
		return "", fmt.Errorf("渲染文件名提示词失败: %w", err)
	}

	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: templates.PackGenerateFilenameSystem},
			{Role: "user", Content: userPrompt},
		},
	}

	var out filenameSuggestion
	if _, err := llm.ChatStructuredWithRetry(client, req, "pack_filename_suggestion", &out, 3, onRetry); err != nil {
		return "", err
	}

	title := normalizeFilenameSuggestion(out.Title)
	if title == "" {
		return "", fmt.Errorf("模型未返回有效文件名标题")
	}

	return title, nil
}

// 渲染文件名建议的用户提示词模板
func renderFilenameSuggestionUserPrompt(data filenameSuggestionTemplateData) (string, error) {
	tmpl, err := template.New("pack_generate_filename_user").Parse(templates.PackGenerateFilenameUser)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// 规范化文件名建议，去除引号并限制长度
func normalizeFilenameSuggestion(s string) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	s = strings.Trim(s, `"'`+"`“”‘’「」『』《》【】")
	if s == "" {
		return ""
	}

	if utf8.RuneCountInString(s) <= maxFilenameSuggestionRunes {
		return s
	}

	runes := []rune(s)
	return strings.TrimSpace(string(runes[:maxFilenameSuggestionRunes]))
}
