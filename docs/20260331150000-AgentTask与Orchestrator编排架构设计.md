# AgentTask 与 Orchestrator 编排架构设计

## 背景与动机

当前 Kontext 的 init 和 update 流程各自实现了一套"调用 LLM → 校验 → 重试 → 写文件"的逻辑，存在以下问题：

1. **串行瓶颈** — `generateAndWrite` 中 ARCHITECTURE_MAP 和 CONVENTIONS 只依赖 MANIFEST，不互相依赖，但仍串行执行。
2. **逻辑重复** — init 用 `GenerateSingleJSON`，update 用 `generateContractPartWithCorrection`，重试策略不一致。
3. **扩展困难** — 新增制品类型需要在 generator/updater 各改一处，无法统一配置 per-task 模型选择等策略。

本设计引入 `internal/agent/` 包，通过声明式的 `AgentTask` 和 DAG 编排器 `Orchestrator`，统一两条流程的执行逻辑。

## 设计目标

- 将"任务声明"与"执行编排"分离：generator/updater 只描述"要生成什么"，Orchestrator 负责"怎么调度"
- 自动并行化：同层无依赖的任务自动并行执行
- 统一 LLM 调用、校验、重试、进度报告逻辑
- 为 per-task 模型选择、自定义重试策略等未来扩展预留空间
- 先落地 init 流程，后续迁移 update 流程

## 核心类型

### AgentTask

```go
package agent

type AgentTask struct {
    // 任务唯一标识，如 "manifest"、"architecture"、"contract:internal/llm"
    ID string

    // 依赖的任务 ID 列表。Orchestrator 保证这些任务完成后才执行当前任务。
    DependsOn []string

    // 人类可读的任务描述，用于进度显示
    Label string

    // === 默认执行路径（Orchestrator 统一处理）===

    // LLM system prompt
    SystemPrompt string

    // 构建 user message。resolved 是已完成的依赖任务的输出，key 为任务 ID，value 为生成内容。
    BuildUserMsg func(resolved map[string]string) (string, error)

    // 校验 LLM 输出内容。校验失败会触发重试（将错误追加到对话让 LLM 修正）。
    // nil 表示不校验。
    Validate func(content string) error

    // 后处理 LLM 输出内容（如 FormatJSON、NormalizeContractJSON）。
    // 在校验通过后、写文件前执行。nil 表示不处理。
    PostProcess func(content string) (string, error)

    // 写入的文件路径。空字符串表示不写文件（结果仍存入 resolved map 供下游使用）。
    OutputPath string

    // LLM 调用校验失败时的修正重试次数。0 使用默认值（3）。
    // 注意：这不是网络层重试（网络重试在 internal/llm 已有）。
    MaxRetries int

    // === 自定义执行路径 ===

    // 有值时跳过默认的 "BuildUserMsg → LLM → Validate → Retry" 流程，
    // 直接调用此函数。返回的 content 仍走 PostProcess 和文件写入。
    // 用于 update 的分段契约生成等复杂场景。
    CustomExecute func(client llm.Client, resolved map[string]string) (string, error)
}
```

### 结果类型

```go
// TaskResult 存储单个任务的执行结果。
type TaskResult struct {
    ID       string
    Content  string        // LLM 生成的内容（校验 + 后处理后）
    Duration time.Duration
    Err      error
}

// RunResult 存储整次编排的执行结果。
type RunResult struct {
    Results  map[string]*TaskResult // ID → TaskResult
    Errors   []error                // 所有失败任务的错误
    Duration time.Duration          // 总耗时
}
```

### 进度事件

```go
type ProgressEvent struct {
    Type    ProgressType
    TaskID  string
    Label   string
    Message string
    Index   int // 当前已完成任务数（含本次）
    Total   int // 总任务数
}

type ProgressType int

const (
    ProgressTaskStart  ProgressType = iota // 任务开始执行
    ProgressLLMStart                        // LLM 调用开始
    ProgressLLMRetry                        // LLM 校验失败，重试
    ProgressTaskDone                        // 任务完成
    ProgressTaskFailed                      // 任务失败
)
```

## Orchestrator

### 接口

```go
type Orchestrator struct {
    client         llm.Client
    maxConcurrency int
    onProgress     func(ProgressEvent)
}

func NewOrchestrator(client llm.Client) *Orchestrator
func (o *Orchestrator) SetMaxConcurrency(n int)
func (o *Orchestrator) SetProgressHandler(h func(ProgressEvent))

// Run 执行任务 DAG，返回所有任务的结果。
func (o *Orchestrator) Run(tasks []*AgentTask) *RunResult
```

### 执行策略：分层并行

Orchestrator 对任务列表做拓扑排序，按依赖深度分层，同层任务并行执行。

**Init 流程的 DAG 示例：**

```
Layer 0: [manifest]                                    — 无依赖，单独执行
Layer 1: [architecture, conventions]                   — 都只依赖 manifest，并行
Layer 2: [contract:cmd, contract:internal/llm, ...]    — 依赖 architecture，并行（受 maxConcurrency 限制）
```

**单任务执行流程：**

```
1. 从 resolved map 收集依赖任务的输出
2. 判断执行路径：
   a. CustomExecute != nil → 直接调用 CustomExecute(client, resolved)
   b. 否则 → 默认路径：
      i.   调用 BuildUserMsg(resolved) 构建用户消息
      ii.  调用 LLM（system prompt + user msg）
      iii. 调用 Validate(content)
           - 失败 → 追加错误到对话历史，让 LLM 修正，重试（最多 MaxRetries 次）
      iv.  校验通过
3. 调用 PostProcess(content)（如有）
4. 写入 OutputPath（如有）
5. 将 content 存入 resolved map，供下游任务使用
```

### 错误传播

- 某任务失败时，所有直接/间接依赖它的下游任务**自动跳过**
- 不依赖失败任务的其他任务**继续执行**
- 示例：manifest 失败 → 全部跳过；architecture 失败 → 契约跳过，但 conventions 正常执行

## Init 流程集成

### 任务构建

generator 包新增 `tasks.go`，负责声明式地构建 AgentTask 列表：

```go
// internal/generator/tasks.go

type InitTaskOptions struct {
    Summary      string
    Conversation string
    KontextDir   string
    Modules      []string // scan init 已确定模块列表；interactive init 为空
}

// BuildInitTasks 构建 init 流程的 AgentTask DAG。
func BuildInitTasks(opts InitTaskOptions) []*agent.AgentTask
```

### 两阶段执行（interactive init）

Interactive init 在构建任务时不知道模块列表（需要从 architecture 输出中提取），采用两阶段 Run：

```
阶段 1: Orchestrator.Run([manifest, architecture, conventions])
        → 从 architecture 结果中提取模块列表
阶段 2: Orchestrator.Run([contract:mod1, contract:mod2, ...])
```

Scan init 在扫描阶段已确定模块列表，可一次性构建完整 DAG 直接 Run。

### 改造点

- `generateAndWrite` 改为：构建任务 → `Orchestrator.Run` → 处理 `RunResult`
- `RunInteractiveInit` 调用方式不变，内部实现迁移到 Orchestrator
- 删除 `GenerateModuleContracts`（并行逻辑由 Orchestrator 接管）

## Update 流程集成（阶段 2）

### 任务构建

```go
// internal/updater/tasks.go

// BuildUpdateTasks 将更新计划转换为 AgentTask DAG。
func BuildUpdateTasks(actions []UpdateAction, report *ChangeReport, kontextDir string) []*agent.AgentTask
```

根据 actions 中实际包含的 target 类型构建依赖关系：

| 场景 | DAG 结构 |
|---|---|
| 只更新 manifest | `[manifest]` |
| 更新 manifest + architecture | `manifest → architecture` |
| 更新 architecture + 若干 contract | `architecture → [contract:A, contract:B]` |
| 全量更新（--force） | `manifest → [architecture, conventions] → [contract:*, ...]` |

关键规则：只有 actions 中存在的 target 才创建任务。如果 contract 依赖的 architecture 不在本次更新范围内，contract 任务不依赖它（直接读取现有文件作为上下文）。

### 契约分段生成

Update 的契约生成采用分三段 LLM 调用 + 拼接的策略（Part1 / Part2a / Part2b），通过 `CustomExecute` 封装：

```go
// contract 任务的 CustomExecute 内部：
// 1. 调用 LLM 生成 Part1（module + owns + not_responsible_for + depends_on）
// 2. 调用 LLM 生成 Part2a（public_interface）
// 3. 调用 LLM 生成 Part2b（modification_rules）
// 4. 拼接三段并校验
```

分段是 update 契约的实现细节，不暴露到 DAG 层面。

### 改造点

- `Executor.Execute` 改为：构建任务 → `Orchestrator.Run` → 处理结果（含备份、校验）
- 备份逻辑移到 AgentTask 的 PostProcess 或 Orchestrator 的写文件前 hook
- 删除 `executeContractBatch`

## 包结构

```
internal/agent/
├── task.go          // AgentTask、TaskResult、RunResult、ProgressEvent 类型定义
├── orchestrator.go  // Orchestrator 实现（DAG 构建、分层并行调度）
└── executor.go      // 单任务执行逻辑（默认路径 + CustomExecute 分发 + 重试）
```

## 依赖关系

```
cmd/init.go ──→ internal/generator/ ──→ internal/agent/
                                    ──→ internal/llm/
                                    ──→ templates/

cmd/update.go ──→ internal/updater/ ──→ internal/agent/
                                    ──→ internal/llm/
                                    ──→ templates/

internal/agent/ ──→ internal/llm/      （调用 LLM）
                ──→ internal/fileutil/  （写文件）
```

`internal/agent/` 不依赖 generator、updater、schema。校验函数和后处理函数通过 AgentTask 的字段注入，避免反向依赖。

## 迁移路径

### 阶段 1：Init 流程（本次实施）

1. 新建 `internal/agent/` 包，实现 AgentTask + Orchestrator
2. generator 包新增 `tasks.go`，实现 `BuildInitTasks`
3. `generateAndWrite` 改为：构建任务 → `Orchestrator.Run` → 处理结果
4. 删除 `GenerateModuleContracts`（并行逻辑由 Orchestrator 接管）
5. 测试验证 init 流程输出与之前一致

### 阶段 2：Update 流程（后续实施）

1. updater 包新增 `tasks.go`，实现 `BuildUpdateTasks`
2. `Executor.Execute` 改为：构建任务 → `Orchestrator.Run`
3. 契约分段生成逻辑搬进 `CustomExecute`
4. 删除 `executeContractBatch`
5. 测试验证 update 流程

## 预期收益

- **性能提升**：ARCHITECTURE_MAP 和 CONVENTIONS 并行生成，init 流程中间阶段节省约 1 次 LLM 调用的等待时间
- **代码精简**：init 和 update 共享同一套执行/重试/进度逻辑，消除重复代码
- **可扩展性**：新增制品类型只需定义 AgentTask，无需修改编排逻辑；未来可轻松添加 per-task 模型选择
