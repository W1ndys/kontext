package llm

import "os"

// Config 保存 LLM 客户端的配置信息。
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
}

// ConfigFromEnv 从环境变量读取 LLM 配置。
// KONTEXT_LLM_BASE_URL: API 地址（默认 https://api.openai.com/v1）
// KONTEXT_LLM_API_KEY: API 密钥（必填）
// KONTEXT_LLM_MODEL: 模型名称（默认 gpt-5.4）
func ConfigFromEnv() (*Config, error) {
	cfg := &Config{
		BaseURL: os.Getenv("KONTEXT_LLM_BASE_URL"),
		APIKey:  os.Getenv("KONTEXT_LLM_API_KEY"),
		Model:   os.Getenv("KONTEXT_LLM_MODEL"),
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-5.4"
	}
	return cfg, nil
}

// GenerateRequest 是发送给 LLM 的请求，包含系统提示词和用户提示词。
type GenerateRequest struct {
	SystemPrompt string
	UserPrompt   string
}

// GenerateResponse 是 LLM 返回的响应，包含生成的内容。
type GenerateResponse struct {
	Content string
}

// Message 表示一条聊天消息，用于多轮对话。
type Message struct {
	Role    string // "system", "user", "assistant"
	Content string
}

// ChatRequest 是多轮对话请求，包含完整的消息历史。
type ChatRequest struct {
	Messages []Message
}

// ChatResponse 是多轮对话的响应。
type ChatResponse struct {
	Content string
}

// Client 是 LLM 交互的统一接口。
type Client interface {
	Generate(req *GenerateRequest) (*GenerateResponse, error)
	Chat(req *ChatRequest) (*ChatResponse, error)
}
