package updater

import (
	"bytes"
	"encoding/json"
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
)

const (
	llmProgressInterval = 15 * time.Second
)

type actionResult struct {
	index      int
	targetPath string
	err        error
}

// UpdatedContent 是 LLM 更新制品时返回的 JSON 结构。
type UpdatedContent struct {
	Content string `json:"content"` // 更新后的 JSON 文本
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
	var wg sync.WaitGroup

	for i, action := range actions {
		globalIndex := start + i + 1

		targetPath, err := e.targetPath(action)
		if err != nil {
			results <- actionResult{index: globalIndex, err: err}
			continue
		}

		// 已删除模块直接删除文件，无需调用 LLM
		if action.ChangeType == "deleted_module" {
			e.emitProgress(ProgressEvent{
				Stage:      ProgressActionStart,
				Action:     action,
				Index:      globalIndex,
				Total:      total,
				TargetPath: targetPath,
			})
			_ = e.backupIfExists(targetPath)
			if fileutil.FileExists(targetPath) {
				_ = os.Remove(targetPath)
			}
			e.emitProgress(ProgressEvent{
				Stage:      ProgressActionDone,
				Action:     action,
				Index:      globalIndex,
				Total:      total,
				TargetPath: targetPath,
				Message:    "已删除",
			})
			results <- actionResult{index: globalIndex, targetPath: targetPath}
			continue
		}

		// 按顺序发出启动日志，保证日志序号有序
		e.emitProgress(ProgressEvent{
			Stage:      ProgressActionStart,
			Action:     action,
			Index:      globalIndex,
			Total:      total,
			TargetPath: targetPath,
		})

		wg.Add(1)
		go func(action UpdateAction, index int, targetPath string) {
			defer wg.Done()
			actionStart := time.Now()

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
		}(action, globalIndex, targetPath)
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

// generateContent 为单个更新动作生成新的 JSON 内容，含 LLM 调用和语义校验。
func (e *Executor) generateContent(report *ChangeReport, action UpdateAction, index, total int, targetPath string) (string, error) {
	// 契约类型使用分段生成策略，避免单次输出过长被截断
	if strings.HasPrefix(action.Target, "contract:") {
		return e.generateContractInParts(report, action, index, total, targetPath)
	}

	// architecture 类型使用分段生成策略，避免大项目输出截断
	if action.Target == "architecture" {
		return e.generateArchitectureInParts(report, action, index, total, targetPath)
	}

	currentPath, err := e.targetPath(action)
	if err != nil {
		return "", err
	}
	currentJSON := readTextIfExists(currentPath)

	userPrompt, allowEmpty, err := e.renderUserPrompt(report, action, currentJSON)
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
		resp, content, err := e.generateJSONContent(req)
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
				Stage:      ProgressJSONRetry,
				Action:     action,
				Index:      index,
				Total:      total,
				TargetPath: targetPath,
				Message:    validateErr.Error(),
			})
			req.Messages = append(req.Messages,
				llm.Message{Role: "assistant", Content: resp.Content},
				llm.Message{Role: "user", Content: fmt.Sprintf("上一次返回的内容无法解析：%v。请保持最小修改，以 JSON 格式返回 {\"content\": \"完整、合法的 JSON 文本\"}。", validateErr)},
			)
		}
	}

	return "", fmt.Errorf("LLM 返回的内容仍不合法: %w", lastValidationErr)
}

// generateArchitectureInParts 分两段生成 ARCHITECTURE_MAP JSON，避免单次输出过长被截断。
// Part 1: layers（层级与包列表，通常是输出的主体部分）
// Part 2: rules（架构规则，基于 Part 1 的层级结构生成）
// 每段均支持语义修正重试：若 LLM 返回的 JSON 解析失败，将错误追加到对话让 LLM 修正。
func (e *Executor) generateArchitectureInParts(report *ChangeReport, action UpdateAction, index, total int, targetPath string) (string, error) {
	currentPath, err := e.targetPath(action)
	if err != nil {
		return "", err
	}
	currentJSON := readTextIfExists(currentPath)
	changes := formatDirectoryChanges(report.DirectoryChanges)
	packages := strings.Join(report.PackagePaths, "\n")

	// Part 1: layers
	part1Prompt, err := generator.RenderTemplate(templates.UpdateArchitecturePart1, map[string]any{
		"CurrentJSON": fallbackJSON(currentJSON),
		"Changes":     changes,
		"Packages":    packages,
	})
	if err != nil {
		return "", fmt.Errorf("渲染 Architecture Part1 模板失败: %w", err)
	}

	e.emitProgress(ProgressEvent{
		Stage:      ProgressLLMStart,
		Action:     action,
		Index:      index,
		Total:      total,
		TargetPath: targetPath,
		Message:    "生成架构第 1/2 部分（layers）",
	})

	part1Content, err := e.generateContractPartWithCorrection(
		templates.UpdateSystem, part1Prompt, action, index, total, targetPath,
	)
	if err != nil {
		return "", fmt.Errorf("生成 Architecture Part1（layers）失败: %w", err)
	}

	// Part 2: rules
	part2Prompt, err := generator.RenderTemplate(templates.UpdateArchitecturePart2, map[string]any{
		"LayersJSON":  part1Content,
		"CurrentJSON": fallbackJSON(currentJSON),
		"Changes":     changes,
	})
	if err != nil {
		return "", fmt.Errorf("渲染 Architecture Part2 模板失败: %w", err)
	}

	e.emitProgress(ProgressEvent{
		Stage:      ProgressLLMStart,
		Action:     action,
		Index:      index,
		Total:      total,
		TargetPath: targetPath,
		Message:    "生成架构第 2/2 部分（rules）",
	})

	part2Content, err := e.generateContractPartWithCorrection(
		templates.UpdateSystem, part2Prompt, action, index, total, targetPath,
	)
	if err != nil {
		return "", fmt.Errorf("生成 Architecture Part2（rules）失败: %w", err)
	}

	// 拼接两段 JSON 并校验
	merged := strings.TrimRight(part1Content, "\n") + "\n\n" +
		strings.TrimLeft(part2Content, "\n")

	// 尝试直接校验拼接结果
	if validateErr := validateGeneratedContent(action, merged); validateErr != nil {
		// 拼接结果不合法，尝试将多个 JSON 对象合并为一个
		mergedJSON, mergeErr := mergeJSONObjects(merged)
		if mergeErr != nil {
			return "", fmt.Errorf("分段拼接后的架构校验失败: %w", validateErr)
		}
		if validateErr2 := validateGeneratedContent(action, mergedJSON); validateErr2 != nil {
			return "", fmt.Errorf("分段拼接后的架构校验失败: %w", validateErr2)
		}
		return mergedJSON, nil
	}

	return merged, nil
}

// generateContractInParts 分三段生成模块契约 JSON，避免单次输出过长被截断。
// Part 1: module + owns + not_responsible_for + depends_on
// Part 2a: public_interface
// Part 2b: modification_rules
// 每段均支持语义修正重试：若 LLM 返回的 JSON 解析失败，将错误追加到对话让 LLM 修正。
func (e *Executor) generateContractInParts(report *ChangeReport, action UpdateAction, index, total int, targetPath string) (string, error) {
	currentPath, err := e.targetPath(action)
	if err != nil {
		return "", err
	}
	currentJSON := readTextIfExists(currentPath)
	moduleName := action.Module
	changes := formatContractChanges(report.ContractChanges, moduleName)
	codeSummary := fallbackText(report.ModuleSummaries[moduleName], "当前没有可用的代码摘要")

	// 对于已删除模块，直接使用原有的完整模板（输出为空字符串，无需分段）
	if action.ChangeType == "deleted_module" {
		return e.generateContractSinglePass(report, action, index, total, targetPath, currentJSON)
	}

	// Part 1: module + owns + not_responsible_for + depends_on
	part1Prompt, err := generator.RenderTemplate(templates.UpdateContractPart1, map[string]any{
		"ModuleName":  moduleName,
		"CurrentJSON": fallbackJSON(currentJSON),
		"Changes":     changes,
		"CodeSummary": codeSummary,
	})
	if err != nil {
		return "", fmt.Errorf("渲染 Part1 模板失败: %w", err)
	}

	e.emitProgress(ProgressEvent{
		Stage:      ProgressLLMStart,
		Action:     action,
		Index:      index,
		Total:      total,
		TargetPath: targetPath,
		Message:    "生成契约第 1/3 部分",
	})

	part1Content, err := e.generateContractPartWithCorrection(
		templates.UpdateSystem, part1Prompt, action, index, total, targetPath,
	)
	if err != nil {
		return "", fmt.Errorf("生成契约 Part1 失败: %w", err)
	}

	// 对于已删除模块返回空内容的情况
	if strings.TrimSpace(part1Content) == "" && action.ChangeType == "deleted_module" {
		return "", nil
	}

	// Part 2a: public_interface
	part2aPrompt, err := generator.RenderTemplate(templates.UpdateContractPart2a, map[string]any{
		"ModuleName":  moduleName,
		"Part1JSON":   part1Content,
		"CurrentJSON": fallbackJSON(currentJSON),
		"CodeSummary": codeSummary,
	})
	if err != nil {
		return "", fmt.Errorf("渲染 Part2a 模板失败: %w", err)
	}

	e.emitProgress(ProgressEvent{
		Stage:      ProgressLLMStart,
		Action:     action,
		Index:      index,
		Total:      total,
		TargetPath: targetPath,
		Message:    "生成契约第 2/3 部分",
	})

	part2aContent, err := e.generateContractPartWithCorrection(
		templates.UpdateSystem, part2aPrompt, action, index, total, targetPath,
	)
	if err != nil {
		return "", fmt.Errorf("生成契约 Part2a 失败: %w", err)
	}

	// Part 2b: modification_rules
	precedingJSON := strings.TrimRight(part1Content, "\n") + "\n\n" + strings.TrimLeft(part2aContent, "\n")
	part2bPrompt, err := generator.RenderTemplate(templates.UpdateContractPart2b, map[string]any{
		"ModuleName":    moduleName,
		"PrecedingJSON": precedingJSON,
		"CurrentJSON":   fallbackJSON(currentJSON),
	})
	if err != nil {
		return "", fmt.Errorf("渲染 Part2b 模板失败: %w", err)
	}

	e.emitProgress(ProgressEvent{
		Stage:      ProgressLLMStart,
		Action:     action,
		Index:      index,
		Total:      total,
		TargetPath: targetPath,
		Message:    "生成契约第 3/3 部分",
	})

	part2bContent, err := e.generateContractPartWithCorrection(
		templates.UpdateSystem, part2bPrompt, action, index, total, targetPath,
	)
	if err != nil {
		return "", fmt.Errorf("生成契约 Part2b 失败: %w", err)
	}

	// 拼接三段 JSON 并校验
	merged := strings.TrimRight(part1Content, "\n") + "\n\n" +
		strings.TrimLeft(strings.TrimRight(part2aContent, "\n"), "\n") + "\n\n" +
		strings.TrimLeft(part2bContent, "\n")

	// 尝试直接校验拼接结果
	if validateErr := validateGeneratedContent(action, merged); validateErr != nil {
		// 拼接结果不合法，尝试将多个 JSON 对象合并为一个
		mergedJSON, mergeErr := mergeJSONObjects(merged)
		if mergeErr != nil {
			return "", fmt.Errorf("分段拼接后的契约校验失败: %w", validateErr)
		}
		if validateErr2 := validateGeneratedContent(action, mergedJSON); validateErr2 != nil {
			return "", fmt.Errorf("分段拼接后的契约校验失败: %w", validateErr2)
		}
		return mergedJSON, nil
	}

	return merged, nil
}

// generateContractPartWithCorrection 调用 LLM 生成契约的某一段 JSON，
// 若 JSON 解析失败则追加错误到对话让 LLM 修正，最多修正 2 次。
func (e *Executor) generateContractPartWithCorrection(systemPrompt, userPrompt string, action UpdateAction, index, total int, targetPath string) (string, error) {
	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		stopHeartbeat := e.startLLMHeartbeat(action, index, total, targetPath)
		var result UpdatedContent
		resp, err := llm.ChatStructuredWithRetry(e.client, req, "updated_content", &result, 1, nil)
		stopHeartbeat()

		if err == nil {
			return result.Content, nil
		}

		lastErr = err

		// 构造修正消息：将错误信息追加到对话，让 LLM 修正 JSON 格式
		// 即使拿不到原始响应，也可以通过追加错误提示让 LLM 重新生成
		e.emitProgress(ProgressEvent{
			Stage:      ProgressJSONRetry,
			Action:     action,
			Index:      index,
			Total:      total,
			TargetPath: targetPath,
			Message:    fmt.Sprintf("第 %d 次修正: %v", attempt+1, err),
		})

		// 若有原始响应则追加为 assistant 消息，否则仅追加用户修正提示
		if resp != nil && resp.Content != "" {
			req.Messages = append(req.Messages,
				llm.Message{Role: "assistant", Content: resp.Content},
			)
		}
		req.Messages = append(req.Messages,
			llm.Message{Role: "user", Content: fmt.Sprintf(
				"上一次返回的 JSON 解析失败：%v。请重新以合法 JSON 格式返回 {\"content\": \"JSON 文本\"}，注意 content 值中的双引号和特殊字符必须正确转义。",
				err,
			)},
		)
	}

	return "", fmt.Errorf("调用 LLM 生成内容失败: %w", lastErr)
}

// generateContractSinglePass 使用原有的完整模板生成契约（用于已删除模块等简单场景）。
func (e *Executor) generateContractSinglePass(report *ChangeReport, action UpdateAction, index, total int, targetPath, currentJSON string) (string, error) {
	moduleName := action.Module
	prompt, err := generator.RenderTemplate(templates.UpdateContract, map[string]any{
		"ModuleName":  moduleName,
		"CurrentJSON": fallbackJSON(currentJSON),
		"Changes":     formatContractChanges(report.ContractChanges, moduleName),
		"CodeSummary": fallbackText(report.ModuleSummaries[moduleName], "当前没有可用的代码摘要"),
	})
	if err != nil {
		return "", err
	}

	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: templates.UpdateSystem},
			{Role: "user", Content: prompt},
		},
	}

	e.emitProgress(ProgressEvent{
		Stage:      ProgressLLMStart,
		Action:     action,
		Index:      index,
		Total:      total,
		TargetPath: targetPath,
	})

	stopHeartbeat := e.startLLMHeartbeat(action, index, total, targetPath)
	_, content, err := e.generateJSONContent(req)
	stopHeartbeat()
	if err != nil {
		return "", err
	}

	if content == "" && action.ChangeType == "deleted_module" {
		return "", nil
	}

	if validateErr := validateGeneratedContent(action, content); validateErr != nil {
		return "", validateErr
	}

	return content, nil
}

// generateJSONContent 调用 LLM 生成 JSON 内容，通过 JSON 结构化输出返回。
func (e *Executor) generateJSONContent(req *llm.ChatRequest) (*llm.ChatResponse, string, error) {
	var result UpdatedContent
	resp, err := llm.ChatStructuredWithRetry(e.client, req, "updated_content", &result, 3, nil)
	if err != nil {
		return nil, "", fmt.Errorf("调用 LLM 生成内容失败: %w", err)
	}

	return resp, result.Content, nil
}

// emitProgress 触发进度回调通知。
func (e *Executor) renderUserPrompt(report *ChangeReport, action UpdateAction, currentJSON string) (string, bool, error) {
	switch action.Target {
	case "architecture":
		prompt, err := generator.RenderTemplate(templates.UpdateArchitecture, map[string]any{
			"CurrentJSON": fallbackJSON(currentJSON),
			"Changes":     formatDirectoryChanges(report.DirectoryChanges),
			"Packages":    strings.Join(report.PackagePaths, "\n"),
		})
		return prompt, false, err
	case "manifest":
		prompt, err := generator.RenderTemplate(templates.UpdateManifest, map[string]any{
			"CurrentJSON": fallbackJSON(currentJSON),
			"Reasons":     strings.Join(report.ManifestReasons, "\n"),
			"Signals":     formatManifestSignals(report),
		})
		return prompt, false, err
	default:
		if strings.HasPrefix(action.Target, "contract:") {
			moduleName := action.Module
			prompt, err := generator.RenderTemplate(templates.UpdateContract, map[string]any{
				"ModuleName":  moduleName,
				"CurrentJSON": fallbackJSON(currentJSON),
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

	// 契约类型使用结构体序列化确保字段顺序一致
	if strings.HasPrefix(action.Target, "contract:") {
		moduleName := strings.TrimPrefix(action.Target, "contract:")
		normalized, err := schema.NormalizeContractJSON(content, moduleName)
		if err != nil {
			return fmt.Errorf("生成的契约 JSON 格式不合法: %w", err)
		}
		return fileutil.WriteFile(targetPath, []byte(normalized))
	}

	formatted, err := generator.FormatJSON(content)
	if err != nil {
		return fmt.Errorf("生成的 JSON 格式不合法: %w", err)
	}
	return fileutil.WriteFile(targetPath, []byte(formatted))
}

// backupIfExists 如果目标文件存在则备份到 backup/ 目录。
func (e *Executor) backupIfExists(path string) error {
	if !fileutil.FileExists(path) {
		return nil
	}

	relPath, err := filepath.Rel(e.kontextDir, path)
	if err != nil {
		return err
	}
	backupPath := filepath.Join(e.kontextDir, "backup", e.backupStamp, relPath)
	data, err := fileutil.ReadFile(path)
	if err != nil {
		return err
	}
	return fileutil.WriteFile(backupPath, data)
}

// pruneBackups 清理超过 5 个的旧备份目录。
func (e *Executor) pruneBackups() error {
	backupRoot := filepath.Join(e.kontextDir, "backup")
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
		return filepath.Join(e.kontextDir, "ARCHITECTURE_MAP.json"), nil
	case "manifest":
		return filepath.Join(e.kontextDir, "PROJECT_MANIFEST.json"), nil
	default:
		if strings.HasPrefix(action.Target, "contract:") {
			return filepath.Join(e.kontextDir, "module_contracts", schema.ContractFilename(action.Module)), nil
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
	if len(lines) == 0 {
		return "没有额外信号"
	}
	return strings.Join(lines, "\n")
}

// fallbackJSON 若内容为空则返回占位注释。
func fallbackJSON(content string) string {
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

// mergeJSONObjects 将多个拼接在一起的 JSON 对象合并为单个 JSON 对象。
// 当 LLM 对分段请求返回完整 JSON 对象（而非 JSON 片段）时，
// 简单的字符串拼接会产生 "{...}\n\n{...}\n\n{...}" 形式的多顶层值，
// 此函数通过逐个解析并合并所有顶层键来修复该问题。
func mergeJSONObjects(content string) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(content))
	merged := make(map[string]json.RawMessage)

	objectCount := 0
	for decoder.More() {
		var obj map[string]json.RawMessage
		if err := decoder.Decode(&obj); err != nil {
			return "", fmt.Errorf("解析第 %d 个 JSON 对象失败: %w", objectCount+1, err)
		}
		objectCount++
		for key, val := range obj {
			merged[key] = val
		}
	}

	if objectCount <= 1 {
		// 只有一个或零个对象，无需合并
		return content, fmt.Errorf("内容不包含多个 JSON 对象")
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(merged); err != nil {
		return "", fmt.Errorf("合并后序列化失败: %w", err)
	}
	return buf.String(), nil
}

// validateGeneratedContent 校验生成的 JSON 内容是否合法且符合对应结构。
func validateGeneratedContent(action UpdateAction, content string) error {
	if err := generator.ValidateJSON(content); err != nil {
		return err
	}

	switch {
	case action.Target == "manifest":
		var manifest schema.ProjectManifest
		if err := json.Unmarshal([]byte(content), &manifest); err != nil {
			return fmt.Errorf("manifest 结构不合法: %w", err)
		}
		if strings.TrimSpace(manifest.Project.Name) == "" {
			return fmt.Errorf("manifest 结构不合法: project.name 不能为空")
		}
	case action.Target == "architecture":
		var arch schema.ArchitectureMap
		if err := json.Unmarshal([]byte(content), &arch); err != nil {
			return fmt.Errorf("architecture 结构不合法: %w", err)
		}
	case strings.HasPrefix(action.Target, "contract:"):
		var contract schema.ModuleContract
		if err := json.Unmarshal([]byte(content), &contract); err != nil {
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
