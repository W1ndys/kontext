package generator

// InterviewResponse 是 LLM 在对话阶段返回的结构化响应。
type InterviewResponse struct {
	Type     string   `json:"type"`     // "question" 或 "done"
	Question string   `json:"question"` // type=question 时的问题内容
	Options  []string `json:"options"`  // type=question 时的推荐选项
	Summary  string   `json:"summary"`  // type=done 时的需求摘要
}

// GeneratedYAML 是 LLM 在生成阶段返回的配置文件内容。
type GeneratedYAML struct {
	ProjectManifest string            `json:"project_manifest"`
	ArchitectureMap string            `json:"architecture_map"`
	Conventions     string            `json:"conventions"`
	ModuleContracts map[string]string `json:"module_contracts"` // 键为模块名（如 "cmd"），值为 YAML 内容
}

// AnalyzedFiles 是 LLM 在扫描阶段识别出的关键文件列表。
type AnalyzedFiles struct {
	ConfigFiles []string `json:"config_files"`
	SourceFiles []string `json:"source_files"`
}

// SelectedFiles 是 LLM 选择的重点文件列表。
type SelectedFiles struct {
	KeyFiles []string          `json:"key_files"`
	Reasons  map[string]string `json:"reasons"`
}

// SingleFileYAML 是分步生成单个文件时的响应结构。
type SingleFileYAML struct {
	Content string `json:"content"` // YAML 内容
}

// ModuleContractYAML 是单个模块契约生成的响应结构。
type ModuleContractYAML struct {
	ModuleName string `json:"module_name"` // 模块名
	Content    string `json:"content"`     // YAML 内容
}

// ModuleContractStreamEvent 表示模块契约流式生成过程中的事件。
type ModuleContractStreamEvent struct {
	ModuleName   string
	Attempt      int
	Delta        string
	Accumulated  string
	Done         bool
	Error        error
	FinalContent string
}

// ModuleDependencyGraph 是模块间依赖关系图。
type ModuleDependencyGraph struct {
	Modules []ModuleDep `json:"modules"`
}

// ModuleDep 是单个模块的依赖关系信息。
type ModuleDep struct {
	Name      string   `json:"name"`       // 模块名
	Path      string   `json:"path"`       // 模块路径
	Purpose   string   `json:"purpose"`    // 一句话描述
	DependsOn []string `json:"depends_on"` // 依赖的模块名列表
}
