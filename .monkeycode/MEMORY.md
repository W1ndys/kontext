# 用户指令记忆

本文件记录了用户的指令、偏好和教导，用于在未来的交互中提供参考。

## 格式

### 用户指令条目
用户指令条目应遵循以下格式：

[用户指令摘要]
- Date: [YYYY-MM-DD]
- Context: [提及的场景或时间]
- Instructions:
  - [用户教导或指示的内容，逐行描述]

### 项目知识条目
Agent 在任务执行过程中发现的条目应遵循以下格式：

[项目知识摘要]
- Date: [YYYY-MM-DD]
- Context: Agent 在执行 [具体任务描述] 时发现
- Category: [代码结构|代码模式|代码生成|构建方法|测试方法|依赖关系|环境配置]
- Instructions:
  - [具体的知识点，逐行描述]

## 去重策略
- 添加新条目前，检查是否存在相似或相同的指令
- 若发现重复，跳过新条目或与已有条目合并
- 合并时，更新上下文或日期信息
- 这有助于避免冗余条目，保持记忆文件整洁

## 条目

LLM 响应 JSON 解析流程
- Date: 2026-03-30
- Context: Agent 在修复 Issue #8 和 #9 时发现
- Category: 代码模式
- Instructions:
  - LLM 响应通过 `internal/llm/openai.go` 的四种模式处理：ChatStructured（结构化 JSON Schema）、Chat（纯文本）、ChatStream（流式）、Generate（简单单轮）
  - JSON 提取链：`stripJSONCodeBlock` -> `extractJSONFromText` -> `json.Decoder.Decode`
  - `ChatStructured` 使用 `json.NewDecoder().Decode()` 只读取第一个 JSON 值，而后续校验 `validateGeneratedContent` 使用 `json.Unmarshal` 严格要求整个输入为单一 JSON 值
  - `parser.go` 中的 Parse 函数只做 `stripCodeBlock` 不做 `extractJSONFromText`，不处理 thinking tokens
  - update 命令的契约生成采用分三段策略（Part1/Part2a/Part2b），每段通过 `ChatStructuredWithRetry` 生成后简单字符串拼接

项目分段契约生成流程
- Date: 2026-03-30
- Context: Agent 在修复 Issue #8 时发现
- Category: 代码模式
- Instructions:
  - `executor.go` 的 `generateContractInParts` 将契约分三段生成：Part1(module+owns+not_responsible_for+depends_on)、Part2a(public_interface)、Part2b(modification_rules)
  - 每段通过 `generateContractPartWithCorrection` 调用 LLM，LLM 被要求返回 `{"content": "..."}` 结构
  - 三段的 `Content` 值被简单字符串拼接（`strings.TrimRight` + `strings.TrimLeft`），假设各段是 JSON 片段
  - 如果 LLM 返回的每段 Content 是完整 JSON 对象（而非片段），拼接后就会产生多个顶层 JSON 值
