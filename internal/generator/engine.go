package generator

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

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

	fmt.Println("\n.kontext/ 初始化完成！")
	return nil
}

// ValidateYAML 校验字符串是否为合法的 YAML。
func ValidateYAML(content string) error {
	var out interface{}
	return yaml.Unmarshal([]byte(content), &out)
}

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

// GenerateStructuredYAML 调用 LLM 生成结构化 YAML，优先使用结构化输出，失败时回退到传统 JSON 模式。
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
		generated, legacyErr := generateLegacyYAML(client, systemPrompt, userMsg)
		if legacyErr != nil {
			return nil, fmt.Errorf("结构化输出失败: %v；回退到传统 JSON 模式也失败: %w", err, legacyErr)
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

	// 优先尝试结构化输出
	var structured AnalyzedFiles
	if _, err := client.ChatStructured(req, "analyzed_files", &structured); err == nil {
		return &structured, nil
	}

	// 回退到传统 JSON 解析
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

	// 优先尝试结构化输出
	var structured SelectedFiles
	if _, err := client.ChatStructured(req, "selected_files", &structured); err == nil {
		return &structured, nil
	}

	// 回退到传统 JSON 解析
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

func generateLegacyYAML(client llm.Client, systemPrompt, userMsg string) (*GeneratedYAML, error) {
	resp, err := client.Chat(&llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("调用 LLM 生成配置失败: %w", err)
	}

	generated, err := ParseGeneratedYAML(resp.Content)
	if err == nil {
		return generated, nil
	}

	retryMsg := fmt.Sprintf("上次生成的 JSON 格式不正确，错误: %s\n请重新生成，确保返回合法的 JSON。", err.Error())
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

	generated, err = ParseGeneratedYAML(resp.Content)
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
