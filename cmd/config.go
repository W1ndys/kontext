package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/w1ndys/kontext/internal/config"
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

---

Set a configuration value. Supported keys:
  llm.base_url   API endpoint URL
  llm.api_key    API key
  llm.model      Model name`,
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
	cfg, err := config.Load()
	if err != nil {
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

	// API Key
	display := maskKey(cfg.APIKey)
	fmt.Printf("API 密钥 [%s]: ", display)
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		cfg.APIKey = strings.TrimSpace(input)
	}

	// Model
	fmt.Printf("模型名称 [%s]: ", cfg.Model)
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		cfg.Model = strings.TrimSpace(input)
	}

	if err := config.Save(cfg); err != nil {
		return err
	}

	configPath, _ := config.GlobalConfigPath()
	fmt.Printf("\n配置已保存至 %s\n", configPath)
	return nil
}

// runConfigSet 设置指定的配置项。
func runConfigSet(key, value string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	switch key {
	case "llm.base_url":
		cfg.BaseURL = value
	case "llm.api_key":
		cfg.APIKey = value
	case "llm.model":
		cfg.Model = value
	default:
		return fmt.Errorf("未知的配置项: %s\n支持的配置项: llm.base_url, llm.api_key, llm.model", key)
	}

	if err := config.Save(cfg); err != nil {
		return err
	}

	fmt.Printf("已设置 %s = %s\n", key, value)
	return nil
}

// runConfigGet 获取指定配置项的值。
func runConfigGet(key string) error {
	cfg, err := config.Load()
	if err != nil {
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
	default:
		return fmt.Errorf("未知的配置项: %s\n支持的配置项: llm.base_url, llm.api_key, llm.model", key)
	}

	fmt.Println(value)
	return nil
}

// runConfigList 列出所有配置项，api_key 脱敏显示。
func runConfigList() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	fmt.Printf("llm.base_url = %s\n", cfg.BaseURL)
	fmt.Printf("llm.api_key  = %s\n", maskKey(cfg.APIKey))
	fmt.Printf("llm.model    = %s\n", cfg.Model)
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
