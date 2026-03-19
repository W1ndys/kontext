package llm

import "fmt"

// NewClient 根据配置创建 LLM 客户端实例。
func NewClient(cfg *Config) (Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("KONTEXT_LLM_API_KEY 为必填项")
	}
	return newOpenAIClient(cfg), nil
}
