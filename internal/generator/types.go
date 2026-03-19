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
