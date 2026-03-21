package updater

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/generator"
	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/internal/schema"
	"github.com/w1ndys/kontext/templates"
)

type yamlEnvelope struct {
	Content string `json:"content"`
}

// Executor 执行 update 计划。
type Executor struct {
	client      llm.Client
	kontextDir  string
	projectDir  string
	backupStamp string
}

// NewExecutor 创建一个 update 执行器。
func NewExecutor(client llm.Client, kontextDir, projectDir string) *Executor {
	return &Executor{
		client:      client,
		kontextDir:  kontextDir,
		projectDir:  projectDir,
		backupStamp: time.Now().Format("20060102-150405"),
	}
}

// Execute 执行更新计划。
func (e *Executor) Execute(report *ChangeReport, actions []UpdateAction) ([]string, error) {
	var updated []string
	for _, action := range actions {
		targetPath, err := e.targetPath(action)
		if err != nil {
			return updated, err
		}

		content, err := e.generateContent(report, action)
		if err != nil {
			return updated, fmt.Errorf("生成 %s 失败: %w", action.Target, err)
		}

		if err := e.applyAction(targetPath, action, content); err != nil {
			return updated, fmt.Errorf("应用 %s 失败: %w", action.Target, err)
		}
		updated = append(updated, targetPath)
	}

	if errs := schema.ValidateBundle(e.kontextDir); len(errs) > 0 {
		return updated, fmt.Errorf("更新后校验失败: %v", errs[0])
	}

	if err := e.pruneBackups(); err != nil {
		return updated, err
	}

	sort.Strings(updated)
	return updated, nil
}

func (e *Executor) generateContent(report *ChangeReport, action UpdateAction) (string, error) {
	currentPath, err := e.targetPath(action)
	if err != nil {
		return "", err
	}
	currentYAML := readTextIfExists(currentPath)

	userPrompt, allowEmpty, err := e.renderUserPrompt(report, action, currentYAML)
	if err != nil {
		return "", err
	}

	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: templates.UpdateSystem},
			{Role: "user", Content: userPrompt},
		},
	}

	var lastValidationErr error
	for semanticAttempt := 0; semanticAttempt < 2; semanticAttempt++ {
		resp, content, err := e.generateYAMLEnvelope(req)
		if err != nil {
			return "", err
		}

		if content == "" && allowEmpty {
			return "", nil
		}

		if validateErr := generator.ValidateYAML(content); validateErr == nil {
			return content, nil
		} else {
			lastValidationErr = validateErr
			req.Messages = append(req.Messages,
				llm.Message{Role: "assistant", Content: resp.Content},
				llm.Message{Role: "user", Content: fmt.Sprintf("上一次返回的 YAML 无法解析：%v。请保持最小修改并返回合法 YAML。", validateErr)},
			)
		}
	}

	return "", fmt.Errorf("LLM 返回的 YAML 仍不合法: %w", lastValidationErr)
}

func (e *Executor) generateYAMLEnvelope(req *llm.ChatRequest) (*llm.ChatResponse, string, error) {
	var out yamlEnvelope
	resp, err := llm.ChatStructuredWithRetry(e.client, req, "updated_yaml", &out, 3, nil)
	if err == nil {
		return resp, strings.TrimSpace(out.Content), nil
	}
	if !llm.IsStructuredOutputError(err) {
		return nil, "", err
	}

	fallbackResp, fallbackContent, fallbackErr := e.generateYAMLEnvelopeLegacy(req)
	if fallbackErr != nil {
		return nil, "", fmt.Errorf("结构化输出失败: %v；回退到传统 JSON 模式也失败: %w", err, fallbackErr)
	}
	return fallbackResp, fallbackContent, nil
}

func (e *Executor) generateYAMLEnvelopeLegacy(req *llm.ChatRequest) (*llm.ChatResponse, string, error) {
	resp, err := llm.ChatWithRetry(e.client, req, 3, nil)
	if err != nil {
		return nil, "", err
	}

	content, err := parseYAMLEnvelope(resp.Content)
	if err == nil {
		return resp, content, nil
	}

	retryReq := &llm.ChatRequest{Messages: append([]llm.Message{}, req.Messages...)}
	retryReq.Messages = append(retryReq.Messages,
		llm.Message{Role: "assistant", Content: resp.Content},
		llm.Message{Role: "user", Content: fmt.Sprintf("上次返回的 JSON 格式不正确，错误: %v。请重新返回合法 JSON，且只包含 content 字段。", err)},
	)

	retryResp, retryErr := llm.ChatWithRetry(e.client, retryReq, 3, nil)
	if retryErr != nil {
		return nil, "", retryErr
	}

	content, err = parseYAMLEnvelope(retryResp.Content)
	if err != nil {
		return nil, "", fmt.Errorf("解析传统 JSON 响应失败: %w", err)
	}
	return retryResp, content, nil
}

func (e *Executor) renderUserPrompt(report *ChangeReport, action UpdateAction, currentYAML string) (string, bool, error) {
	switch action.Target {
	case "architecture":
		prompt, err := generator.RenderTemplate(templates.UpdateArchitecture, map[string]any{
			"CurrentYAML": fallbackYAML(currentYAML),
			"Changes":     formatDirectoryChanges(report.DirectoryChanges),
			"Packages":    strings.Join(report.PackagePaths, "\n"),
		})
		return prompt, false, err
	case "manifest":
		prompt, err := generator.RenderTemplate(templates.UpdateManifest, map[string]any{
			"CurrentYAML": fallbackYAML(currentYAML),
			"Reasons":     strings.Join(report.ManifestReasons, "\n"),
			"Signals":     formatManifestSignals(report),
		})
		return prompt, false, err
	default:
		if strings.HasPrefix(action.Target, "contract:") {
			moduleName := action.Module
			prompt, err := generator.RenderTemplate(templates.UpdateContract, map[string]any{
				"ModuleName":  moduleName,
				"CurrentYAML": fallbackYAML(currentYAML),
				"Changes":     formatContractChanges(report.ContractChanges, moduleName),
				"CodeSummary": fallbackText(report.ModuleSummaries[moduleName], "当前没有可用的代码摘要"),
			})
			return prompt, action.ChangeType == "deleted_module", err
		}
	}
	return "", false, fmt.Errorf("不支持的更新目标: %s", action.Target)
}

func (e *Executor) applyAction(targetPath string, action UpdateAction, content string) error {
	if err := e.backupIfExists(targetPath); err != nil {
		return err
	}

	if strings.HasPrefix(action.Target, "contract:") && action.ChangeType == "deleted_module" && strings.TrimSpace(content) == "" {
		if fileutil.FileExists(targetPath) {
			return os.Remove(targetPath)
		}
		return nil
	}

	return fileutil.WriteFile(targetPath, []byte(content))
}

func (e *Executor) backupIfExists(path string) error {
	if !fileutil.FileExists(path) {
		return nil
	}

	relPath, err := filepath.Rel(e.kontextDir, path)
	if err != nil {
		return err
	}
	backupPath := filepath.Join(e.kontextDir, ".backup", e.backupStamp, relPath)
	data, err := fileutil.ReadFile(path)
	if err != nil {
		return err
	}
	return fileutil.WriteFile(backupPath, data)
}

func (e *Executor) pruneBackups() error {
	backupRoot := filepath.Join(e.kontextDir, ".backup")
	if !fileutil.DirExists(backupRoot) {
		return nil
	}

	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		return err
	}
	if len(entries) <= 5 {
		return nil
	}

	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	sort.Strings(dirs)
	for len(dirs) > 5 {
		oldest := filepath.Join(backupRoot, dirs[0])
		if err := os.RemoveAll(oldest); err != nil {
			return err
		}
		dirs = dirs[1:]
	}
	return nil
}

func (e *Executor) targetPath(action UpdateAction) (string, error) {
	switch action.Target {
	case "architecture":
		return filepath.Join(e.kontextDir, "ARCHITECTURE_MAP.yaml"), nil
	case "manifest":
		return filepath.Join(e.kontextDir, "PROJECT_MANIFEST.yaml"), nil
	default:
		if strings.HasPrefix(action.Target, "contract:") {
			return filepath.Join(e.kontextDir, "module_contracts", fmt.Sprintf("%s_CONTRACT.yaml", action.Module)), nil
		}
	}
	return "", fmt.Errorf("未知目标: %s", action.Target)
}

func formatDirectoryChanges(changes []DirectoryChange) string {
	if len(changes) == 0 {
		return "没有检测到目录结构变化"
	}

	var lines []string
	for _, change := range changes {
		lines = append(lines, fmt.Sprintf("- %s: %s", change.Type, change.Path))
	}
	return strings.Join(lines, "\n")
}

func formatContractChanges(changes []ContractChange, moduleName string) string {
	var lines []string
	for _, change := range changes {
		if change.Module != moduleName {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", change.Type, change.Details))
	}
	if len(lines) == 0 {
		return "没有检测到直接变化，但用户要求更新该契约"
	}
	return strings.Join(lines, "\n")
}

func formatManifestSignals(report *ChangeReport) string {
	var lines []string
	for _, reason := range report.ManifestReasons {
		lines = append(lines, "- "+reason)
	}
	if len(report.GitChangedFiles) > 0 {
		lines = append(lines, "变更文件:")
		for _, file := range report.GitChangedFiles {
			lines = append(lines, "- "+file)
		}
	}
	if len(lines) == 0 {
		return "没有额外信号"
	}
	return strings.Join(lines, "\n")
}

func fallbackYAML(content string) string {
	if strings.TrimSpace(content) == "" {
		return "# 当前文件为空"
	}
	return content
}

func fallbackText(content, fallback string) string {
	if strings.TrimSpace(content) == "" {
		return fallback
	}
	return content
}

func readTextIfExists(path string) string {
	if !fileutil.FileExists(path) {
		return ""
	}
	data, err := fileutil.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func parseYAMLEnvelope(raw string) (string, error) {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		if len(lines) >= 2 && strings.HasPrefix(lines[0], "```") {
			lines = lines[1:]
			if n := len(lines); n > 0 && strings.TrimSpace(lines[n-1]) == "```" {
				lines = lines[:n-1]
			}
			cleaned = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}

	var out yamlEnvelope
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Content), nil
}
