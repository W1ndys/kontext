package packer

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/schema"
)


var supportedCodeExtensions = map[string]bool{
	".go": true, ".py": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
	".java": true, ".rs": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
	".rb": true, ".php": true, ".swift": true, ".kt": true, ".scala": true,
}

// CandidateContext 保存为 Prompt 生成收集的候选上下文数据。
type CandidateContext struct {
	DirectoryTree    []string
	MatchedFiles     []string
	FileSummaries    map[string]string
	CodeSnippets     map[string]string
	Contracts        []schema.ModuleContract
	RelevantFiles    []FileRelevance
	MentionedReasons map[string]string
	IdentifiedFiles  []IdentifiedFile
}

// ScanCandidateFiles 扫描项目文件，返回候选文件路径列表
func ScanCandidateFiles(root string) ([]string, error) {
	return fileutil.ScanDirectoryTree(root, scanDepth)
}

// CollectContext 进行粗筛，返回候选文件路径和签名摘要。
// 如果提供了 mentionedFiles，将使用 LLM 识别结果；否则仅返回基础目录树与模块契约。
func CollectContext(bundle *schema.Bundle, task string, root string, mentionedFiles *MentionedFiles) (*CandidateContext, error) {
	ctx := &CandidateContext{
		FileSummaries:    make(map[string]string),
		MentionedReasons: make(map[string]string),
	}

	allFiles, err := fileutil.ScanDirectoryTree(root, scanDepth)
	if err != nil {
		return nil, err
	}
	sort.Strings(allFiles)
	ctx.DirectoryTree = allFiles

	sourceFiles := collectSourceFiles(allFiles)
	if len(sourceFiles) == 0 {
		ctx.Contracts = MatchContracts(task, bundle.Contracts)
		return ctx, nil
	}

	if mentionedFiles == nil || len(mentionedFiles.Paths) == 0 {
		ctx.Contracts = MatchContracts(task, bundle.Contracts)
		return ctx, nil
	}

	candidatePaths := mentionedFiles.Paths
	for path, reason := range mentionedFiles.Reasons {
		ctx.MentionedReasons[path] = reason
	}

	ctx.MatchedFiles = make([]string, 0, min(maxCandidateFiles, len(candidatePaths)))
	for _, relPath := range candidatePaths {
		if len(ctx.MatchedFiles) >= maxCandidateFiles {
			break
		}
		fullPath := filepath.Join(root, relPath)
		summary, summaryErr := fileutil.ExtractFileSummary(fullPath)
		if summaryErr == nil && summary != "" {
			ctx.FileSummaries[relPath] = summary
		}
		ctx.MatchedFiles = append(ctx.MatchedFiles, relPath)
	}

	ctx.Contracts = MatchContracts(task, bundle.Contracts)
	return ctx, nil
}

// PreloadIdentifiedFiles 预加载已识别文件的内容到上下文中，支持大小限制和截断处理。
func PreloadIdentifiedFiles(root string, ctx *CandidateContext) error {
	if len(ctx.MatchedFiles) == 0 {
		ctx.CodeSnippets = make(map[string]string)
		ctx.IdentifiedFiles = nil
		return nil
	}
	ctx.CodeSnippets = make(map[string]string)
	totalBytes := 0
	for _, relPath := range ctx.MatchedFiles {
		if totalBytes >= maxIdentifiedTotalBytes {
			break
		}
		content, truncated, err := readIdentifiedFileContent(root, relPath, &totalBytes)
		if err != nil {
			continue
		}
		if truncated {
			content = content + "\n\n[truncated]"
		}
		ctx.CodeSnippets[relPath] = content
	}
	ctx.IdentifiedFiles = buildIdentifiedFilesFromPaths(ctx.MatchedFiles, ctx.CodeSnippets, ctx.MentionedReasons)
	return nil
}

// HydrateContext 根据精筛结果读取最终上下文代码。
func HydrateContext(task string, ctx *CandidateContext, refine *RefineResult) {
	selected := selectRelevantFiles(ctx, refine)
	ctx.RelevantFiles = selected
	ctx.Contracts = filterContractsByRelevantFiles(task, ctx.Contracts, selected)
	ctx.IdentifiedFiles = buildIdentifiedFiles(selected, ctx.CodeSnippets, ctx.MentionedReasons)
}

// 根据精筛后的相关文件列表构建 IdentifiedFile 切片
func buildIdentifiedFiles(files []FileRelevance, snippets map[string]string, reasons map[string]string) []IdentifiedFile {
	result := make([]IdentifiedFile, 0, len(files))
	for _, file := range files {
		content := snippets[file.Path]
		if content == "" {
			continue
		}
		reason := file.Reason
		if reason == "" {
			if fallback, ok := reasons[file.Path]; ok {
				reason = fallback
			}
		}
		if reason == "" {
			reason = defaultIdentifiedReason
		}
		result = append(result, IdentifiedFile{
			Path:      file.Path,
			Reason:    reason,
			Content:   content,
			Truncated: strings.Contains(content, "[truncated]"),
		})
	}
	return result
}

// 根据路径列表构建 IdentifiedFile 切片
func buildIdentifiedFilesFromPaths(paths []string, snippets map[string]string, reasons map[string]string) []IdentifiedFile {
	result := make([]IdentifiedFile, 0, len(paths))
	for _, path := range paths {
		content := snippets[path]
		if content == "" {
			continue
		}
		reason := defaultIdentifiedReason
		if fallback, ok := reasons[path]; ok {
			reason = fallback
		}
		result = append(result, IdentifiedFile{
			Path:      path,
			Reason:    reason,
			Content:   content,
			Truncated: strings.Contains(content, "[truncated]"),
		})
	}
	return result
}

// 从全部文件列表中筛选出支持的源码文件
func collectSourceFiles(allFiles []string) []string {
	files := make([]string, 0, len(allFiles))
	for _, relPath := range allFiles {
		if supportedCodeExtensions[strings.ToLower(filepath.Ext(relPath))] {
			files = append(files, relPath)
		}
	}
	return files
}

// 根据精筛结果选择最终的相关文件列表
func selectRelevantFiles(ctx *CandidateContext, refine *RefineResult) []FileRelevance {
	if refine == nil || len(refine.RelevantFiles) == 0 {
		selected := make([]FileRelevance, 0, min(maxContextFiles, len(ctx.MatchedFiles)))
		for i, relPath := range ctx.MatchedFiles {
			if i >= maxContextFiles {
				break
			}
			reason := defaultIdentifiedReason
			if fallback, ok := ctx.MentionedReasons[relPath]; ok && strings.TrimSpace(fallback) != "" {
				reason = strings.TrimSpace(fallback)
			}
			selected = append(selected, FileRelevance{
				Path:      relPath,
				Relevance: "high",
				Reason:    reason,
			})
		}
		return selected
	}

	ordered := make([]FileRelevance, 0, len(refine.RelevantFiles))
	seen := make(map[string]bool, len(refine.RelevantFiles))
	for _, file := range refine.RelevantFiles {
		if seen[file.Path] || ctx.FileSummaries[file.Path] == "" {
			continue
		}
		if file.Relevance != "high" && file.Relevance != "medium" {
			continue
		}
		seen[file.Path] = true
		ordered = append(ordered, file)
		if len(ordered) >= maxContextFiles {
			break
		}
	}
	return ordered
}

// 读取单个识别文件的内容，支持大小限制和截断
func readIdentifiedFileContent(root, relPath string, totalBytes *int) (string, bool, error) {
	fullPath := filepath.Join(root, relPath)
	data, err := fileutil.ReadFile(fullPath)
	if err != nil {
		return "", false, err
	}

	remainingTotal := maxIdentifiedTotalBytes - *totalBytes
	if remainingTotal <= 0 {
		return "", false, errors.New("total byte limit reached")
	}

	limit := maxIdentifiedFileBytes
	if remainingTotal < limit {
		limit = remainingTotal
	}

	truncated := false
	if len(data) > limit {
		data = data[:limit]
		truncated = true
	}

	*totalBytes += len(data)
	return strings.TrimSpace(string(data)), truncated, nil
}

// 根据相关文件过滤出对应模块的契约
func filterContractsByRelevantFiles(task string, contracts []schema.ModuleContract, files []FileRelevance) []schema.ModuleContract {
	if len(files) == 0 {
		return MatchContracts(task, contracts)
	}

	modules := make(map[string]bool, len(files))
	for _, file := range files {
		moduleName := moduleNameFromPath(file.Path)
		if moduleName != "" {
			modules[moduleName] = true
		}
	}

	if len(modules) == 0 {
		return MatchContracts(task, contracts)
	}

	filtered := make([]schema.ModuleContract, 0, len(contracts))
	for _, contract := range contracts {
		if modules[contract.Module.Name] {
			filtered = append(filtered, contract)
		}
	}

	if len(filtered) == 0 {
		return MatchContracts(task, contracts)
	}
	return filtered
}

// 从文件路径推导所属模块名
func moduleNameFromPath(relPath string) string {
	normalized := filepath.ToSlash(relPath)
	parts := strings.Split(normalized, "/")
	if len(parts) == 0 {
		return ""
	}
	if parts[0] == "cmd" {
		return "cmd"
	}
	if (parts[0] == "internal" || parts[0] == "pkg") && len(parts) > 1 {
		return parts[1]
	}
	if strings.HasSuffix(normalized, ".go") {
		return "main"
	}
	return ""
}

// 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
