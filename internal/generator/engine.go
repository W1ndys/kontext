package generator

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/templates"
	"go.yaml.in/yaml/v4"
)

const maxRounds = 10

// RunInteractiveInit 执行 AI 交互式初始化的完整两阶段流程。
func RunInteractiveInit(client llm.Client, description string) error {
	// 阶段 1：多轮对话澄清需求
	summary, conversation, err := runInterview(client, description, os.Stdin, os.Stdout)
	if err != nil {
		return err
	}

	// 阶段 2：生成 YAML 配置文件
	fmt.Fprintln(os.Stdout, "\n需求澄清完成！正在生成配置文件...")
	return generateAndWrite(client, summary, conversation)
}

// runInterview 执行多轮对话，返回需求摘要和完整对话记录。
func runInterview(client llm.Client, description string, input io.Reader, output io.Writer) (string, string, error) {
	// 构建初始消息
	systemPrompt := templates.InitInterviewSystem
	userMsg, err := RenderTemplate(templates.InitInterviewUser, map[string]string{
		"Description": description,
	})
	if err != nil {
		return "", "", fmt.Errorf("渲染用户消息模板失败: %w", err)
	}

	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMsg},
	}

	var conversationLog strings.Builder
	conversationLog.WriteString(fmt.Sprintf("用户初始描述: %s\n\n", description))

	scanner := bufio.NewScanner(input)

	for round := 1; round <= maxRounds; round++ {
		resp, interview, err := interviewStep(client, messages)
		if err != nil {
			return "", "", fmt.Errorf("解析 LLM 响应失败: %w", err)
		}

		// 将 assistant 回复加入消息历史
		messages = append(messages, llm.Message{Role: "assistant", Content: resp.Content})

		if interview.Type == "done" {
			conversationLog.WriteString(fmt.Sprintf("需求摘要: %s\n", interview.Summary))
			return interview.Summary, conversationLog.String(), nil
		}

		// 显示问题和选项
		fmt.Fprintf(output, "\n[问题 %d/%d] %s\n", round, maxRounds, interview.Question)
		for i, opt := range interview.Options {
			fmt.Fprintf(output, "  %d. %s\n", i+1, opt)
		}
		fmt.Fprintf(output, "\n请选择 [1-%d] 或输入自定义回答: ", len(interview.Options))

		// 读取用户输入
		if !scanner.Scan() {
			// EOF (Ctrl+D) 或错误，退出
			return "", "", fmt.Errorf("用户取消输入")
		}
		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "" {
			userInput = "1" // 默认选第一项
		}

		// 处理数字选择
		var answer string
		if num, err := strconv.Atoi(userInput); err == nil && num >= 1 && num <= len(interview.Options) {
			answer = interview.Options[num-1]
		} else {
			answer = userInput
		}

		conversationLog.WriteString(fmt.Sprintf("Q%d: %s\nA%d: %s\n\n", round, interview.Question, round, answer))

		// 将用户回答加入消息历史
		messages = append(messages, llm.Message{Role: "user", Content: answer})
	}

	// 达到最大轮数，强制要求总结
	messages = append(messages, llm.Message{
		Role:    "user",
		Content: "已达到最大提问次数，请根据目前收集到的信息生成需求摘要。请以 JSON 格式回复：{\"type\": \"done\", \"summary\": \"...\"}",
	})

	resp, interview, err := interviewStep(client, messages)
	if err != nil {
		return "", "", fmt.Errorf("解析摘要响应失败: %w", err)
	}

	summary := interview.Summary
	if summary == "" {
		summary = resp.Content
	}

	conversationLog.WriteString(fmt.Sprintf("需求摘要: %s\n", summary))
	return summary, conversationLog.String(), nil
}

// generateAndWrite 调用 LLM 生成 YAML 并写入文件。
func generateAndWrite(client llm.Client, summary, conversation string) error {
	systemPrompt := templates.InitGenerateSystem
	userMsg, err := RenderTemplate(templates.InitGenerateUser, map[string]string{
		"Summary":      summary,
		"Conversation": conversation,
	})
	if err != nil {
		return fmt.Errorf("渲染生成模板失败: %w", err)
	}

	generated, err := GenerateStructuredYAML(client, systemPrompt, userMsg)
	if err != nil {
		return err
	}

	return WriteGeneratedYAML(generated)
}

// WriteGeneratedYAML 校验 GeneratedYAML 并写入 .kontext/ 目录。
func WriteGeneratedYAML(generated *GeneratedYAML) error {
	// 校验 YAML 合法性
	if err := ValidateYAML(generated.ProjectManifest); err != nil {
		return fmt.Errorf("生成的 PROJECT_MANIFEST.yaml 不合法: %w", err)
	}
	if err := ValidateYAML(generated.ArchitectureMap); err != nil {
		return fmt.Errorf("生成的 ARCHITECTURE_MAP.yaml 不合法: %w", err)
	}
	if err := ValidateYAML(generated.Conventions); err != nil {
		return fmt.Errorf("生成的 CONVENTIONS.yaml 不合法: %w", err)
	}

	// 校验模块契约
	for name, content := range generated.ModuleContracts {
		if err := ValidateYAML(content); err != nil {
			return fmt.Errorf("生成的 %s_CONTRACT.yaml 不合法: %w", name, err)
		}
	}

	// 写入文件
	kontextDir := ".kontext"
	dirs := []string{
		kontextDir,
		filepath.Join(kontextDir, "module_contracts"),
		filepath.Join(kontextDir, "prompts"),
	}
	for _, d := range dirs {
		if err := fileutil.EnsureDir(d); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", d, err)
		}
	}

	// 写入核心配置文件
	files := map[string]string{
		filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml"): generated.ProjectManifest,
		filepath.Join(kontextDir, "ARCHITECTURE_MAP.yaml"): generated.ArchitectureMap,
		filepath.Join(kontextDir, "CONVENTIONS.yaml"):      generated.Conventions,
	}

	for path, content := range files {
		if err := fileutil.WriteFile(path, []byte(content)); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", path, err)
		}
		fmt.Printf("  已创建: %s\n", path)
	}

	// 写入模块契约文件
	if len(generated.ModuleContracts) > 0 {
		fmt.Println()
		fmt.Printf("  模块契约 (%d 个):\n", len(generated.ModuleContracts))
		for name, content := range generated.ModuleContracts {
			filename := fmt.Sprintf("%s_CONTRACT.yaml", name)
			path := filepath.Join(kontextDir, "module_contracts", filename)
			if err := fileutil.WriteFile(path, []byte(content)); err != nil {
				return fmt.Errorf("写入 %s 失败: %w", path, err)
			}
			fmt.Printf("    已创建: %s\n", path)
		}
	}

	fmt.Println("\n.kontext/ 初始化完成！")
	return nil
}

// ValidateYAML 校验字符串是否为合法的 YAML。
func ValidateYAML(content string) error {
	var out interface{}
	return yaml.Unmarshal([]byte(content), &out)
}

// interviewStep 执行一轮 LLM 对话，优先使用 JSON Schema 结构化输出解析 InterviewResponse，失败时回退到文本解析。
func interviewStep(
	client llm.Client,
	messages []llm.Message,
) (*llm.ChatResponse, *InterviewResponse, error) {
	req := &llm.ChatRequest{Messages: messages}

	var structured InterviewResponse
	resp, err := client.ChatStructured(req, "interview_response", &structured)
	if err == nil {
		return resp, &structured, nil
	}

	resp, chatErr := client.Chat(req)
	if chatErr != nil {
		return nil, nil, fmt.Errorf("结构化输出失败: %v；回退调用失败: %w", err, chatErr)
	}

	parsed, parseErr := ParseInterviewResponse(resp.Content)
	if parseErr != nil {
		return nil, nil, fmt.Errorf("结构化输出失败: %v；回退解析失败: %w", err, parseErr)
	}

	return resp, parsed, nil
}

// GenerateStructuredYAML 调用 LLM 生成结构化 YAML，优先使用 JSON Schema 结构化输出，失败时回退到文本解析。
func GenerateStructuredYAML(client llm.Client, systemPrompt, userMsg string) (*GeneratedYAML, error) {
	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
	}

	var structured GeneratedYAML
	if _, err := client.ChatStructured(req, "generated_yaml", &structured); err == nil {
		return &structured, nil
	} else {
		generated, fallbackErr := generateJSONWithRetry(client, systemPrompt, userMsg)
		if fallbackErr != nil {
			return nil, fmt.Errorf("JSON 结构化输出失败: %v；回退解析也失败: %w", err, fallbackErr)
		}
		return generated, nil
	}
}

// AnalyzeProjectFiles 调用 LLM 分析项目目录树，识别关键文件。
func AnalyzeProjectFiles(client llm.Client, systemPrompt, userMsg string) (*AnalyzedFiles, error) {
	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
	}

	// 优先尝试 JSON Schema 结构化输出
	var structured AnalyzedFiles
	if _, err := client.ChatStructured(req, "analyzed_files", &structured); err == nil {
		return &structured, nil
	}

	// 回退到文本解析
	resp, err := client.Chat(req)
	if err != nil {
		return nil, fmt.Errorf("调用 LLM 分析文件失败: %w", err)
	}

	result, err := ParseAnalyzedFiles(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("解析 LLM 文件识别结果失败: %w", err)
	}

	return result, nil
}

// SelectKeyFiles 调用 LLM 根据文件概要选择重点文件。
func SelectKeyFiles(client llm.Client, systemPrompt, userMsg string) (*SelectedFiles, error) {
	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
	}

	// 优先尝试 JSON Schema 结构化输出
	var structured SelectedFiles
	if _, err := client.ChatStructured(req, "selected_files", &structured); err == nil {
		return &structured, nil
	}

	// 回退到文本解析
	resp, err := client.Chat(req)
	if err != nil {
		return nil, fmt.Errorf("调用 LLM 选择重点文件失败: %w", err)
	}

	result, err := ParseSelectedFiles(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("解析 LLM 重点文件选择结果失败: %w", err)
	}

	return result, nil
}

// GenerateDependencyGraph 生成模块依赖关系图。
func GenerateDependencyGraph(client llm.Client, systemPrompt, userMsg string) (*ModuleDependencyGraph, error) {
	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
	}

	// 优先尝试 JSON Schema 结构化输出
	var structured ModuleDependencyGraph
	if _, err := client.ChatStructured(req, "module_dependency_graph", &structured); err == nil {
		return &structured, nil
	}

	// 回退到文本解析
	resp, err := client.Chat(req)
	if err != nil {
		return nil, fmt.Errorf("调用 LLM 生成依赖图失败：%w", err)
	}

	result, err := ParseModuleDependencyGraph(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("解析 LLM 依赖图结果失败：%w", err)
	}

	return result, nil
}

// ParseModuleDependencyGraph 解析模块依赖关系图的 JSON 响应。
func ParseModuleDependencyGraph(content string) (*ModuleDependencyGraph, error) {
	content = strings.TrimSpace(content)

	// 尝试提取代码块（去除 markdown 代码块）
	if strings.HasPrefix(content, "```") {
		re := regexp.MustCompile("(?s)```(?:json|ya?ml)?\\s*\\n(.+?)\\n```")
		if matches := re.FindStringSubmatch(content); len(matches) >= 2 {
			content = strings.TrimSpace(matches[1])
		}
	}

	var graph ModuleDependencyGraph
	if err := json.Unmarshal([]byte(content), &graph); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败：%w", err)
	}

	return &graph, nil
}

// generateJSONWithRetry 使用文本模式调用 LLM 生成 JSON，作为 JSON Schema 结构化输出的回退方案。
func generateJSONWithRetry(client llm.Client, systemPrompt, userMsg string) (*GeneratedYAML, error) {
	resp, err := client.Chat(&llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("调用 LLM 生成配置失败: %w", err)
	}

	generated, err := ParseGeneratedJSON(resp.Content)
	if err == nil {
		return generated, nil
	}

	retryMsg := fmt.Sprintf("上次生成的 JSON 格式不正确，错误: %s\n请重新生成，确保直接返回合法的 JSON，不要添加额外说明。", err.Error())
	resp, err = client.Chat(&llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
			{Role: "assistant", Content: resp.Content},
			{Role: "user", Content: retryMsg},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("重试调用 LLM 失败: %w", err)
	}

	generated, err = ParseGeneratedJSON(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("LLM 生成的 JSON 格式不正确: %w", err)
	}

	return generated, nil
}

// RenderTemplate 渲染 Go 模板。
func RenderTemplate(tmpl string, data interface{}) (string, error) {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// GenerateSingleYAML 通用单文件生成函数，用于分步生成配置文件。内置重试机制。
func GenerateSingleYAML(client llm.Client, systemPrompt, userMsg string) (string, error) {
	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
	}

	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			time.Sleep(backoff)
		}

		// 优先尝试 JSON Schema 结构化输出
		var structured SingleFileYAML
		if _, err := client.ChatStructured(req, "single_file_yaml", &structured); err == nil {
			if content, valErr := firstValidYAMLCandidate(structured.Content); valErr == nil {
				return content, nil
			} else if strings.TrimSpace(structured.Content) != "" {
				lastErr = fmt.Errorf("JSON 结构化输出中的内容不合法 (尝试 %d/%d): %w", attempt+1, maxRetries, valErr)
			}
		}

		// 回退到文本解析
		resp, err := client.Chat(req)
		if err != nil {
			lastErr = fmt.Errorf("调用 LLM 生成配置失败 (尝试 %d/%d): %w", attempt+1, maxRetries, err)
			continue
		}

		// 尝试解析 JSON 响应
		parsed, err := ParseSingleFileJSON(resp.Content)
		if err == nil {
			if content, valErr := firstValidYAMLCandidate(parsed.Content); valErr == nil {
				return content, nil
			} else if strings.TrimSpace(parsed.Content) != "" {
				lastErr = fmt.Errorf("响应中的 YAML 不合法 (尝试 %d/%d): %w", attempt+1, maxRetries, valErr)
				continue
			}
		}

		// 解析失败，尝试直接提取 YAML 内容
		if content, valErr := firstValidYAMLCandidate(resp.Content, extractYAMLFromResponse(resp.Content)); valErr == nil {
			return content, nil
		} else if strings.TrimSpace(resp.Content) != "" {
			lastErr = fmt.Errorf("LLM 返回了内容，但未能提取合法 YAML (尝试 %d/%d): %w", attempt+1, maxRetries, valErr)
			continue
		}

		lastErr = fmt.Errorf("LLM 返回内容为空 (尝试 %d/%d)", attempt+1, maxRetries)
	}

	return "", lastErr
}

// firstValidYAMLCandidate 从多个候选字符串中返回第一个合法的 YAML 内容。
func firstValidYAMLCandidate(candidates ...string) (string, error) {
	var lastErr error

	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}

		if err := ValidateYAML(candidate); err == nil {
			return candidate, nil
		} else {
			lastErr = err
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("候选内容为空")
	}
	return "", lastErr
}

// GenerateModuleContract 生成单个模块的契约文件，支持自动重试。
func GenerateModuleContract(client llm.Client, systemPrompt, userMsg string, moduleName string) (*ModuleContractYAML, error) {
	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
	}

	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// 指数退避
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			time.Sleep(backoff)
		}

		// 优先尝试 JSON Schema 结构化输出
		var structured ModuleContractYAML
		if _, err := client.ChatStructured(req, "module_contract_yaml", &structured); err == nil {
			// 如果 LLM 返回的模块名为空，使用传入的模块名
			if structured.ModuleName == "" {
				structured.ModuleName = moduleName
			}
			return &structured, nil
		}

		// 回退到文本解析
		resp, err := client.Chat(req)
		if err != nil {
			lastErr = fmt.Errorf("调用 LLM 生成模块契约失败 (尝试 %d/%d): %w", attempt+1, maxRetries, err)
			continue
		}

		// 尝试解析 JSON 响应
		parsed, err := ParseModuleContractJSON(resp.Content)
		if err == nil {
			if parsed.ModuleName == "" {
				parsed.ModuleName = moduleName
			}
			return parsed, nil
		}

		// 解析失败，尝试直接提取 YAML 内容
		yamlContent := extractYAMLFromResponse(resp.Content)
		if yamlContent != "" {
			return &ModuleContractYAML{
				ModuleName: moduleName,
				Content:    yamlContent,
			}, nil
		}

		lastErr = fmt.Errorf("解析模块契约响应失败 (尝试 %d/%d): %w", attempt+1, maxRetries, err)
	}

	return nil, lastErr
}

// extractYAMLFromResponse 尝试从 LLM 响应中提取 YAML 内容。
// 处理各种可能的格式：纯 YAML、markdown 代码块包裹的 YAML 等。
func extractYAMLFromResponse(content string) string {
	content = strings.TrimSpace(content)

	// 尝试提取 markdown 代码块中的内容
	// 匹配 ```yaml ... ``` 或 ``` ... ```
	patterns := []string{
		"(?s)```yaml\\s*\\n(.+?)\\n```",
		"(?s)```\\s*\\n(.+?)\\n```",
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(content); len(matches) >= 2 {
			return strings.TrimSpace(matches[1])
		}
	}

	// 检查是否以 YAML 常见开头开始（module:, name:, 等）
	if strings.HasPrefix(content, "module:") ||
		strings.HasPrefix(content, "# ") ||
		strings.HasPrefix(content, "---") {
		return content
	}

	return ""
}

// FilterFilesByModule 筛选属于指定模块的文件。
func FilterFilesByModule(files map[string]string, moduleName string) map[string]string {
	result := make(map[string]string)
	for path, content := range files {
		if BelongsToModule(path, moduleName) {
			result[path] = content
		}
	}
	return result
}

// BelongsToModule 判断文件路径是否属于指定模块。
func BelongsToModule(filePath, moduleName string) bool {
	normalized := filepath.ToSlash(filePath)
	parts := strings.Split(normalized, "/")

	if len(parts) == 0 {
		return false
	}

	// cmd/xxx.go → 属于 cmd 模块
	if parts[0] == moduleName {
		return true
	}

	// internal/config/xxx.go → 属于 config 模块
	if len(parts) >= 2 && (parts[0] == "internal" || parts[0] == "pkg") {
		return parts[1] == moduleName
	}

	return false
}

// ModuleContractResult 是并行生成模块契约的单个结果。
type ModuleContractResult struct {
	ModuleName string
	Content    string
	Error      error
	Duration   float64 // 耗时（秒）
}

// GenerateModuleContractStream 生成单个模块的契约文件，支持自动重试。
// 使用 ChatStructured 非流式调用。
func GenerateModuleContractStream(
	client llm.Client,
	systemPrompt, userMsg string,
	moduleName string,
	onStream func(ModuleContractStreamEvent),
) (*ModuleContractYAML, error) {
	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
	}

	maxRetries := 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			backoff := time.Duration(1<<uint(attempt-2)) * time.Second
			time.Sleep(backoff)
		}

		if onStream != nil {
			onStream(ModuleContractStreamEvent{
				ModuleName: moduleName,
				Attempt:    attempt,
			})
		}

		// 优先尝试 JSON Schema 结构化输出
		var structured ModuleContractYAML
		if _, err := client.ChatStructured(req, "module_contract_yaml", &structured); err == nil {
			if structured.ModuleName == "" {
				structured.ModuleName = moduleName
			}
			finalContent := strings.TrimSpace(structured.Content)
			if finalContent != "" {
				if onStream != nil {
					onStream(ModuleContractStreamEvent{
						ModuleName:   moduleName,
						Attempt:      attempt,
						Done:         true,
						FinalContent: finalContent,
					})
				}
				return &structured, nil
			}
		}

		// 回退到文本 Chat + JSON 解析
		resp, err := client.Chat(req)
		if err != nil {
			lastErr = fmt.Errorf("调用 LLM 生成模块契约失败 (尝试 %d/%d): %w", attempt, maxRetries, err)
			if onStream != nil {
				onStream(ModuleContractStreamEvent{
					ModuleName: moduleName,
					Attempt:    attempt,
					Error:      lastErr,
				})
			}
			continue
		}

		// 尝试解析 JSON 响应
		parsed, parseErr := ParseModuleContractJSON(resp.Content)
		if parseErr == nil && strings.TrimSpace(parsed.Content) != "" {
			if parsed.ModuleName == "" {
				parsed.ModuleName = moduleName
			}
			if onStream != nil {
				onStream(ModuleContractStreamEvent{
					ModuleName:   moduleName,
					Attempt:      attempt,
					Done:         true,
					FinalContent: parsed.Content,
				})
			}
			return parsed, nil
		}

		// 尝试直接提取 YAML 内容
		finalContent := extractYAMLFromResponse(resp.Content)
		if finalContent == "" {
			trimmed := strings.TrimSpace(resp.Content)
			if trimmed != "" {
				finalContent = trimmed
			}
		}
		if finalContent == "" {
			lastErr = fmt.Errorf("LLM 返回内容为空 (尝试 %d/%d)", attempt, maxRetries)
			if onStream != nil {
				onStream(ModuleContractStreamEvent{
					ModuleName: moduleName,
					Attempt:    attempt,
					Done:       true,
					Error:      lastErr,
				})
			}
			continue
		}

		if onStream != nil {
			onStream(ModuleContractStreamEvent{
				ModuleName:   moduleName,
				Attempt:      attempt,
				Done:         true,
				FinalContent: finalContent,
			})
		}

		return &ModuleContractYAML{
			ModuleName: moduleName,
			Content:    finalContent,
		}, nil
	}

	return nil, lastErr
}

// GenerateModuleContracts 并行生成多个模块契约，限制最大并发数。
func GenerateModuleContracts(
	client llm.Client,
	systemPrompt string,
	modules []string,
	userMsgGenerator func(moduleName string) (string, error),
	maxConcurrency int,
	onStream func(event ModuleContractStreamEvent),
	onProgress func(result ModuleContractResult),
) (map[string]string, []error) {
	if maxConcurrency <= 0 {
		maxConcurrency = 3
	}

	results := make(map[string]string)
	var errors []error
	var mu sync.Mutex
	var wg sync.WaitGroup

	sem := make(chan struct{}, maxConcurrency)

	for _, mod := range modules {
		wg.Add(1)
		go func(moduleName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			startTime := time.Now()

			userMsg, err := userMsgGenerator(moduleName)
			if err != nil {
				result := ModuleContractResult{
					ModuleName: moduleName,
					Error:      fmt.Errorf("生成用户消息失败: %w", err),
					Duration:   time.Since(startTime).Seconds(),
				}
				mu.Lock()
				errors = append(errors, fmt.Errorf("模块 %s: %w", moduleName, result.Error))
				mu.Unlock()
				if onProgress != nil {
					onProgress(result)
				}
				return
			}

			contract, err := GenerateModuleContractStream(client, systemPrompt, userMsg, moduleName, func(event ModuleContractStreamEvent) {
				if onStream != nil {
					onStream(event)
				}
			})

			elapsed := time.Since(startTime).Seconds()
			var progressResult ModuleContractResult

			mu.Lock()
			if err != nil {
				errors = append(errors, fmt.Errorf("模块 %s: %w", moduleName, err))
				progressResult = ModuleContractResult{
					ModuleName: moduleName,
					Error:      err,
					Duration:   elapsed,
				}
			} else {
				results[moduleName] = contract.Content
				progressResult = ModuleContractResult{
					ModuleName: moduleName,
					Content:    contract.Content,
					Duration:   elapsed,
				}
			}
			mu.Unlock()

			if onProgress != nil {
				onProgress(progressResult)
			}
		}(mod)
	}

	wg.Wait()
	return results, errors
}
