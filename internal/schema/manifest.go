package schema

// ProjectManifest 映射 .kontext/PROJECT_MANIFEST.yaml 的顶层结构。
type ProjectManifest struct {
	Project   Project   `yaml:"project"`
	TechStack TechStack `yaml:"tech_stack"`
	Scale     Scale     `yaml:"scale"`
	Status    Status    `yaml:"status"`
}

// Project 描述项目的基本信息和业务上下文。
type Project struct {
	Name            string     `yaml:"name"`
	OneLine         string     `yaml:"one_line"`
	Type            string     `yaml:"type"`
	BusinessContext string     `yaml:"business_context"`
	CoreFlows       []CoreFlow `yaml:"core_flows"`
}

// CoreFlow 描述一条核心业务流程。
type CoreFlow struct {
	Name       string `yaml:"name"`
	Steps      string `yaml:"steps"`
	EntryPoint string `yaml:"entry_point"`
}

// TechStack 描述项目的技术栈和关键技术决策。
type TechStack struct {
	Language        string        `yaml:"language"`
	CLIFramework    string        `yaml:"cli_framework"`
	YAMLParser      string        `yaml:"yaml_parser"`
	SchemaValidator string        `yaml:"schema_validator"`
	TemplateEngine  string        `yaml:"template_engine"`
	LLMClient       string        `yaml:"llm_client"`
	GitClient       string        `yaml:"git_client"`
	KeyDecisions    []KeyDecision `yaml:"key_decisions"`
}

// KeyDecision 描述一项关键的架构决策及其约束。
type KeyDecision struct {
	Decision   string `yaml:"decision"`
	Reason     string `yaml:"reason"`
	Constraint string `yaml:"constraint"`
}

// Scale 描述项目的规模概览。
type Scale struct {
	EstimatedFiles string `yaml:"estimated_files"`
	Modules        string `yaml:"modules"`
	Phase          string `yaml:"phase"`
}

// Status 描述项目各模块的当前开发状态。
type Status struct {
	CompletedModules []string `yaml:"completed_modules"`
	InProgress       []string `yaml:"in_progress"`
	NotStarted       []string `yaml:"not_started"`
}
