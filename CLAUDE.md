# CLAUDE.md

本文件为 Claude Code (claude.ai/code) 在本仓库中工作时提供指引。

## 项目概述

Kontext 是一个 Go 命令行工具，将项目知识编译为结构化上下文，再按任务打包成 Markdown Prompt 供大模型消费。核心工作目录为 `.kontext/`，包含 JSON 制品（项目清单、架构图、编码规范、模块契约），生成的 Prompt 文档输出到 `.kontext/prompts/`。

## 构建与测试命令

```bash
# 构建当前平台
task build                    # 输出到 dist/kontext
go build -o dist/kontext .    # 不使用 task runner

# 构建全平台
task build:all

# 运行全部测试
go test ./...

# 运行指定包的测试
go test ./internal/packer/...
go test ./internal/schema/...

# 运行单个测试
go test ./internal/packer/ -run TestCollector
```

目前测试集中在 `internal/packer/` 和 `internal/schema/`。

## 架构

**入口：** `main.go` → `cmd.Execute()` → `cmd/` 下的 Cobra 子命令

**命令层 (`cmd/`)：** 每个文件对应一个子命令 — `init`、`pack`、`validate`、`update`、`config`。根命令在 `PersistentPreRunE` 中通过 `internal/logging` 初始化结构化日志。

**核心流水线：**

- **`internal/generator/`** — `init` 命令逻辑。`Engine` 编排多阶段扫描/生成流程，`parser.go` 负责源码扫描，最终生成所有 `.kontext/` JSON 制品。
- **`internal/packer/`** — `pack` 命令逻辑。`Engine` 执行 9 阶段流水线：加载配置 → 识别文件 → 收集代码 → 匹配/精筛 → 组装 Markdown Prompt。关键组件：`identifier.go`（LLM 文件选择）、`collector.go`（源码读取）、`matcher.go`（关键词匹配）、`refiner.go`（LLM 精筛）、`builder.go`（Markdown 组装）。
- **`internal/updater/`** — `update` 命令逻辑。检测 git 变更，重新生成受影响的制品。

**公共基础设施：**

- **`internal/llm/`** — 基于 `Client` 接口的 OpenAI 兼容客户端。`openai.go` 实现结构化输出（JSON Schema）和重试逻辑，所有 LLM 调用均经由此处。
- **`internal/schema/`** — `.kontext/` 各类 JSON 文件的 Go 结构体定义 + `LoadBundle()` 一次性加载。
- **`internal/config/`** — 全局 LLM 配置（`~/.kontext/config.yaml`），支持环境变量覆盖。
- **`internal/cache/`** — `init --scan` 的检查点与断点恢复。
- **`internal/promptdoc/`** — Markdown Prompt 文档构建器。
- **`internal/fileutil/`** — 文件系统工具函数。
- **`templates/`** — 通过 Go `embed` 打包的提示词模板（`.tmpl` 文件），每个模板在 `templates.go` 中暴露为包级 `string` 变量。

## 关键约定

- 所有 LLM 交互使用 JSON Schema 结构化输出（参见 `invopop/jsonschema` 依赖和 `internal/llm/types.go` 中的响应类型）。
- 提示词模板使用 Go `text/template` 语法，编译时嵌入二进制。
- `.kontext/` 制品使用 JSON 格式存储，通过 `encoding/json` 处理。
- 全局 LLM 配置（`~/.kontext/config.yaml`）仍使用 YAML 格式（通过 `gopkg.in/yaml.v3`）。
- TUI 交互（交互式 init、config）使用 Bubble Tea（`charmbracelet/bubbletea`）。
- 代码注释和用户可见字符串使用中文，保持此惯例。

## 日志

通过 `log/slog` 实现结构化日志。在 `cmd/root.go` 的 `PersistentPreRunE` 中初始化。`cmd/logging_helpers.go` 负责敏感信息脱敏（API Key 等）。日志文件输出到 `.kontext/logs/`。

## 项目上下文

本项目使用 Kontext 生成了结构化上下文，存放在 `.kontext/` 目录中。开始任务前请先阅读以下制品：

- `.kontext/PROJECT_MANIFEST.json` — 项目清单（定位、技术栈、核心流程）
- `.kontext/ARCHITECTURE_MAP.json` — 架构分层与模块归属
- `.kontext/CONVENTIONS.json` — 编码规范与约束
- `.kontext/module_contracts/` — 各模块的职责边界与接口契约

请基于这些上下文理解项目结构后再进行开发。
