package schema

// Conventions 描述项目的编码规范和 AI 协作规则。
type Conventions struct {
	Coding        []ConventionItem `json:"coding"`
	ErrorHandling []ConventionItem `json:"error_handling"`
	Forbidden     []ConventionItem `json:"forbidden"`
	AIRules       []ConventionItem `json:"ai_rules"`
}

// ConventionItem 描述一条规范条目，包含规则、示例和原因。
type ConventionItem struct {
	Rule    string `json:"rule"`
	Example string `json:"example,omitempty"`
	Reason  string `json:"reason,omitempty"`
}
