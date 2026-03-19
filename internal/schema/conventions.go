package schema

// Conventions 描述项目的编码规范和 AI 协作规则。
type Conventions struct {
	Coding        []ConventionItem `yaml:"coding"`
	ErrorHandling []ConventionItem `yaml:"error_handling"`
	Forbidden     []ConventionItem `yaml:"forbidden"`
	AIRules       []ConventionItem `yaml:"ai_rules"`
}

// ConventionItem 描述一条规范条目，包含规则、示例和原因。
type ConventionItem struct {
	Rule    string `yaml:"rule"`
	Example string `yaml:"example,omitempty"`
	Reason  string `yaml:"reason,omitempty"`
}
