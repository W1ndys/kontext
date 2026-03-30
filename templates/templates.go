package templates

import _ "embed"

//go:embed llm_system_prompt.tmpl
var SystemPrompt string // SystemPrompt 是 LLM 的系统提示词模板

//go:embed llm_user_prompt.tmpl
var UserPrompt string // UserPrompt 是 LLM 的用户提示词模板

//go:embed pack_refine_system.tmpl
var PackRefineSystem string // PackRefineSystem 是 Pack 精筛阶段的系统提示词

//go:embed pack_refine_user.tmpl
var PackRefineUser string // PackRefineUser 是 Pack 精筛阶段的用户提示词

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

//go:embed init_scan_manifest_system.tmpl
var InitScanManifestSystem string // InitScanManifestSystem 是分步生成 PROJECT_MANIFEST 的系统提示词

//go:embed init_scan_architecture_system.tmpl
var InitScanArchitectureSystem string // InitScanArchitectureSystem 是分步生成 ARCHITECTURE_MAP 的系统提示词

//go:embed init_scan_conventions_system.tmpl
var InitScanConventionsSystem string // InitScanConventionsSystem 是分步生成 CONVENTIONS 的系统提示词

//go:embed init_scan_contract_system.tmpl
var InitScanContractSystem string // InitScanContractSystem 是分步生成单个模块契约的系统提示词

//go:embed init_scan_depgraph_system.tmpl
var InitScanDepgraphSystem string // InitScanDepgraphSystem 是生成模块依赖关系图的系统提示词

//go:embed update_system.tmpl
var UpdateSystem string // UpdateSystem 是 update 命令的系统提示词

//go:embed update_architecture.tmpl
var UpdateArchitecture string // UpdateArchitecture 是更新 ARCHITECTURE_MAP 的用户提示词

//go:embed update_contract.tmpl
var UpdateContract string // UpdateContract 是更新模块契约的用户提示词

//go:embed update_contract_part1.tmpl
var UpdateContractPart1 string // UpdateContractPart1 是分段更新模块契约的第一部分（module/owns/not_responsible_for/depends_on）

//go:embed update_contract_part2.tmpl
var UpdateContractPart2 string // UpdateContractPart2 是分段更新模块契约的第二部分（public_interface/modification_rules）

//go:embed update_manifest.tmpl
var UpdateManifest string // UpdateManifest 是更新 PROJECT_MANIFEST 的用户提示词

//go:embed pack_identify_files_system.tmpl
var PackIdentifyFilesSystem string // PackIdentifyFilesSystem 是 Pack 文件识别阶段的系统提示词

//go:embed pack_identify_files_user.tmpl
var PackIdentifyFilesUser string // PackIdentifyFilesUser 是 Pack 文件识别阶段的用户提示词

//go:embed pack_generate_filename_system.tmpl
var PackGenerateFilenameSystem string // PackGenerateFilenameSystem 是 Pack 文件名生成阶段的系统提示词

//go:embed pack_generate_filename_user.tmpl
var PackGenerateFilenameUser string // PackGenerateFilenameUser 是 Pack 文件名生成阶段的用户提示词
