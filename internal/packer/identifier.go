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
	var batchResults []*MentionedFiles
	for start := 0; start < len(candidatePaths); start += maxIdentifyCandidateBatchSize {
		end := min(start+maxIdentifyCandidateBatchSize, len(candidatePaths))
		batch := candidatePaths[start:end]

		out, err := identifyRelevantFilesBatch(client, task, batch, projectRoot, architectureSummary, moduleSummary, onRetry)
		if err != nil {
			return nil, err
		}
		batchResults = append(batchResults, out)
	}

	return mergeMentionedFiles(batchResults), nil
}

// 合并多个批次的文件识别结果，去重并限制最大数量
func mergeMentionedFiles(results []*MentionedFiles) *MentionedFiles {
	merged := &MentionedFiles{
		Paths:   make([]string, 0, maxIdentifiedFiles),
		Reasons: make(map[string]string),
	}
	seen := make(map[string]bool)

	for _, result := range results {
		if result == nil {
			continue
		}
		for _, path := range result.Paths {
			if seen[path] {
				continue
			}
			seen[path] = true
			merged.Paths = append(merged.Paths, path)
			if reason := strings.TrimSpace(result.Reasons[path]); reason != "" {
				merged.Reasons[path] = reason
			} else {
				merged.Reasons[path] = defaultIdentifiedReason
			}
			if len(merged.Paths) >= maxIdentifiedFiles {
				return merged
			}
		}
	}
	return merged
}

// 对单个批次的候选文件调用 LLM 进行相关性识别
func identifyRelevantFilesBatch(client llm.Client, task string, candidatePaths []string, projectRoot, architectureSummary, moduleSummary string, onRetry func(attempt int, err error, backoff time.Duration)) (*MentionedFiles, error) {
	data := identifyTemplateData{
		Task:                task,
		CandidatePaths:      candidatePaths,
		ArchitectureSummary: architectureSummary,
		ModuleSummary:       moduleSummary,
	}

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

	return validateAndCleanMentionedFiles(&out, candidatePaths, projectRoot)
}

// identifyTemplateData 是文件识别模板的数据结构
type identifyTemplateData struct {
	Task                string
	CandidatePaths      []string
	ArchitectureSummary string
	ModuleSummary       string
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
			validReasons[cleanPath] = defaultIdentifiedReason
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
