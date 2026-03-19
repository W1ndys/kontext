package templates

import _ "embed"

//go:embed llm_system_prompt.tmpl
var SystemPrompt string // SystemPrompt 是 LLM 的系统提示词模板

//go:embed llm_user_prompt.tmpl
var UserPrompt string // UserPrompt 是 LLM 的用户提示词模板

//go:embed init_interview_system.tmpl
var InitInterviewSystem string // InitInterviewSystem 是 AI 初始化对话阶段的系统提示词

//go:embed init_interview_user.tmpl
var InitInterviewUser string // InitInterviewUser 是 AI 初始化对话阶段的首条用户消息模板

//go:embed init_generate_system.tmpl
var InitGenerateSystem string // InitGenerateSystem 是 AI 初始化生成阶段的系统提示词

//go:embed init_generate_user.tmpl
var InitGenerateUser string // InitGenerateUser 是 AI 初始化生成阶段的用户消息模板

//go:embed init_scan_system.tmpl
var InitScanSystem string // InitScanSystem 是扫描模式的系统提示词

//go:embed init_scan_user.tmpl
var InitScanUser string // InitScanUser 是扫描模式的用户消息模板

//go:embed init_scan_analyze_system.tmpl
var InitScanAnalyzeSystem string // InitScanAnalyzeSystem 是扫描模式文件识别阶段的系统提示词

//go:embed init_scan_analyze_user.tmpl
var InitScanAnalyzeUser string // InitScanAnalyzeUser 是扫描模式文件识别阶段的用户消息模板

//go:embed init_scan_select_system.tmpl
var InitScanSelectSystem string // InitScanSelectSystem 是扫描模式重点文件选择阶段的系统提示词

//go:embed init_scan_select_user.tmpl
var InitScanSelectUser string // InitScanSelectUser 是扫描模式重点文件选择阶段的用户消息模板
