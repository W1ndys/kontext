package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/w1ndys/kontext/internal/llm"
)

// FileConfig 表示 ~/.kontext/config.json 的顶层结构。
type FileConfig struct {
	LLM LLMConfig `json:"llm"`
}

// LLMConfig 表示配置文件中的 llm 部分。
type LLMConfig struct {
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	Model     string `json:"model"`
	Timeout   int    `json:"timeout"`    // 超时时间（秒），0 表示使用默认值
	MaxTokens int    `json:"max_tokens"` // 最大输出 token 数，0 表示使用默认值
}

// GlobalConfigPath 返回全局配置文件路径 ~/.kontext/config.json。
func GlobalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户主目录失败: %w", err)
	}
	return filepath.Join(home, ".kontext", "config.json"), nil
}

// legacyConfigPath 返回旧版配置文件路径 ~/.kontext/config.yaml。
func legacyConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户主目录失败: %w", err)
	}
	return filepath.Join(home, ".kontext", "config.yaml"), nil
}

// migrateYAMLToJSON 检测旧版 config.yaml 是否存在，若存在则自动迁移为 config.json。
// 迁移成功后删除旧文件。仅在 config.json 不存在时执行迁移。
func migrateYAMLToJSON() {
	jsonPath, err := GlobalConfigPath()
	if err != nil {
		return
	}

	// 如果 config.json 已存在，无需迁移
	if _, err := os.Stat(jsonPath); err == nil {
		return
	}

	yamlPath, err := legacyConfigPath()
	if err != nil {
		return
	}

	// 如果 config.yaml 不存在，无需迁移
	yamlData, err := os.ReadFile(yamlPath)
	if err != nil {
		return
	}

	slog.Info("检测到旧版 config.yaml，正在自动迁移为 config.json", "yaml_path", yamlPath)

	// 手动解析 YAML 键值对（简单的顶层 key: value 格式），避免引入 yaml 依赖
	cfg := parseLegacyYAML(yamlData)

	// 确保目录存在
	dir := filepath.Dir(jsonPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Warn("迁移失败：创建配置目录失败", "error", err)
		return
	}

	// 写入 JSON
	jsonData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		slog.Warn("迁移失败：序列化 JSON 失败", "error", err)
		return
	}

	if err := os.WriteFile(jsonPath, jsonData, 0600); err != nil {
		slog.Warn("迁移失败：写入 config.json 失败", "error", err)
		return
	}

	// 删除旧文件
	if err := os.Remove(yamlPath); err != nil {
		slog.Warn("迁移完成但删除旧 config.yaml 失败", "error", err)
	} else {
		slog.Info("配置迁移完成，已删除旧 config.yaml", "json_path", jsonPath)
	}
}

// parseLegacyYAML 解析旧版 config.yaml 内容为 FileConfig。
// 支持简单的两级 YAML 结构（llm: 下的 key: value），无需引入 yaml 库。
func parseLegacyYAML(data []byte) *FileConfig {
	fc := &FileConfig{}
	lines := strings.Split(string(data), "\n")
	inLLM := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// 检查是否进入 llm 节
		if trimmed == "llm:" {
			inLLM = true
			continue
		}

		// 顶层其他节（非缩进行且不在 llm 下），退出 llm 节
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inLLM = false
			continue
		}

		if !inLLM {
			continue
		}

		// 解析 llm 下的 key: value
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		// 去除可能的引号
		value = strings.Trim(value, "\"'")

		switch key {
		case "base_url":
			fc.LLM.BaseURL = value
		case "api_key":
			fc.LLM.APIKey = value
		case "model":
			fc.LLM.Model = value
		case "timeout":
			if v, err := strconv.Atoi(value); err == nil {
				fc.LLM.Timeout = v
			}
		case "max_tokens":
			if v, err := strconv.Atoi(value); err == nil {
				fc.LLM.MaxTokens = v
			}
		}
	}

	return fc
}

// Load 按优先级加载 LLM 配置：环境变量 > 配置文件 > 默认值。
func Load() (*LLMConfig, error) {
	// 0. 自动迁移旧版 config.yaml
	migrateYAMLToJSON()

	// 1. 从配置文件读取
	cfg := &LLMConfig{}
	configPath, err := GlobalConfigPath()
	if err == nil {
		if data, readErr := os.ReadFile(configPath); readErr == nil {
			var fc FileConfig
			if jsonErr := json.Unmarshal(data, &fc); jsonErr == nil {
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

	// 4. 规范化 BaseURL（仅去除末尾斜杠）
	cfg.BaseURL = NormalizeBaseURL(cfg.BaseURL)

	return cfg, nil
}

// Save 将配置写入 ~/.kontext/config.json。
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
	data, err := json.MarshalIndent(&fc, "", "  ")
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
// 仅去除末尾的 /，不自动追加版本路径。
// 用户需提供完整的 URL 路由（如 https://api.openai.com/v1）。
func NormalizeBaseURL(url string) string {
	url = strings.TrimSuffix(url, "/")
	return url
}

// NormalizeBaseURLWithHint 规范化 API Base URL 并返回是否进行了修正。
// 返回值: (规范化后的URL, 是否进行了修正, 修正说明)
func NormalizeBaseURLWithHint(url string) (string, bool, string) {
	original := url
	var hints []string

	// 去除末尾的斜杠
	if strings.HasSuffix(url, "/") {
		url = strings.TrimSuffix(url, "/")
		hints = append(hints, "已去除末尾的 /")
	}

	if len(hints) > 0 {
		return url, true, strings.Join(hints, "，")
	}
	return original, false, ""
}
