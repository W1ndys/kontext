package packer

import (
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/templates"
)

// IdentifyRelevantFiles 使用 LLM 识别与任务相关的文件
func IdentifyRelevantFiles(client llm.Client, task string, candidatePaths []string, projectRoot, architectureSummary, moduleSummary string, onRetry func(attempt int, err error, backoff time.Duration)) (*MentionedFiles, error) {
	// 构建模板数据
	data := identifyTemplateData{
		Task:               task,
		CandidatePaths:     candidatePaths,
		ArchitectureSummary: architectureSummary,
		ModuleSummary:      moduleSummary,
	}

	// 渲染用户提示词
	userPrompt, err := renderIdentifyUserPrompt(data)
	if err != nil {
		return nil, fmt.Errorf("渲染文件识别提示词失败: %w", err)
	}

	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: templates.PackIdentifyFilesSystem},
			{Role: "user", Content: userPrompt},
		},
	}

	var out MentionedFiles
	if _, err := llm.ChatStructuredWithRetry(client, req, "pack_identify_files", &out, 3, onRetry); err != nil {
		return nil, err
	}

	// 校验和清理结果
	return validateAndCleanMentionedFiles(&out, candidatePaths, projectRoot)
}

// identifyTemplateData 是文件识别模板的数据结构
type identifyTemplateData struct {
	Task               string
	CandidatePaths     []string
	ArchitectureSummary string
	ModuleSummary      string
}

// renderIdentifyUserPrompt 渲染文件识别用户提示词
func renderIdentifyUserPrompt(data identifyTemplateData) (string, error) {
	tmpl, err := template.New("pack_identify_files_user").Parse(templates.PackIdentifyFilesUser)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// validateAndCleanMentionedFiles 校验 LLM 返回的文件路径并清理结果
func validateAndCleanMentionedFiles(mentioned *MentionedFiles, candidatePaths []string, projectRoot string) (*MentionedFiles, error) {
	// 构建候选路径集合用于快速查找
	candidateSet := make(map[string]bool, len(candidatePaths))
	for _, path := range candidatePaths {
		clean := filepath.Clean(strings.TrimSpace(path))
		if clean == "." || clean == "" {
			continue
		}
		candidateSet[clean] = true
	}

	validPaths := make([]string, 0, len(mentioned.Paths))
	validReasons := make(map[string]string)
	seen := make(map[string]bool)

	for _, path := range mentioned.Paths {
		cleanPath := filepath.Clean(strings.TrimSpace(path))
		if cleanPath == "." || cleanPath == "" {
			continue
		}
		if seen[cleanPath] {
			continue
		}
		if !candidateSet[cleanPath] {
			continue
		}
		if !fileutil.FileExists(filepath.Join(projectRoot, cleanPath)) {
			continue
		}

		seen[cleanPath] = true
		validPaths = append(validPaths, cleanPath)
		if reason, ok := mentioned.Reasons[path]; ok && strings.TrimSpace(reason) != "" {
			validReasons[cleanPath] = strings.TrimSpace(reason)
		} else {
			validReasons[cleanPath] = "LLM 识别为相关文件"
		}

		if len(validPaths) >= maxIdentifiedFiles {
			break
		}
	}

	return &MentionedFiles{
		Paths:   validPaths,
		Reasons: validReasons,
	}, nil
}