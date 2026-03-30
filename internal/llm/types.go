package llm

import (
	"os"
	"strconv"
	"time"
)

// DefaultTimeout 是 LLM API 调用的默认超时时间。
const DefaultTimeout = 300 * time.Second // 5 分钟

// DefaultMaxTokens 是 LLM API 调用的默认最大输出 token 数。
const DefaultMaxTokens int64 = 128000

// Config 保存 LLM 客户端的配置信息。
type Config struct {
	BaseURL   string
	APIKey    string
	Model     string
	Timeout   time.Duration // API 调用超时时间，0 表示使用默认值
	MaxTokens int64         // 最大输出 token 数，0 表示使用默认值
}

// GetTimeout 返回配置的超时时间，如果未设置则返回默认值。
func (c *Config) GetTimeout() time.Duration {
	if c.Timeout <= 0 {
		return DefaultTimeout
	}
	return c.Timeout
}

// GetMaxTokens 返回配置的最大输出 token 数，如果未设置则返回默认值。
func (c *Config) GetMaxTokens() int64 {
	if c.MaxTokens <= 0 {
		return DefaultMaxTokens
	}
	return c.MaxTokens
}

// ConfigFromEnv 从环境变量读取 LLM 配置。
// KONTEXT_LLM_BASE_URL: API 地址（默认 https://api.openai.com/v1）
// KONTEXT_LLM_API_KEY: API 密钥（必填）
// KONTEXT_LLM_MODEL: 模型名称（默认 gpt-5.4）
// KONTEXT_LLM_TIMEOUT: 超时时间（秒，默认 300）
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
	// 解析超时设置
	if timeoutStr := os.Getenv("KONTEXT_LLM_TIMEOUT"); timeoutStr != "" {
		if seconds, err := strconv.Atoi(timeoutStr); err == nil && seconds > 0 {
			cfg.Timeout = time.Duration(seconds) * time.Second
		}
	}
	// 解析最大输出 token 数
	if maxTokensStr := os.Getenv("KONTEXT_LLM_MAX_TOKENS"); maxTokensStr != "" {
		if tokens, err := strconv.ParseInt(maxTokensStr, 10, 64); err == nil && tokens > 0 {
			cfg.MaxTokens = tokens
		}
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
	ChatStream(req *ChatRequest, onChunk func(string) error) (*ChatResponse, error)
	ChatStructured(req *ChatRequest, schemaName string, out any) (*ChatResponse, error)
	ListModels() ([]string, error)
}
