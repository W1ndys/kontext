package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/generator"
	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/internal/schema"
	"github.com/w1ndys/kontext/templates"
	"go.yaml.in/yaml/v4"
)

const (
	contractUpdateConcurrency = 4
	llmProgressInterval       = 15 * time.Second
)

type actionResult struct {
	index      int
	targetPath string
	err        error
}

// Executor 执行 update 计划。
type Executor struct {
	client      llm.Client
	kontextDir  string
	projectDir  string
	backupStamp string
	onProgress  func(ProgressEvent)
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

// SetProgressHandler 设置 update 执行进度回调。
func (e *Executor) SetProgressHandler(handler func(ProgressEvent)) {
	e.onProgress = handler
}

// Execute 执行更新计划。
func (e *Executor) Execute(report *ChangeReport, actions []UpdateAction) ([]string, error) {
	var updated []string
	for i := 0; i < len(actions); {
		if isContractAction(actions[i]) {
			j := i
			for j < len(actions) && isContractAction(actions[j]) {
				j++
			}

			batchUpdated, err := e.executeContractBatch(report, actions[i:j], i, len(actions))
			updated = append(updated, batchUpdated...)
			if err != nil {
				return updated, err
			}
			i = j
			continue
		}

		targetPath, err := e.targetPath(actions[i])
		if err != nil {
			return updated, err
		}
		actionStart := time.Now()

		e.emitProgress(ProgressEvent{
			Stage:      ProgressActionStart,
			Action:     actions[i],
			Index:      i + 1,
			Total:      len(actions),
			TargetPath: targetPath,
		})

		content, err := e.generateContent(report, actions[i], i+1, len(actions), targetPath)
		if err != nil {
			return updated, fmt.Errorf("生成 %s 失败: %w", actions[i].Target, err)
		}

		if err := e.applyAction(targetPath, actions[i], content); err != nil {
			return updated, fmt.Errorf("应用 %s 失败: %w", actions[i].Target, err)
		}
		updated = append(updated, targetPath)

		e.emitProgress(ProgressEvent{
			Stage:      ProgressActionDone,
			Action:     actions[i],
			Index:      i + 1,
			Total:      len(actions),
			TargetPath: targetPath,
			Message:    formatElapsedDuration(time.Since(actionStart)),
		})
		i++
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

// executeContractBatch 并行执行一批模块契约的更新动作。
func (e *Executor) executeContractBatch(report *ChangeReport, actions []UpdateAction, start, total int) ([]string, error) {
	results := make(chan actionResult, len(actions))
	sem := make(chan struct{}, contractUpdateConcurrency)
	var wg sync.WaitGroup

	for i, action := range actions {
		globalIndex := start + i + 1
		wg.Add(1)
		go func(action UpdateAction, index int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			targetPath, err := e.targetPath(action)
			if err != nil {
				results <- actionResult{index: index, err: err}
				return
			}
			actionStart := time.Now()

			e.emitProgress(ProgressEvent{
				Stage:      ProgressActionStart,
				Action:     action,
				Index:      index,
				Total:      total,
				TargetPath: targetPath,
			})

			content, err := e.generateContent(report, action, index, total, targetPath)
			if err != nil {
				results <- actionResult{index: index, err: fmt.Errorf("生成 %s 失败: %w", action.Target, err)}
				return
			}

			if err := e.applyAction(targetPath, action, content); err != nil {
				results <- actionResult{index: index, err: fmt.Errorf("应用 %s 失败: %w", action.Target, err)}
				return
			}

			e.emitProgress(ProgressEvent{
				Stage:      ProgressActionDone,
				Action:     action,
				Index:      index,
				Total:      total,
				TargetPath: targetPath,
				Message:    formatElapsedDuration(time.Since(actionStart)),
			})
			results <- actionResult{index: index, targetPath: targetPath}
		}(action, globalIndex)
	}

	wg.Wait()
	close(results)

	var updated []string
	var firstErr error
	firstErrIndex := total + 1
	for result := range results {
		if result.targetPath != "" {
			updated = append(updated, result.targetPath)
		}
		if result.err != nil && result.index < firstErrIndex {
			firstErr = result.err
			firstErrIndex = result.index
		}
	}

	return updated, firstErr
}

// generateContent 为单个更新动作生成新的 YAML 内容，含 LLM 调用和语义校验。
func (e *Executor) generateContent(report *ChangeReport, action UpdateAction, index, total int, targetPath string) (string, error) {
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
		e.emitProgress(ProgressEvent{
			Stage:      ProgressLLMStart,
			Action:     action,
			Index:      index,
			Total:      total,
			TargetPath: targetPath,
		})

		stopHeartbeat := e.startLLMHeartbeat(action, index, total, targetPath)
		resp, content, err := e.generateYAMLContent(req, action, index, total, targetPath)
		stopHeartbeat()
		if err != nil {
			return "", err
		}

		if content == "" && allowEmpty {
			return "", nil
		}

		if validateErr := validateGeneratedContent(action, content); validateErr == nil {
			return content, nil
		} else {
			lastValidationErr = validateErr
			e.emitProgress(ProgressEvent{
				Stage:      ProgressYAMLRetry,
				Action:     action,
				Index:      index,
				Total:      total,
				TargetPath: targetPath,
				Message:    validateErr.Error(),
			})
			req.Messages = append(req.Messages,
				llm.Message{Role: "assistant", Content: resp.Content},
				llm.Message{Role: "user", Content: fmt.Sprintf("上一次返回的 YAML 无法解析：%v。请保持最小修改，直接返回完整、合法的 YAML 文本，不要使用 JSON 包装。", validateErr)},
			)
		}
	}

	return "", fmt.Errorf("LLM 返回的 YAML 仍不合法: %w", lastValidationErr)
}

// generateYAMLContent 调用 LLM 生成 YAML 内容，直接返回纯 YAML 文本。
func (e *Executor) generateYAMLContent(req *llm.ChatRequest, action UpdateAction, index, total int, targetPath string) (*llm.ChatResponse, string, error) {
	resp, err := llm.ChatWithRetry(e.client, req, 3, nil)
	if err != nil {
		return nil, "", fmt.Errorf("调用 LLM 生成 YAML 失败: %w", err)
	}

	content := extractYAMLContent(resp.Content)
	return resp, content, nil
}

// extractYAMLContent 从 LLM 响应中提取纯 YAML 内容。
// 去除 markdown 代码块包裹，返回清理后的 YAML 文本。
func extractYAMLContent(raw string) string {
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
	return cleaned
}

// emitProgress 触发进度回调通知。
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

// applyAction 将生成的内容应用到目标文件（含备份和删除逻辑）。
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

// backupIfExists 如果目标文件存在则备份到 .backup/ 目录。
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

// pruneBackups 清理超过 5 个的旧备份目录。
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

// targetPath 根据更新动作类型返回目标文件路径。
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

// formatDirectoryChanges 将目录变更列表格式化为文本。
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

// formatContractChanges 将指定模块的契约变更格式化为文本。
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

// formatManifestSignals 将 Manifest 更新信号格式化为文本。
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

// fallbackYAML 若内容为空则返回占位注释。
func fallbackYAML(content string) string {
	if strings.TrimSpace(content) == "" {
		return "# 当前文件为空"
	}
	return content
}

// fallbackText 若内容为空则返回指定的回退文本。
func fallbackText(content, fallback string) string {
	if strings.TrimSpace(content) == "" {
		return fallback
	}
	return content
}

// readTextIfExists 读取文件内容，文件不存在或读取失败时返回空字符串。
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

// emitProgress 触发进度回调通知。
func (e *Executor) emitProgress(event ProgressEvent) {
	if e.onProgress != nil {
		e.onProgress(event)
	}
}

// validateGeneratedContent 校验生成的 YAML 内容是否合法且符合对应结构。
func validateGeneratedContent(action UpdateAction, content string) error {
	if err := generator.ValidateYAML(content); err != nil {
		return err
	}

	switch {
	case action.Target == "manifest":
		var manifest schema.ProjectManifest
		if err := yaml.Unmarshal([]byte(content), &manifest); err != nil {
			return fmt.Errorf("manifest 结构不合法: %w", err)
		}
		if strings.TrimSpace(manifest.Project.Name) == "" {
			return fmt.Errorf("manifest 结构不合法: project.name 不能为空")
		}
	case action.Target == "architecture":
		var arch schema.ArchitectureMap
		if err := yaml.Unmarshal([]byte(content), &arch); err != nil {
			return fmt.Errorf("architecture 结构不合法: %w", err)
		}
	case strings.HasPrefix(action.Target, "contract:"):
		var contract schema.ModuleContract
		if err := yaml.Unmarshal([]byte(content), &contract); err != nil {
			return fmt.Errorf("contract 结构不合法: %w", err)
		}
		if err := contract.Validate(); err != nil {
			return fmt.Errorf("contract 结构不合法: %w", err)
		}
	}

	return nil
}

// isContractAction 判断更新动作是否为契约类型。
func isContractAction(action UpdateAction) bool {
	return strings.HasPrefix(action.Target, "contract:")
}

// startLLMHeartbeat 启动 LLM 调用的心跳定时器，定期发送进度事件。
func (e *Executor) startLLMHeartbeat(action UpdateAction, index, total int, targetPath string) func() {
	done := make(chan struct{})
	var once sync.Once
	start := time.Now()

	go func() {
		ticker := time.NewTicker(llmProgressInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				e.emitProgress(ProgressEvent{
					Stage:      ProgressLLMTick,
					Action:     action,
					Index:      index,
					Total:      total,
					TargetPath: targetPath,
					Message:    formatElapsedDuration(time.Since(start)),
				})
			}
		}
	}()

	return func() {
		once.Do(func() {
			close(done)
		})
	}
}

// formatElapsedDuration 将耗时格式化为 "Ns" 字符串。
func formatElapsedDuration(elapsed time.Duration) string {
	seconds := int(elapsed.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("%ds", seconds)
}
