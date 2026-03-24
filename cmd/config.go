package cmd

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/w1ndys/kontext/internal/config"
	"github.com/w1ndys/kontext/internal/llm"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "管理全局 LLM 配置 / Manage global LLM configuration (~/.kontext/config.yaml)",
	Long: `管理 Kontext 的全局 LLM 配置。

无参数时启动交互式配置引导：
  kontext config

子命令：
  kontext config set llm.model gpt-5.4
  kontext config get llm.model
  kontext config list

---

Manage Kontext global LLM configuration.

Without arguments, start interactive configuration wizard:
  kontext config

Subcommands:
  kontext config set llm.model gpt-5.4
  kontext config get llm.model
  kontext config list`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInteractiveConfig()
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "设置配置项 / Set a configuration value",
	Long: `设置指定的配置项。支持的 key：
  llm.base_url   API 地址
  llm.api_key    API 密钥
  llm.model      模型名称
  llm.timeout    超时时间（秒）

---

Set a configuration value. Supported keys:
  llm.base_url   API endpoint URL
  llm.api_key    API key
  llm.model      Model name
  llm.timeout    Timeout in seconds`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConfigSet(args[0], args[1])
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "获取配置项的值 / Get a configuration value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConfigGet(args[0])
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有配置项 / List all configuration values (api_key masked)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConfigList()
	},
}

func init() {
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configListCmd)
}

// runInteractiveConfig 交互式引导用户设置 LLM 配置。
func runInteractiveConfig() error {
	logger := namedLogger(commandPathConfig)
	logger.Info("interactive config started")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config failed", "error", err)
		return err
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Kontext LLM 配置向导")
	fmt.Println(strings.Repeat("-", 40))

	// Base URL
	fmt.Printf("API 地址 [%s]: ", cfg.BaseURL)
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		cfg.BaseURL = strings.TrimSpace(input)
	}

	// 规范化 Base URL 并提示
	normalized, changed, hint := config.NormalizeBaseURLWithHint(cfg.BaseURL)
	if changed {
		logger.Info("normalized base url",
			"from", cfg.BaseURL,
			"to", normalized,
			"hint", hint,
		)
		fmt.Printf("  ⚠ URL 已自动修正: %s → %s（%s）\n", cfg.BaseURL, normalized, hint)
		cfg.BaseURL = normalized
	}

	// API Key
	display := maskKey(cfg.APIKey)
	fmt.Printf("API 密钥 [%s]: ", display)
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		cfg.APIKey = strings.TrimSpace(input)
	}

	// 验证 API Key 并获取模型列表
	if cfg.APIKey != "" {
		logger.Info("verifying llm configuration",
			"base_url", cfg.BaseURL,
			"model", cfg.Model,
		)
		fmt.Println("\n正在验证 API Key...")
		llmCfg := &llm.Config{
			BaseURL: cfg.BaseURL,
			APIKey:  cfg.APIKey,
			Model:   cfg.Model,
		}
		client, err := llm.NewClient(llmCfg)
		if err != nil {
			logger.Warn("create llm client failed", "error", err)
			fmt.Printf("  ✗ 创建客户端失败: %v\n", err)
			// 允许手动输入模型名称
			fmt.Printf("模型名称 [%s]: ", cfg.Model)
			if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
				cfg.Model = strings.TrimSpace(input)
			}
		} else {
			models, err := client.ListModels()
			if err != nil {
				logger.Warn("list models failed", "error", err)
				fmt.Printf("  ✗ 获取模型列表失败: %v\n", err)
				fmt.Println("  提示: 请检查 API Key 是否正确，或者 API 地址是否可访问")
				// 获取模型列表失败时，允许手动输入模型名称
				fmt.Printf("模型名称 [%s]: ", cfg.Model)
				if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
					cfg.Model = strings.TrimSpace(input)
				}
			} else {
				logger.Info("llm configuration verified", "model_count", len(models))
				fmt.Printf("  ✓ API Key 验证成功！发现 %d 个可用模型\n\n", len(models))

				// 排序模型列表
				sort.Strings(models)

				// 使用交互式列表选择模型
				if len(models) > 0 {
					selected, err := runModelSelector(models, cfg.Model)
					if err != nil {
						logger.Warn("model selector failed", "error", err)
						fmt.Printf("  模型选择器出错: %v，将使用当前模型 %s\n", err, cfg.Model)
					} else if selected == manualInputModelName {
						// 用户选择手动输入
						fmt.Printf("请输入模型名称 [%s]: ", cfg.Model)
						if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
							cfg.Model = strings.TrimSpace(input)
						}
						fmt.Printf("  已设置模型: %s\n", cfg.Model)
					} else if selected != "" {
						cfg.Model = selected
						fmt.Printf("  已选择: %s\n", cfg.Model)
					}
				}
			}
		}
	} else {
		logger.Info("interactive config proceeding without api key")
		// Model（没有 API Key 时使用手动输入）
		fmt.Printf("模型名称 [%s]: ", cfg.Model)
		if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
			cfg.Model = strings.TrimSpace(input)
		}
	}

	if err := config.Save(cfg); err != nil {
		logger.Error("save config failed", "error", err)
		return err
	}

	configPath, _ := config.GlobalConfigPath()
	logger.Info("config saved",
		"path", configPath,
		"model", cfg.Model,
		"base_url", cfg.BaseURL,
		"has_api_key", cfg.APIKey != "",
		"timeout_seconds", cfg.Timeout,
	)
	fmt.Printf("\n配置已保存至 %s\n", configPath)
	return nil
}

// runConfigSet 设置指定的配置项。
func runConfigSet(key, value string) error {
	logger := namedLogger(commandPathConfigSet).With("key", key)
	logger.Info("config set started", "sensitive", isSensitiveConfigKey(key))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config failed", "error", err)
		return err
	}

	switch key {
	case "llm.base_url":
		normalized, changed, hint := config.NormalizeBaseURLWithHint(value)
		if changed {
			logger.Info("normalized base url",
				"from", value,
				"to", normalized,
				"hint", hint,
			)
			fmt.Printf("  ⚠ URL 已自动修正: %s → %s（%s）\n", value, normalized, hint)
			value = normalized
		}
		cfg.BaseURL = value
	case "llm.api_key":
		cfg.APIKey = value
	case "llm.model":
		cfg.Model = value
	case "llm.timeout":
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds <= 0 {
			logger.Warn("invalid timeout value")
			return fmt.Errorf("超时时间必须是正整数（秒）")
		}
		cfg.Timeout = seconds
	default:
		logger.Warn("unknown config key")
		return fmt.Errorf("未知的配置项: %s\n支持的配置项: llm.base_url, llm.api_key, llm.model, llm.timeout", key)
	}

	if err := config.Save(cfg); err != nil {
		logger.Error("save config failed", "error", err)
		return err
	}

	attrs := []any{
		"key", key,
		"sensitive", isSensitiveConfigKey(key),
	}
	switch key {
	case "llm.base_url":
		attrs = append(attrs, "base_url", cfg.BaseURL)
	case "llm.model":
		attrs = append(attrs, "model", cfg.Model)
	case "llm.timeout":
		attrs = append(attrs, "timeout_seconds", cfg.Timeout)
	}
	logger.Info("config value updated", attrs...)

	fmt.Printf("已设置 %s = %s\n", key, value)
	return nil
}

// runConfigGet 获取指定配置项的值。
func runConfigGet(key string) error {
	logger := namedLogger(commandPathConfigGet).With("key", key)
	logger.Info("config get started", "sensitive", isSensitiveConfigKey(key))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config failed", "error", err)
		return err
	}

	var value string
	switch key {
	case "llm.base_url":
		value = cfg.BaseURL
	case "llm.api_key":
		value = cfg.APIKey
	case "llm.model":
		value = cfg.Model
	case "llm.timeout":
		if cfg.Timeout > 0 {
			value = strconv.Itoa(cfg.Timeout)
		} else {
			value = fmt.Sprintf("%d (默认)", int(llm.DefaultTimeout.Seconds()))
		}
	default:
		logger.Warn("unknown config key")
		return fmt.Errorf("未知的配置项: %s\n支持的配置项: llm.base_url, llm.api_key, llm.model, llm.timeout", key)
	}

	if isSensitiveConfigKey(key) {
		logger.Info("config value retrieved")
	} else {
		logger.Info("config value retrieved", "value_length", len(value))
	}
	fmt.Println(value)
	return nil
}

// runConfigList 列出所有配置项，api_key 脱敏显示。
func runConfigList() error {
	logger := namedLogger(commandPathConfigList)
	logger.Info("config list started")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config failed", "error", err)
		return err
	}

	fmt.Printf("llm.base_url = %s\n", cfg.BaseURL)
	fmt.Printf("llm.api_key  = %s\n", maskKey(cfg.APIKey))
	fmt.Printf("llm.model    = %s\n", cfg.Model)
	if cfg.Timeout > 0 {
		fmt.Printf("llm.timeout  = %d 秒\n", cfg.Timeout)
	} else {
		fmt.Printf("llm.timeout  = %d 秒 (默认)\n", int(llm.DefaultTimeout.Seconds()))
	}
	logger.Info("config list completed",
		"base_url", cfg.BaseURL,
		"model", cfg.Model,
		"has_api_key", cfg.APIKey != "",
		"timeout_seconds", cfg.Timeout,
	)
	return nil
}

// maskKey 对 API 密钥进行脱敏，仅显示前4位和后4位。
func maskKey(key string) string {
	if key == "" {
		return "(未设置)"
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}
