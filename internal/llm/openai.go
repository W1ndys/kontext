package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/invopop/jsonschema"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
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
		return nil, fmt.Errorf("调用 LLM API 失败: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("LLM API 未返回任何结果")
	}

	return &GenerateResponse{Content: resp.Choices[0].Message.Content}, nil
}

// Chat 支持多轮对话，接受完整的消息历史。
func (c *openaiClient) Chat(req *ChatRequest) (*ChatResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.GetTimeout())
	defer cancel()

	resp, err := c.createChatCompletion(ctx, req, openai.ChatCompletionNewParamsResponseFormatUnion{})
	if err != nil {
		return nil, fmt.Errorf("调用 LLM API 失败: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("LLM API 未返回任何结果")
	}

	return &ChatResponse{Content: resp.Choices[0].Message.Content}, nil
}

// ChatStructured 使用 JSON Schema 约束模型输出，并反序列化到 out。
func (c *openaiClient) ChatStructured(req *ChatRequest, schemaName string, out any) (*ChatResponse, error) {
	if out == nil {
		return nil, fmt.Errorf("结构化输出目标不能为空")
	}

	schema, err := generateJSONSchema(out)
	if err != nil {
		return nil, fmt.Errorf("生成 JSON Schema 失败: %w", err)
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
		return nil, fmt.Errorf("调用结构化输出失败: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("LLM API 未返回任何结果")
	}

	content := resp.Choices[0].Message.Content
	if err := json.Unmarshal([]byte(content), out); err != nil {
		return nil, fmt.Errorf("解析结构化输出失败: %w", err)
	}

	return &ChatResponse{Content: content}, nil
}

func (c *openaiClient) createChatCompletion(
	ctx context.Context,
	req *ChatRequest,
	responseFormat openai.ChatCompletionNewParamsResponseFormatUnion,
) (*openai.ChatCompletion, error) {
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
			return nil, fmt.Errorf("不支持的消息角色[%d]: %s", i, m.Role)
		}
	}

	return c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:          shared.ChatModel(c.cfg.Model),
		Messages:       msgs,
		ResponseFormat: responseFormat,
	})
}

// ListModels 获取可用的模型列表。
func (c *openaiClient) ListModels() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp := c.client.Models.ListAutoPaging(ctx)

	var models []string
	for resp.Next() {
		model := resp.Current()
		models = append(models, model.ID)
	}
	if err := resp.Err(); err != nil {
		return nil, fmt.Errorf("获取模型列表失败: %w", err)
	}

	return models, nil
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
