package schema

// ArchitectureMap 描述项目的分层架构和架构规则。
type ArchitectureMap struct {
	Layers []Layer `json:"layers"`
	Rules  []Rule  `json:"rules"`
}

// Layer 描述一个架构层级，包含名称、说明和所属包。
type Layer struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Packages    []string `json:"packages"`
}

// Rule 描述一条架构规则及其原因。
type Rule struct {
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
}
