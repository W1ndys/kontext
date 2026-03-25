package packer

import (
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/internal/schema"
	"github.com/w1ndys/kontext/templates"
)

// CandidateFile 是发送给精筛模型的候选文件。
type CandidateFile struct {
	Path    string `yaml:"path" json:"path"`
	Summary string `yaml:"summary" json:"summary"`
}

// FileRelevance 描述单个文件与任务的相关度。
type FileRelevance struct {
	Path       string   `yaml:"path" json:"path"`
	Relevance  string   `yaml:"relevance" json:"relevance"`
	Reason     string   `yaml:"reason" json:"reason"`
	FocusAreas []string `yaml:"focus_areas" json:"focus_areas"`
}

// RefineResult 是 LLM 精筛后的结构化结果。
type RefineResult struct {
	RelevantFiles []FileRelevance `yaml:"relevant_files" json:"relevant_files"`
}

type refineTemplateData struct {
	Task            string
	Candidates      []CandidateFile
	Contracts       []schema.ModuleContract
	IdentifiedFiles []IdentifiedFile
}

// RefineContext 使用 LLM 对候选文件做二次精筛。
func RefineContext(client llm.Client, task string, candidates []CandidateFile, contracts []schema.ModuleContract, identifiedFiles []IdentifiedFile, onRetry func(attempt int, err error, backoff time.Duration)) (*RefineResult, error) {
	userPrompt, err := renderPackRefineUserPrompt(refineTemplateData{
		Task:            task,
		Candidates:      candidates,
		Contracts:       contracts,
		IdentifiedFiles: identifiedFiles,
	})
	if err != nil {
		return nil, fmt.Errorf("渲染精筛提示词失败: %w", err)
	}

	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: templates.PackRefineSystem},
			{Role: "user", Content: userPrompt},
		},
	}

	var out RefineResult
	if _, err := llm.ChatStructuredWithRetry(client, req, "pack_refine_result", &out, 3, onRetry); err != nil {
		return nil, err
	}

	filtered := make([]FileRelevance, 0, len(out.RelevantFiles))
	for _, file := range out.RelevantFiles {
		switch strings.ToLower(strings.TrimSpace(file.Relevance)) {
		case "high", "medium":
			file.Relevance = strings.ToLower(strings.TrimSpace(file.Relevance))
			filtered = append(filtered, file)
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Relevance == filtered[j].Relevance {
			return filtered[i].Path < filtered[j].Path
		}
		return filtered[i].Relevance == "high"
	})

	return &RefineResult{RelevantFiles: filtered}, nil
}

// 渲染精筛用户提示词模板
func renderPackRefineUserPrompt(data refineTemplateData) (string, error) {
	tmpl, err := template.New("pack_refine_user").Parse(templates.PackRefineUser)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
