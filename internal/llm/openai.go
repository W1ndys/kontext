package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
	"github.com/w1ndys/kontext/internal/logging"
)

// openaiClient 实现了兼容 OpenAI API 规范的 LLM 客户端。
type openaiClient struct {
	cfg    *Config
	client openai.Client
}

// newOpenAIClient 创建一个新的 OpenAI 兼容客户端。
func newOpenAIClient(cfg *Config) *openaiClient {
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	return &openaiClient{
		cfg:    cfg,
		client: openai.NewClient(opts...),
	}
}

// Generate 调用 LLM API 生成内容。
func (c *openaiClient) Generate(req *GenerateRequest) (*GenerateResponse, error) {
	startedAt := time.Now()
	c.logGenerateRequestStart(req)

	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.GetTimeout())
	defer cancel()

	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: shared.ChatModel(c.cfg.Model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(req.SystemPrompt),
			openai.UserMessage(req.UserPrompt),
		},
	})
	if err != nil {
		wrappedErr := fmt.Errorf("调用 LLM API 失败: %w", err)
		c.logGenerateError(req, wrappedErr, startedAt)
		return nil, wrappedErr
	}
	if len(resp.Choices) == 0 {
		wrappedErr := fmt.Errorf("LLM API 未返回任何结果")
		c.logGenerateError(req, wrappedErr, startedAt)
		return nil, wrappedErr
	}

	content := resp.Choices[0].Message.Content
	c.logGenerateResponse(req, content, startedAt)
	return &GenerateResponse{Content: content}, nil
}

// Chat 支持多轮对话，接受完整的消息历史。
func (c *openaiClient) Chat(req *ChatRequest) (*ChatResponse, error) {
	startedAt := time.Now()
	c.logChatRequestStart("chat", req)

	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.GetTimeout())
	defer cancel()

	resp, err := c.createChatCompletion(ctx, req, openai.ChatCompletionNewParamsResponseFormatUnion{})
	if err != nil {
		wrappedErr := fmt.Errorf("调用 LLM API 失败: %w", err)
		c.logChatError("chat", req, wrappedErr, startedAt)
		return nil, wrappedErr
	}
	if len(resp.Choices) == 0 {
		wrappedErr := fmt.Errorf("LLM API 未返回任何结果")
		c.logChatError("chat", req, wrappedErr, startedAt)
		return nil, wrappedErr
	}

	content := resp.Choices[0].Message.Content
	c.logChatResponse("chat", req, content, startedAt)
	return &ChatResponse{Content: content}, nil
}

// ChatStream 支持流式多轮对话，并在每次收到新增文本时回调。
func (c *openaiClient) ChatStream(req *ChatRequest, onChunk func(string) error) (*ChatResponse, error) {
	startedAt := time.Now()
	c.logChatRequestStart("chat_stream", req)

	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.GetTimeout())
	defer cancel()

	params, err := c.buildChatCompletionParams(req, openai.ChatCompletionNewParamsResponseFormatUnion{})
	if err != nil {
		c.logChatError("chat_stream", req, err, startedAt)
		return nil, err
	}
	params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
		IncludeUsage: openai.Bool(true),
	}

	stream := c.client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()

	var content strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		for _, choice := range chunk.Choices {
			delta := choice.Delta.Content
			if delta == "" {
				continue
			}
			content.WriteString(delta)
			if onChunk != nil {
				if chunkErr := onChunk(delta); chunkErr != nil {
					c.logChatError("chat_stream", req, chunkErr, startedAt,
						"partial_content_length", content.Len(),
						"partial_content", content.String(),
					)
					return nil, chunkErr
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		wrappedErr := fmt.Errorf("调用 LLM 流式 API 失败: %w", err)
		c.logChatError("chat_stream", req, wrappedErr, startedAt,
			"partial_content_length", content.Len(),
			"partial_content", content.String(),
		)
		return nil, wrappedErr
	}

	finalContent := content.String()
	c.logChatResponse("chat_stream", req, finalContent, startedAt)
	return &ChatResponse{Content: finalContent}, nil
}

// ChatStructured 使用 JSON Schema 约束模型输出，并反序列化到 out。
func (c *openaiClient) ChatStructured(req *ChatRequest, schemaName string, out any) (*ChatResponse, error) {
	startedAt := time.Now()
	c.logChatRequestStart("chat_structured", req, "schema_name", schemaName)

	if out == nil {
		err := fmt.Errorf("结构化输出目标不能为空")
		c.logChatError("chat_structured", req, err, startedAt, "schema_name", schemaName)
		return nil, err
	}

	schema, err := generateJSONSchema(out)
	if err != nil {
		wrappedErr := fmt.Errorf("生成 JSON Schema 失败: %w", err)
		c.logChatError("chat_structured", req, wrappedErr, startedAt, "schema_name", schemaName)
		return nil, wrappedErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.GetTimeout())
	defer cancel()

	resp, err := c.createChatCompletion(ctx, req, openai.ChatCompletionNewParamsResponseFormatUnion{
		OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
			JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
				Name:   schemaName,
				Strict: openai.Bool(true),
				Schema: schema,
			},
		},
	})
	if err != nil {
		wrappedErr := fmt.Errorf("调用结构化输出失败: %w", err)
		c.logChatError("chat_structured", req, wrappedErr, startedAt, "schema_name", schemaName)
		return nil, wrappedErr
	}
	if len(resp.Choices) == 0 {
		wrappedErr := fmt.Errorf("LLM API 未返回任何结果")
		c.logChatError("chat_structured", req, wrappedErr, startedAt, "schema_name", schemaName)
		return nil, wrappedErr
	}

	choice := resp.Choices[0]
	content := strings.TrimSpace(choice.Message.Content)
	if content == "" {
		rawMessage := compactSnippet(choice.Message.RawJSON(), 240)
		if choice.Message.Refusal != "" {
			wrappedErr := fmt.Errorf(
				"解析结构化输出失败: 响应内容为空（finish_reason=%s, refusal=%q, raw_message=%s）",
				choice.FinishReason,
				choice.Message.Refusal,
				rawMessage,
			)
			c.logChatError("chat_structured", req, wrappedErr, startedAt,
				"schema_name", schemaName,
				"finish_reason", choice.FinishReason,
				"refusal", choice.Message.Refusal,
				"raw_message", rawMessage,
			)
			return nil, wrappedErr
		}
		wrappedErr := fmt.Errorf(
			"解析结构化输出失败: 响应内容为空（finish_reason=%s, raw_message=%s）",
			choice.FinishReason,
			rawMessage,
		)
		c.logChatError("chat_structured", req, wrappedErr, startedAt,
			"schema_name", schemaName,
			"finish_reason", choice.FinishReason,
			"raw_message", rawMessage,
		)
		return nil, wrappedErr
	}
	if err := json.Unmarshal([]byte(content), out); err != nil {
		wrappedErr := fmt.Errorf("解析结构化输出失败: %w（content=%s）", err, compactSnippet(content, 240))
		c.logChatError("chat_structured", req, wrappedErr, startedAt,
			"schema_name", schemaName,
			"response_content", content,
		)
		return nil, wrappedErr
	}

	c.logChatResponse("chat_structured", req, content, startedAt, "schema_name", schemaName)
	return &ChatResponse{Content: content}, nil
}

func (c *openaiClient) createChatCompletion(
	ctx context.Context,
	req *ChatRequest,
	responseFormat openai.ChatCompletionNewParamsResponseFormatUnion,
) (*openai.ChatCompletion, error) {
	params, err := c.buildChatCompletionParams(req, responseFormat)
	if err != nil {
		return nil, err
	}

	return c.client.Chat.Completions.New(ctx, params)
}

func (c *openaiClient) buildChatCompletionParams(
	req *ChatRequest,
	responseFormat openai.ChatCompletionNewParamsResponseFormatUnion,
) (openai.ChatCompletionNewParams, error) {
	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages))
	for i, m := range req.Messages {
		switch m.Role {
		case "system":
			msgs = append(msgs, openai.SystemMessage(m.Content))
		case "user":
			msgs = append(msgs, openai.UserMessage(m.Content))
		case "assistant":
			msgs = append(msgs, openai.AssistantMessage(m.Content))
		default:
			return openai.ChatCompletionNewParams{}, fmt.Errorf("不支持的消息角色[%d]: %s", i, m.Role)
		}
	}

	return openai.ChatCompletionNewParams{
		Model:          shared.ChatModel(c.cfg.Model),
		Messages:       msgs,
		ResponseFormat: responseFormat,
	}, nil
}

// ListModels 获取可用的模型列表。
func (c *openaiClient) ListModels() ([]string, error) {
	startedAt := time.Now()
	logger := c.logger()
	logger.Debug("llm request started", "method", "list_models")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp := c.client.Models.ListAutoPaging(ctx)

	var models []string
	for resp.Next() {
		model := resp.Current()
		models = append(models, model.ID)
	}
	if err := resp.Err(); err != nil {
		wrappedErr := fmt.Errorf("获取模型列表失败: %w", err)
		logger.Error("llm request failed",
			"method", "list_models",
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"error", wrappedErr,
		)
		return nil, wrappedErr
	}

	logger.Debug("llm response received",
		"method", "list_models",
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"model_count", len(models),
		"models", models,
	)
	return models, nil
}

func (c *openaiClient) logger() *slog.Logger {
	return logging.Default().With(
		"component", "llm",
		"provider", "openai",
		"model", c.cfg.Model,
		"base_url", c.cfg.BaseURL,
	)
}

func (c *openaiClient) logGenerateRequestStart(req *GenerateRequest) {
	c.logger().Debug("llm request started",
		"method", "generate",
		"system_prompt_length", len(req.SystemPrompt),
		"user_prompt_length", len(req.UserPrompt),
	)
}

func (c *openaiClient) logGenerateResponse(req *GenerateRequest, content string, startedAt time.Time) {
	c.logger().Debug("llm response received",
		"method", "generate",
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"system_prompt_length", len(req.SystemPrompt),
		"user_prompt_length", len(req.UserPrompt),
		"content_length", len(content),
		"content", content,
	)
}

func (c *openaiClient) logGenerateError(req *GenerateRequest, err error, startedAt time.Time) {
	c.logger().Error("llm request failed",
		"method", "generate",
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"system_prompt_length", len(req.SystemPrompt),
		"user_prompt_length", len(req.UserPrompt),
		"error", err,
	)
}

func (c *openaiClient) logChatRequestStart(method string, req *ChatRequest, extra ...any) {
	attrs := []any{
		"method", method,
		"message_count", len(req.Messages),
		"input_chars", chatRequestChars(req),
	}
	attrs = append(attrs, extra...)
	c.logger().Debug("llm request started", attrs...)
}

func (c *openaiClient) logChatResponse(method string, req *ChatRequest, content string, startedAt time.Time, extra ...any) {
	attrs := []any{
		"method", method,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"message_count", len(req.Messages),
		"input_chars", chatRequestChars(req),
		"content_length", len(content),
		"content", content,
	}
	attrs = append(attrs, extra...)
	c.logger().Debug("llm response received", attrs...)
}

func (c *openaiClient) logChatError(method string, req *ChatRequest, err error, startedAt time.Time, extra ...any) {
	attrs := []any{
		"method", method,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"message_count", len(req.Messages),
		"input_chars", chatRequestChars(req),
		"error", err,
	}
	attrs = append(attrs, extra...)
	c.logger().Error("llm request failed", attrs...)
}

func chatRequestChars(req *ChatRequest) int {
	if req == nil {
		return 0
	}

	total := 0
	for _, msg := range req.Messages {
		total += len(msg.Content)
	}
	return total
}

func generateJSONSchema(v any) (map[string]any, error) {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}

	schema := reflector.Reflect(v)
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return result, nil
}

func compactSnippet(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return `""`
	}

	s = strings.Join(strings.Fields(s), " ")
	if maxLen > 0 && len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return fmt.Sprintf("%q", s)
}
