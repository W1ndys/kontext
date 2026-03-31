package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ModuleContract 定义模块的职责边界和依赖关系。
type ModuleContract struct {
	Module            ModuleInfo         `json:"module"`
	Owns              []string           `json:"owns"`
	NotResponsibleFor []string           `json:"not_responsible_for"`
	DependsOn         []ModuleDependency `json:"depends_on"`
	PublicInterface   []InterfaceItem    `json:"public_interface"`
	ModificationRules []ModificationRule `json:"modification_rules"`
}

// ModuleInfo 描述模块的基本信息。
type ModuleInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}

// ModuleDependency 描述模块依赖关系。
type ModuleDependency struct {
	Module string `json:"module"`
	Reason string `json:"reason"`
}

// InterfaceItem 描述模块对外暴露的接口。
type InterfaceItem struct {
	Name        string `json:"name"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
}

// ModificationRule 描述修改模块时必须遵守的规则。
type ModificationRule struct {
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
}

// Validate 校验模块契约的必填字段。
func (c ModuleContract) Validate() error {
	if c.Module.Name == "" {
		return fmt.Errorf("module.name 不能为空")
	}
	if c.Module.Path == "" {
		return fmt.Errorf("module.path 不能为空")
	}
	if c.Module.Purpose == "" {
		return fmt.Errorf("module.purpose 不能为空")
	}
	if len(c.Owns) == 0 {
		return fmt.Errorf("owns 为必填字段，至少需要 1 个条目")
	}
	for i, item := range c.Owns {
		if item == "" {
			return fmt.Errorf("owns[%d] 不能为空", i)
		}
	}
	return nil
}

// ContractFilename 将模块路径转换为契约文件名。
// 例如 "internal/config" → "internal_config.json"，"cmd" → "cmd.json"。
func ContractFilename(modulePath string) string {
	return strings.ReplaceAll(modulePath, "/", "_") + ".json"
}

// ContractModuleKey 从契约中提取用于匹配的模块标识符。
// 优先使用 Module.Path，回退到 Module.Name，两者均空时返回空字符串。
func ContractModuleKey(c ModuleContract) string {
	if c.Module.Path != "" {
		return c.Module.Path
	}
	return c.Module.Name
}

// NormalizeContractJSON 将契约 JSON 反序列化为 ModuleContract 再序列化回去，
// 确保字段顺序与结构体定义一致，并将 module.path 强制设为 modulePath（调用方传入的相对路径）。
func NormalizeContractJSON(content, modulePath string) (string, error) {
	var c ModuleContract
	if err := json.Unmarshal([]byte(content), &c); err != nil {
		return "", fmt.Errorf("解析契约 JSON 失败: %w", err)
	}
	if modulePath != "" {
		c.Module.Path = modulePath
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&c); err != nil {
		return "", fmt.Errorf("序列化契约 JSON 失败: %w", err)
	}
	return buf.String(), nil
}
