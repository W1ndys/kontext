package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/w1ndys/kontext/internal/llm"
	"go.yaml.in/yaml/v4"
)

// FileConfig 表示 ~/.kontext/config.yaml 的顶层结构。
type FileConfig struct {
	LLM LLMConfig `yaml:"llm"`
}

// LLMConfig 表示配置文件中的 llm 部分。
type LLMConfig struct {
	BaseURL   string `yaml:"base_url"`
	APIKey    string `yaml:"api_key"`
	Model     string `yaml:"model"`
	Timeout   int    `yaml:"timeout"`    // 超时时间（秒），0 表示使用默认值
	MaxTokens int    `yaml:"max_tokens"` // 最大输出 token 数，0 表示使用默认值
}

// GlobalConfigPath 返回全局配置文件路径 ~/.kontext/config.yaml。
func GlobalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户主目录失败: %w", err)
	}
	return filepath.Join(home, ".kontext", "config.yaml"), nil
}

// Load 按优先级加载 LLM 配置：环境变量 > 配置文件 > 默认值。
func Load() (*LLMConfig, error) {
	// 1. 从配置文件读取
	cfg := &LLMConfig{}
	configPath, err := GlobalConfigPath()
	if err == nil {
		if data, readErr := os.ReadFile(configPath); readErr == nil {
			var fc FileConfig
			if yamlErr := yaml.Unmarshal(data, &fc); yamlErr == nil {
				cfg = &fc.LLM
			}
		}
	}

	// 2. 环境变量覆盖
	if v := os.Getenv("KONTEXT_LLM_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("KONTEXT_LLM_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("KONTEXT_LLM_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("KONTEXT_LLM_TIMEOUT"); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			cfg.Timeout = seconds
		}
	}
	if v := os.Getenv("KONTEXT_LLM_MAX_TOKENS"); v != "" {
		if tokens, err := strconv.Atoi(v); err == nil && tokens > 0 {
			cfg.MaxTokens = tokens
		}
	}

	// 3. 默认值兜底
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-5.4"
	}

	// 4. 规范化 BaseURL
	cfg.BaseURL = NormalizeBaseURL(cfg.BaseURL)

	return cfg, nil
}

// Save 将配置写入 ~/.kontext/config.yaml。
func Save(cfg *LLMConfig) error {
	configPath, err := GlobalConfigPath()
	if err != nil {
		return err
	}

	// 确保目录存在
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	fc := FileConfig{LLM: *cfg}
	data, err := yaml.Marshal(&fc)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return nil
}

// ToLLMConfig 将 LLMConfig 转换为 llm.Config。
func (c *LLMConfig) ToLLMConfig() *llm.Config {
	var timeout time.Duration
	if c.Timeout > 0 {
		timeout = time.Duration(c.Timeout) * time.Second
	}
	return &llm.Config{
		BaseURL:   c.BaseURL,
		APIKey:    c.APIKey,
		Model:     c.Model,
		Timeout:   timeout,
		MaxTokens: int64(c.MaxTokens),
	}
}

// NormalizeBaseURL 规范化 API Base URL：
// 1. 去除末尾的 /
// 2. 如果不以 /v1 结尾，自动添加 /v1
// 返回规范化后的 URL。
func NormalizeBaseURL(url string) string {
	// 去除末尾的斜杠
	url = strings.TrimSuffix(url, "/")

	// 检查是否以 /v1 结尾
	if !strings.HasSuffix(url, "/v1") {
		url = url + "/v1"
	}

	return url
}

// NormalizeBaseURLWithHint 规范化 API Base URL 并返回是否进行了自动修正。
// 返回值: (规范化后的URL, 是否进行了修正, 修正说明)
func NormalizeBaseURLWithHint(url string) (string, bool, string) {
	original := url
	var hints []string

	// 去除末尾的斜杠
	if strings.HasSuffix(url, "/") {
		url = strings.TrimSuffix(url, "/")
		hints = append(hints, "已去除末尾的 /")
	}

	// 检查是否以 /v1 结尾
	if !strings.HasSuffix(url, "/v1") {
		url = url + "/v1"
		hints = append(hints, "已自动添加 /v1 后缀")
	}

	if len(hints) > 0 {
		return url, true, strings.Join(hints, "，")
	}
	return original, false, ""
}
