package schema

// ProjectManifest 映射 .kontext/PROJECT_MANIFEST.json 的顶层结构。
type ProjectManifest struct {
	Project   Project   `json:"project"`
	TechStack TechStack `json:"tech_stack"`
	Scale     Scale     `json:"scale"`
	Status    Status    `json:"status"`
}

// Project 描述项目的基本信息和业务上下文。
type Project struct {
	Name            string     `json:"name"`
	OneLine         string     `json:"one_line"`
	Type            string     `json:"type"`
	BusinessContext string     `json:"business_context"`
	CoreFlows       []CoreFlow `json:"core_flows"`
}

// CoreFlow 描述一条核心业务流程。
type CoreFlow struct {
	Name       string `json:"name"`
	Steps      string `json:"steps"`
	EntryPoint string `json:"entry_point"`
}

// TechStack 描述项目的技术栈和关键技术决策。
type TechStack struct {
	Language        string        `json:"language"`
	CLIFramework    string        `json:"cli_framework"`
	DataFormat      string        `json:"data_format"`
	SchemaValidator string        `json:"schema_validator"`
	TemplateEngine  string        `json:"template_engine"`
	LLMClient       string        `json:"llm_client"`
	GitClient       string        `json:"git_client"`
	KeyDecisions    []KeyDecision `json:"key_decisions"`
}

// KeyDecision 描述一项关键的架构决策及其约束。
type KeyDecision struct {
	Decision   string `json:"decision"`
	Reason     string `json:"reason"`
	Constraint string `json:"constraint"`
}

// Scale 描述项目的规模概览。
type Scale struct {
	EstimatedFiles string `json:"estimated_files"`
	Modules        string `json:"modules"`
	Phase          string `json:"phase"`
}

// Status 描述项目各模块的当前开发状态。
type Status struct {
	CompletedModules []string `json:"completed_modules"`
	InProgress       []string `json:"in_progress"`
	NotStarted       []string `json:"not_started"`
}
