package packer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/internal/promptdoc"
	"github.com/w1ndys/kontext/internal/schema"
)

const packStages = 10

// Engine 编排 Pack 流水线。
type Engine struct {
	llmClient     llm.Client
	kontextDir    string
	projectDir    string
	OnProgress    func(stage, total int, msg string)
	OnWarn        func(msg string) // 警告回调，替代直接 stderr 输出
	DisableRefine bool
	OutputPath    string // 用户指定的输出文件路径，为空时自动生成
}

// NewEngine 创建一个新的 Pack 引擎。
func NewEngine(llmClient llm.Client, kontextDir, projectDir string) *Engine {
	return &Engine{
		llmClient:  llmClient,
		kontextDir: kontextDir,
		projectDir: projectDir,
	}
}

// Pack 执行完整的 10 阶段流水线，返回生成的 Prompt 文件路径。
func (e *Engine) Pack(task string) (string, error) {
	e.progress(1, "加载 .kontext/ 配置...")
	bundle, err := schema.LoadBundle(e.kontextDir)
	if err != nil {
		return "", fmt.Errorf("阶段 1 (加载配置): %w", err)
	}

	e.progress(2, "扫描项目文件生成候选清单...")
	candidateFiles, err := ScanCandidateFiles(e.projectDir)
	if err != nil {
		return "", fmt.Errorf("阶段 2 (扫描候选文件): %w", err)
	}

	e.progress(3, "调用 LLM 识别需求相关文件...")
	var mentionedFiles *MentionedFiles
	// 构建架构和模块摘要
	archSummary := ""
	moduleSummary := ""
	if bundle != nil && len(bundle.Architecture.Layers) > 0 {
		var layers []string
		for _, l := range bundle.Architecture.Layers {
			layers = append(layers, fmt.Sprintf("- %s: %s", l.Name, l.Description))
		}
		archSummary = strings.Join(layers, "\n")
	}
	if bundle != nil && len(bundle.Contracts) > 0 {
		var modules []string
		for _, c := range bundle.Contracts {
			modules = append(modules, fmt.Sprintf("- %s: %s", c.Module.Name, c.Module.Purpose))
		}
		moduleSummary = strings.Join(modules, "\n")
	}

	mentionedFiles, err = IdentifyRelevantFiles(e.llmClient, task, candidateFiles, e.projectDir, archSummary, moduleSummary, func(attempt int, retryErr error, backoff time.Duration) {
		e.warn(fmt.Sprintf("⚠ 文件识别失败(%s)，%s 后重试第 %d 次...", retryErr, backoff, attempt))
	})
	if err != nil {
		e.warn(fmt.Sprintf("⚠ 文件识别失败，将继续打包但不附加源码：%v", err))
		mentionedFiles = nil
	}

	e.progress(4, "收集候选上下文...")
	ctx, err := CollectContext(bundle, task, e.projectDir, mentionedFiles)
	if err != nil {
		return "", fmt.Errorf("阶段 4 (收集候选上下文): %w", err)
	}
	if err := PreloadIdentifiedFiles(e.projectDir, ctx); err != nil {
		e.warn(fmt.Sprintf("⚠ 读取识别文件失败，将继续打包并使用当前可用上下文：%v", err))
	}

	var refine *RefineResult
	if !e.DisableRefine && len(ctx.MatchedFiles) > 0 {
		e.progress(5, "调用 LLM 精筛候选上下文...")
		candidates := make([]CandidateFile, 0, len(ctx.MatchedFiles))
		for _, relPath := range ctx.MatchedFiles {
			candidates = append(candidates, CandidateFile{
				Path:    relPath,
				Summary: ctx.FileSummaries[relPath],
			})
		}

		refine, err = RefineContext(e.llmClient, task, candidates, ctx.Contracts, ctx.IdentifiedFiles, func(attempt int, retryErr error, backoff time.Duration) {
			e.warn(fmt.Sprintf("⚠ Pack 精筛失败(%s)，%s 后重试第 %d 次...", retryErr, backoff, attempt))
		})
		if err != nil {
			e.warn(fmt.Sprintf("⚠ Pack 精筛失败，将继续使用已识别候选上下文：%v", err))
		}
	} else {
		e.progress(5, "跳过 LLM 精筛，继续使用已识别候选上下文...")
	}

	e.progress(6, "整理相关文件与上下文...")
	HydrateContext(task, ctx, refine)

	e.progress(7, "构建提示词模板...")
	data := BuildTemplateData(task, bundle, ctx)

	e.progress(8, "渲染提示词并调用 LLM 生成 Prompt 文档...")
	systemPrompt, err := RenderSystemPrompt()
	if err != nil {
		return "", fmt.Errorf("阶段 8 (渲染系统提示词): %w", err)
	}
	userPrompt, err := RenderUserPrompt(data)
	if err != nil {
		return "", fmt.Errorf("阶段 8 (渲染用户提示词): %w", err)
	}

	resp, err := llm.ChatWithRetry(e.llmClient, buildChatRequest(systemPrompt, userPrompt), 3, func(attempt int, retryErr error, backoff time.Duration) {
		e.warn(fmt.Sprintf("⚠ LLM 调用失败(%s)，%s 后重试第 %d 次...", retryErr, backoff, attempt))
	})
	if err != nil {
		return "", fmt.Errorf("阶段 8 (LLM 生成): %w", err)
	}

	e.progress(9, "生成输出文件名...")
	filenameTitle, err := GenerateFilenameSuggestion(e.llmClient, task, func(attempt int, retryErr error, backoff time.Duration) {
		e.warn(fmt.Sprintf("⚠ 文件名生成失败(%s)，%s 后重试第 %d 次...", retryErr, backoff, attempt))
	})
	if err != nil {
		e.warn(fmt.Sprintf("⚠ 文件名生成失败，将回退为默认命名：%v", err))
	}

	e.progress(10, "保存文件...")

	var outPath string
	if e.OutputPath != "" {
		// 用户指定了输出路径
		dir := filepath.Dir(e.OutputPath)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", fmt.Errorf("阶段 10 (创建输出目录): %w", err)
			}
		}
		if err := os.WriteFile(e.OutputPath, []byte(resp.Content), 0o644); err != nil {
			return "", fmt.Errorf("阶段 10 (保存文档): %w", err)
		}
		outPath = e.OutputPath
	} else {
		filename := promptdoc.GenerateFilename(task, filenameTitle)
		var err error
		outPath, err = promptdoc.SavePrompt(e.kontextDir, filename, resp.Content)
		if err != nil {
			return "", fmt.Errorf("阶段 10 (保存文档): %w", err)
		}
	}

	absPath, _ := filepath.Abs(outPath)
	return absPath, nil
}

// 触发进度回调通知，若未设置回调则忽略
func (e *Engine) progress(stage int, msg string) {
	if e.OnProgress != nil {
		e.OnProgress(stage, packStages, msg)
	}
}

// 触发警告回调，若未设置则忽略
func (e *Engine) warn(msg string) {
	if e.OnWarn != nil {
		e.OnWarn(msg)
	}
}

// 构建包含 system 和 user 消息的 LLM 聊天请求
func buildChatRequest(systemPrompt, userPrompt string) *llm.ChatRequest {
	return &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}
}