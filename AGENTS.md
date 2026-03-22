# 仓库指南

## 项目结构与模块组织
`main.go` 是 CLI 入口。`cmd/` 存放 Cobra 命令，如 `init`、`pack`、`update`、`validate`、`config`。核心实现位于 `internal/`：`generator/` 负责生成 `.kontext` 元数据，`packer/` 负责组装 Prompt 文档，`updater/` 负责刷新生成物，`schema/` 负责 YAML 结构校验，`llm/` 负责封装 OpenAI 兼容客户端。`templates/` 保存嵌入式模板，`docs/` 保存设计文档。`dist/` 和 `.kontext/` 视为生成产物。

## 构建、测试与开发命令
请使用 Go 1.24.2 或更高版本。

- `go run . init --scan`：在当前仓库中本地运行 CLI。
- `go build -o dist/kontext .`：构建当前平台二进制到 `dist/`。
- `go test ./...`：运行全部测试。
- `task build`：通过 `Taskfile.yml` 构建当前平台版本。
- `task build:all`：交叉构建 Darwin、Linux、Windows 发布产物。

## 编码风格与命名约定
遵循标准 Go 格式化，使用 `gofmt` 或 `go fmt ./...` 处理缩进和空白。包名保持小写，导出标识符使用 `PascalCase`，内部辅助函数使用 `camelCase`。新增 Cobra 命令文件时，沿用现有 `cmd/<command>.go` 命名模式。修改面向用户的文本时，保持结构化 `slog` 日志风格，并确保中英双语帮助文案一致。

## 测试指南
测试使用 Go 内置 `testing` 包，并与源码并列放在 `*_test.go` 中。优先编写小而聚焦的单元测试，参考 `internal/packer` 与 `internal/schema` 中已有用例。凡是涉及 YAML 校验、Prompt 组装、文件筛选逻辑的行为变更，都应同步补充或更新测试。提交 PR 前运行 `go test ./...`。

## 提交与 Pull Request 规范
提交历史采用 Conventional Commit 风格，例如 `feat(cmd/pack.go): ...` 或 `fix(internal/generator/engine.go): ...`，scope 使用受影响的包或文件路径。每次提交应尽量聚焦，并说明用户可感知的变化，而不是只描述重构。PR 应包含简短说明、验证时执行的命令，以及交互式 CLI 流程变更对应的示例输出或截图。匹配 `v*` 的 tag 会触发发布流程，因此仅在完成测试后再创建发布 tag。

## 安全与配置提示
不要提交 `.env`、API Key，或生成的 `.kontext/` 缓存、Prompt、日志文件。LLM 本地配置优先使用环境变量或 `~/.kontext/config.yaml`。
