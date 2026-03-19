package templates

import _ "embed"

//go:embed llm_system_prompt.tmpl
var SystemPrompt string // SystemPrompt 是 LLM 的系统提示词模板

//go:embed llm_user_prompt.tmpl
var UserPrompt string // UserPrompt 是 LLM 的用户提示词模板
