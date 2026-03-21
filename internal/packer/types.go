package packer

// MentionedFiles 是 LLM 驱动的文件识别阶段返回的结构化响应。
// 用于从用户需求中识别被提到或语义指向的项目文件路径。
type MentionedFiles struct {
	Paths   []string          `json:"paths"`   // 识别出的文件相对路径列表
	Reasons map[string]string `json:"reasons"` // 每个文件的识别原因说明
}

// IdentifiedFiles 是文件识别结果，包含文件路径、原因和内容
type IdentifiedFiles struct {
	Files []IdentifiedFile `json:"files"`
}

// IdentifiedFile 是单个被识别的文件信息
type IdentifiedFile struct {
	Path     string `json:"path"`     // 文件相对路径
	Reason   string `json:"reason"`   // 为什么这个文件与需求相关
	Content  string `json:"content"`  // 文件内容（可能截断）
	Truncated bool  `json:"truncated"` // 内容是否被截断
}