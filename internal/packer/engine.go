package packer

import (
	"fmt"
	"path/filepath"

	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/internal/promptdoc"
	"github.com/w1ndys/kontext/internal/schema"
)

// Engine 编排 Pack 流水线的 6 个阶段。
type Engine struct {
	llmClient  llm.Client
	kontextDir string
	projectDir string
}

// NewEngine 创建一个新的 Pack 引擎。
func NewEngine(llmClient llm.Client, kontextDir, projectDir string) *Engine {
	return &Engine{
		llmClient:  llmClient,
		kontextDir: kontextDir,
		projectDir: projectDir,
	}
}

// Pack 执行完整的 6 阶段流水线，返回生成的 Prompt 文件路径。
// 阶段 1: 加载 Bundle → 阶段 2: 收集上下文 → 阶段 3: 构建模板数据
// 阶段 4: 渲染提示词 → 阶段 5: 调用 LLM → 阶段 6: 保存 Prompt 文档
func (e *Engine) Pack(task string) (string, error) {
	// 阶段 1: 加载 .kontext/ 配置
	bundle, err := schema.LoadBundle(e.kontextDir)
	if err != nil {
		return "", fmt.Errorf("阶段 1 (加载配置): %w", err)
	}

	// 阶段 2: 收集候选上下文
	ctx, err := CollectContext(bundle, task, e.projectDir)
	if err != nil {
		return "", fmt.Errorf("阶段 2 (收集上下文): %w", err)
	}

	// 阶段 3: 构建模板数据
	data := BuildTemplateData(task, bundle, ctx)

	// 阶段 4: 渲染提示词
	systemPrompt, err := RenderSystemPrompt()
	if err != nil {
		return "", fmt.Errorf("阶段 4 (渲染系统提示词): %w", err)
	}
	userPrompt, err := RenderUserPrompt(data)
	if err != nil {
		return "", fmt.Errorf("阶段 4 (渲染用户提示词): %w", err)
	}

	// 阶段 5: 调用 LLM 生成内容
	resp, err := e.llmClient.Generate(&llm.GenerateRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
	})
	if err != nil {
		return "", fmt.Errorf("阶段 5 (LLM 生成): %w", err)
	}

	// 阶段 6: 保存 Prompt 文档
	filename := promptdoc.GenerateFilename(task)
	outPath, err := promptdoc.SavePrompt(e.kontextDir, filename, resp.Content)
	if err != nil {
		return "", fmt.Errorf("阶段 6 (保存文档): %w", err)
	}

	absPath, _ := filepath.Abs(outPath)
	return absPath, nil
}
