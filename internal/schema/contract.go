package schema

// ModuleContract 定义模块的职责边界和依赖关系。
type ModuleContract struct {
	Module         string   `yaml:"module"`
	Description    string   `yaml:"description"`
	Owns           []string `yaml:"owns"`
	DependsOn      []string `yaml:"depends_on"`
	AllowedChanges []string `yaml:"allowed_changes"`
}
