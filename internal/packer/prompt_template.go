package packer

import (
	"bytes"
	"text/template"

	"github.com/w1ndys/kontext/templates"
)

// TemplateData 保存传递给 LLM 提示词模板的所有数据。
type TemplateData struct {
	Task            string
	ProjectName     string
	ProjectOneLine  string
	ProjectType     string
	TechStack       string
	Phase           string
	BusinessContext string
	Architecture    string
	Conventions     string
	Contracts       string
	RelevantFiles   string
	DirectoryTree   string
	RelevantCode    string
}

// RenderSystemPrompt 返回渲染后的系统提示词。
func RenderSystemPrompt() (string, error) {
	return templates.SystemPrompt, nil
}

// RenderUserPrompt 使用给定数据渲染用户提示词模板。
func RenderUserPrompt(data *TemplateData) (string, error) {
	tmpl, err := template.New("user_prompt").Parse(templates.UserPrompt)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
