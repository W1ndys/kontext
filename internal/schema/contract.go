package schema

import "fmt"

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
