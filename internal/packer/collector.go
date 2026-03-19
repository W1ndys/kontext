package packer

import (
	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/schema"
)

const (
	maxFiles        = 20  // 最多收集的文件数量
	maxLinesPerFile = 100 // 每个文件最多读取的行数
	scanDepth       = 5   // 目录扫描的最大深度
)

// CandidateContext 保存为 Prompt 生成收集的候选上下文数据。
type CandidateContext struct {
	DirectoryTree []string            // 项目目录树
	MatchedFiles  []string            // 与任务匹配的文件列表
	CodeSnippets  map[string]string   // 文件路径到代码片段的映射
	Contracts     []schema.ModuleContract // 匹配的模块契约
}

// CollectContext 为指定任务从项目根目录收集候选上下文。
// 流程：扫描目录树 → 关键词匹配文件 → 读取代码片段 → 匹配模块契约。
func CollectContext(bundle *schema.Bundle, task string, root string) (*CandidateContext, error) {
	ctx := &CandidateContext{}

	// 扫描项目目录树
	allFiles, err := fileutil.ScanDirectoryTree(root, scanDepth)
	if err != nil {
		return nil, err
	}
	ctx.DirectoryTree = allFiles

	// 根据任务关键词匹配文件
	matched := MatchFiles(task, allFiles)

	// 如果没有匹配到任何文件，回退为包含所有 Go 文件
	if len(matched) == 0 {
		goFiles, _ := fileutil.FindGoFiles(root)
		matched = goFiles
	}

	// 限制最大文件数
	if len(matched) > maxFiles {
		matched = matched[:maxFiles]
	}
	ctx.MatchedFiles = matched

	// 读取匹配文件的代码片段
	ctx.CodeSnippets = fileutil.ReadCodeSnippets(root, matched, maxLinesPerFile)

	// 匹配相关的模块契约
	ctx.Contracts = MatchContracts(task, bundle.Contracts)

	return ctx, nil
}
