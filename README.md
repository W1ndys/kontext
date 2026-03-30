# Kontext

Kontext 是一个面向 AI 辅助研发场景的 Go 命令行工具。它的目标不是直接帮你写代码，而是先把一个项目的关键知识编译成结构化上下文，供大模型或 AI 编程工具直接消费。

简单说，它解决的是这类问题：

- AI 不知道项目整体结构，每次都要重新解释
- 项目规范、模块边界、架构约束通常散落在代码、文档和人脑里
- 同一个任务换一个模型、换一个对话窗口，就得重复补上下文
- 全量喂代码成本高，而且效果并不稳定

Kontext 的做法是把这些信息沉淀到项目内的 `.kontext/` 目录中，AI 编程工具（Claude Code、Codex 等）可以直接读取这些结构化制品来理解项目。

## 它是干什么的

从代码实现和 `.kontext/` 中的工程说明看，Kontext 可以概括为一个”面向 AI 开发工作流的上下文编译器”：

- `init`：初始化 `.kontext/`，生成项目清单、架构图、编码约定、模块契约等上下文文件
- `validate`：校验 `.kontext/` 里的 YAML 配置是否合法
- `update`：检测项目源码变化，按需更新 `.kontext/` 中的物料
- `config`：管理全局 LLM 配置
- ~~`pack`~~：已弃用。此前用于按任务打包 Markdown Prompt，现在推荐让 AI 编程工具直接读取 `.kontext/` 目录

它既支持“交互式初始化”，也支持“扫描源码自动生成配置”，然后再进入“按任务打包 Prompt”的使用方式。

## 核心产物

Kontext 主要围绕项目根目录下的 `.kontext/` 工作。当前实现里最核心的产物包括：

- `.kontext/PROJECT_MANIFEST.yaml`
  项目全局清单，描述项目定位、业务背景、核心流程、技术栈、当前阶段
- `.kontext/ARCHITECTURE_MAP.yaml`
  项目的分层结构、模块归属和架构规则
- `.kontext/CONVENTIONS.yaml`
  编码规范、错误处理规则、AI 协作约束
- `.kontext/module_contracts/*_CONTRACT.yaml`
  每个模块的职责边界、依赖关系、对外接口和修改规则
- `.kontext/.cache/`
  `init --scan` 的阶段缓存和断点恢复数据

## 项目结构概览

仓库当前是一个标准的 Go CLI 工程，入口和分层比较清晰：

- `main.go`
  程序入口
- `cmd/`
  Cobra 命令层，对外暴露 `init`、`validate`、`update`、`config`
- `internal/generator/`
  初始化与扫描生成流程
- `internal/updater/`
  变更检测与物料更新
- `internal/schema/`
  `.kontext` 各类 YAML 的结构定义、加载和校验
- `internal/llm/`
  OpenAI 兼容接口封装、结构化输出和重试逻辑
- `internal/config/`
  全局 LLM 配置加载与保存
- `internal/cache/`
  扫描缓存与检查点恢复
- `templates/`
  提示词模板，使用 Go embed 打包进二进制
- `docs/`
  设计记录和方案演进文档
- `.kontext/`
  当前仓库自身的 Kontext 配置，可作为理解项目的参考样本

## 工作流程

典型使用流程如下：

1. 配置 LLM
2. 运行 `kontext init` 或 `kontext init --scan`
3. 检查并补充 `.kontext/` 中的 YAML 内容
4. 运行 `kontext validate`
5. 在 AI 编程工具（Claude Code、Codex 等）中告知 LLM 可以读取 `.kontext/` 目录获取项目上下文
6. 代码变更后运行 `kontext update` 保持上下文同步

## 安装

### 方式一：直接用 Go 安装

要求：

- Go 1.24.2 或更高版本

安装命令：

```bash
go install github.com/w1ndys/kontext@latest
```

安装完成后，确认 `GOBIN` 或 `GOPATH/bin` 已加入 `PATH`。

### 方式二：从源码构建

```bash
git clone https://github.com/w1ndys/kontext.git
cd kontext
go build -o dist/kontext .
```

Windows 下可输出为：

```powershell
go build -o dist/kontext.exe .
```

### 方式三：使用 Taskfile 构建

仓库内已经提供 `Taskfile.yml`，支持当前平台和多平台构建，例如：

```bash
task build
task build:all
```

## LLM 配置

Kontext 当前通过 OpenAI 兼容接口工作。配置优先级为：

- 环境变量
- `~/.kontext/config.yaml`
- 默认值

默认值：

- `llm.base_url = https://api.openai.com/v1`
- `llm.model = gpt-5.4`

### 方式一：交互式配置

```bash
kontext config
```

这个命令会引导你设置：

- API Base URL
- API Key
- 模型名
- 超时时间

如果 API 可访问，它还会尝试读取模型列表并让你在终端里选择。

### 方式二：命令行配置

```bash
kontext config set llm.base_url https://api.openai.com/v1
kontext config set llm.api_key your-api-key
kontext config set llm.model gpt-5.4
kontext config set llm.timeout 120
```

查看配置：

```bash
kontext config list
kontext config get llm.model
```

### 方式三：环境变量

```bash
export KONTEXT_LLM_BASE_URL=https://api.openai.com/v1
export KONTEXT_LLM_API_KEY=your-api-key
export KONTEXT_LLM_MODEL=gpt-5.4
export KONTEXT_LLM_TIMEOUT=120
```

Windows PowerShell：

```powershell
$env:KONTEXT_LLM_BASE_URL = "https://api.openai.com/v1"
$env:KONTEXT_LLM_API_KEY = "your-api-key"
$env:KONTEXT_LLM_MODEL = "gpt-5.4"
$env:KONTEXT_LLM_TIMEOUT = "120"
```

## 如何使用

### 命令速查

| 命令 | 说明 |
|------|------|
| `kontext -v` | 查看版本号 |
| `kontext init` | 交互式初始化 `.kontext/` 目录 |
| `kontext init --scan` | 自动扫描源码生成配置 |
| `kontext init --scan --fresh` | 忽略缓存，强制重新扫描 |
| `kontext init --scan --resume` | 从检查点继续（不询问） |
| `kontext validate` | 校验 `.kontext/` 下的 YAML 文件 |
| `kontext update` | 检测代码与物料偏差，确认后调用 LLM 更新 |
| `kontext config` | 交互式配置向导 |
| `kontext config set <key> <value>` | 设置配置项 |
| `kontext config get <key>` | 获取配置项 |
| `kontext config list` | 列出所有配置 |

### 1. 初始化 `.kontext`

#### 交互式初始化

```bash
kontext init
```

行为如下：

- 如果输入项目描述，会调用 LLM 进行交互式初始化
- 如果直接回车，会写入一套静态模板
- 如果当前目录已经存在 `.kontext/PROJECT_MANIFEST.yaml`，会先询问是否覆盖

#### 扫描源码自动生成

```bash
kontext init --scan
```

这个模式会分阶段执行：

1. 扫描目录树
2. 用 LLM 识别关键文件
3. 读取配置和依赖文件
4. 提取源码概要
5. 选择重点文件
6. 生成 `PROJECT_MANIFEST.yaml`
7. 生成 `ARCHITECTURE_MAP.yaml` 和 `CONVENTIONS.yaml`
8. 生成模块契约
9. 补充依赖与收尾

缓存与恢复：

```bash
kontext init --scan --fresh
kontext init --scan --resume
```

- `--fresh` 忽略缓存，强制重新扫描
- `--resume` 直接从有效检查点继续

### 2. 校验生成结果

```bash
kontext validate
```

当前会重点检查：

- `PROJECT_MANIFEST.yaml` 是否存在
- YAML 是否可解析
- `project.name` 等必要字段是否存在
- 架构图、规范、模块契约文件是否可解析

### 3. 根据代码变更更新 `.kontext`

```bash
kontext update
```

该命令不接收额外参数，执行流程如下：

1. **变更检测**：扫描项目源码目录，与 `.kontext/` 中已有的物料进行比对
   - 检查是否有新增或删除的包目录（对比 `ARCHITECTURE_MAP.yaml` 中的 `packages`）
   - 检查模块契约是否过期（对比导出符号和 owns 条目）
   - 检测是否存在缺少契约的新模块，或代码已删除但契约仍存在的废弃模块
   - 检查 `PROJECT_MANIFEST.yaml` 是否因技术栈变化等信号需要更新
2. **生成更新计划**：列出所有需要更新的物料及原因
3. **用户确认**：展示计划并等待用户确认（`y/N`）
4. **调用 LLM 执行更新**：逐个调用 LLM 重新生成受影响的 YAML 物料并写入文件

如果未检测到任何变更，命令会直接提示"未检测到需要更新的物料"并退出。

## 最小可用示例

如果你想最快验证一遍：

```bash
kontext config
kontext init --scan
kontext validate
```

然后在 AI 编程工具中引导 LLM 读取 `.kontext/` 目录即可。详见下方「与 AI 编程工具集成」。

## 与 AI 编程工具集成

生成 `.kontext/` 后，需要在 AI 编程工具中添加提示词，让 LLM 知道项目上下文的存在。根据你使用的工具，将以下内容加入对应的配置文件或对话中：

### Claude Code

在项目根目录的 `CLAUDE.md` 中添加：

```markdown
## 项目上下文

本项目使用 Kontext 生成了结构化上下文，存放在 `.kontext/` 目录中。开始任务前请先阅读以下制品：

- `.kontext/PROJECT_MANIFEST.yaml` — 项目清单（定位、技术栈、核心流程）
- `.kontext/ARCHITECTURE_MAP.yaml` — 架构分层与模块归属
- `.kontext/CONVENTIONS.yaml` — 编码规范与约束
- `.kontext/module_contracts/` — 各模块的职责边界与接口契约

请基于这些上下文理解项目结构后再进行开发。
```

### OpenAI Codex

在项目根目录的 `AGENTS.md` 中添加相同内容。

### Cursor

在项目根目录的 `.cursorrules` 中添加相同内容。

### 其他工具

如果工具不支持配置文件，可以在对话开始时直接发送：

```
请先阅读 .kontext/ 目录下的 YAML 文件了解项目上下文，然后再开始任务。
```

## 适用场景

当前更适合以下类型项目：

- CLI 工具
- Go / Python / Node.js 服务端项目
- 多模块后端系统
- AI Agent / LLM 应用
- 需要长期和多个 AI 工具协作的工程

## 当前实现特点

结合现有代码，Kontext 的几个关键实现点是：

- 使用 Cobra 构建 CLI
- 使用 `.kontext` YAML 作为项目知识的标准载体
- 使用 OpenAI 兼容 LLM 接口生成结构化结果
- 使用模板系统统一组织 Prompt
- `init --scan` 具备阶段缓存和断点恢复能力
- 生成的上下文制品可被 Claude Code、Codex 等 AI 编程工具直接读取

## 局限与注意事项

- 运行依赖可用的 LLM API Key
- 当前实现主要围绕 OpenAI 兼容接口
- 自动生成的 `.kontext` 不是最终真相，仍建议人工检查和修订
- 测试覆盖还不高，当前已有测试主要集中在 `internal/packer` 和 `internal/schema`
- 仓库内设计文档里提到的一些更大范围物料类型，并不代表当前版本都已经落地

## 开发与验证

本仓库当前可以通过：

```bash
go test ./...
```

我在当前代码上执行过该命令，测试通过。

## 参考

如果你要进一步理解这个项目，建议优先阅读：

- `.kontext/PROJECT_MANIFEST.yaml`
- `.kontext/ARCHITECTURE_MAP.yaml`
- `.kontext/CONVENTIONS.yaml`
- `cmd/`
- `internal/generator/`
- `docs/`

这几部分基本能覆盖“项目是什么、怎么工作、为什么这样设计”。
