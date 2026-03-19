package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/w1ndys/kontext/internal/llm"
	"gopkg.in/yaml.v3"
)

// FileConfig 表示 ~/.kontext/config.yaml 的顶层结构。
type FileConfig struct {
	LLM LLMConfig `yaml:"llm"`
}

// LLMConfig 表示配置文件中的 llm 部分。
type LLMConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
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

	// 3. 默认值兜底
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-5.4"
	}

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
	return &llm.Config{
		BaseURL: c.BaseURL,
		APIKey:  c.APIKey,
		Model:   c.Model,
	}
}
