package packer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/schema"
)

const (
	maxCandidateFiles = 50
	maxContextFiles   = 20
	maxHighFileLines  = 500
	scanDepth         = 5
)

var supportedCodeExtensions = map[string]bool{
	".go": true, ".py": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
	".java": true, ".rs": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
	".rb": true, ".php": true, ".swift": true, ".kt": true, ".scala": true,
}

// CandidateContext 保存为 Prompt 生成收集的候选上下文数据。
type CandidateContext struct {
	DirectoryTree []string
	MatchedFiles  []string
	FileSummaries map[string]string
	CodeSnippets  map[string]string
	Contracts     []schema.ModuleContract
	RelevantFiles []FileRelevance
}

type scoredCandidate struct {
	path    string
	score   int
	summary string
}

// CollectContext 进行粗筛，返回候选文件路径和签名摘要。
func CollectContext(bundle *schema.Bundle, task string, root string) (*CandidateContext, error) {
	ctx := &CandidateContext{
		FileSummaries: make(map[string]string),
	}

	allFiles, err := fileutil.ScanDirectoryTree(root, scanDepth)
	if err != nil {
		return nil, err
	}
	sort.Strings(allFiles)
	ctx.DirectoryTree = allFiles

	sourceFiles := collectSourceFiles(allFiles)
	if len(sourceFiles) == 0 {
		return ctx, nil
	}

	candidates := make([]scoredCandidate, 0, len(sourceFiles))
	for _, relPath := range sourceFiles {
		fullPath := filepath.Join(root, relPath)
		summary, summaryErr := fileutil.ExtractFileSummary(fullPath)
		if summaryErr != nil {
			continue
		}
		ctx.FileSummaries[relPath] = summary

		score := matchScore(task, relPath, summary)
		if score > 0 {
			candidates = append(candidates, scoredCandidate{
				path:    relPath,
				score:   score,
				summary: summary,
			})
		}
	}

	if len(candidates) == 0 {
		for _, relPath := range sourceFiles {
			summary := ctx.FileSummaries[relPath]
			if summary == "" {
				continue
			}
			candidates = append(candidates, scoredCandidate{
				path:    relPath,
				score:   1,
				summary: summary,
			})
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].path < candidates[j].path
		}
		return candidates[i].score > candidates[j].score
	})

	if len(candidates) > maxCandidateFiles {
		candidates = candidates[:maxCandidateFiles]
	}

	ctx.MatchedFiles = make([]string, 0, len(candidates))
	filteredSummaries := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		ctx.MatchedFiles = append(ctx.MatchedFiles, candidate.path)
		filteredSummaries[candidate.path] = candidate.summary
	}
	ctx.FileSummaries = filteredSummaries
	ctx.Contracts = MatchContracts(task, bundle.Contracts)
	return ctx, nil
}

// HydrateContext 根据精筛结果读取最终上下文代码。
func HydrateContext(task string, root string, ctx *CandidateContext, refine *RefineResult) error {
	ctx.CodeSnippets = make(map[string]string)

	selected := selectRelevantFiles(ctx, refine)
	ctx.RelevantFiles = selected

	for _, file := range selected {
		if file.Relevance == "medium" {
			if summary := ctx.FileSummaries[file.Path]; summary != "" {
				ctx.CodeSnippets[file.Path] = summary
			}
			continue
		}

		content, err := readRelevantFile(root, file.Path, file.FocusAreas, maxHighFileLines)
		if err != nil {
			if summary := ctx.FileSummaries[file.Path]; summary != "" {
				ctx.CodeSnippets[file.Path] = summary
			}
			continue
		}
		ctx.CodeSnippets[file.Path] = content
	}

	if len(ctx.CodeSnippets) == 0 {
		for _, relPath := range ctx.MatchedFiles {
			if summary := ctx.FileSummaries[relPath]; summary != "" {
				ctx.CodeSnippets[relPath] = summary
			}
			if len(ctx.CodeSnippets) >= min(maxContextFiles, len(ctx.MatchedFiles)) {
				break
			}
		}
	}

	ctx.Contracts = filterContractsByRelevantFiles(task, ctx.Contracts, selected)
	return nil
}

func collectSourceFiles(allFiles []string) []string {
	files := make([]string, 0, len(allFiles))
	for _, relPath := range allFiles {
		if supportedCodeExtensions[strings.ToLower(filepath.Ext(relPath))] {
			files = append(files, relPath)
		}
	}
	return files
}

func matchScore(task, path, summary string) int {
	keywords := extractKeywords(task)
	if len(keywords) == 0 {
		return 1
	}

	lowerPath := strings.ToLower(path)
	lowerSummary := strings.ToLower(summary)

	score := 0
	for _, kw := range keywords {
		if strings.Contains(lowerPath, kw) {
			score += 3
		}
		if strings.Contains(lowerSummary, kw) {
			score += 2
		}
	}
	return score
}

func selectRelevantFiles(ctx *CandidateContext, refine *RefineResult) []FileRelevance {
	if refine == nil || len(refine.RelevantFiles) == 0 {
		selected := make([]FileRelevance, 0, min(maxContextFiles, len(ctx.MatchedFiles)))
		for i, relPath := range ctx.MatchedFiles {
			if i >= maxContextFiles {
				break
			}
			selected = append(selected, FileRelevance{
				Path:      relPath,
				Relevance: "high",
				Reason:    "基于关键词匹配回退选择",
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

func readRelevantFile(root, relPath string, focusAreas []string, maxLines int) (string, error) {
	fullPath := filepath.Join(root, relPath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return strings.TrimSpace(content), nil
	}

	windows := focusWindows(lines, focusAreas, maxLines)
	if len(windows) == 0 {
		return strings.Join(lines[:maxLines], "\n"), nil
	}

	var parts []string
	for _, window := range windows {
		header := fmt.Sprintf("// === %s:%d-%d ===", relPath, window.start+1, window.end)
		parts = append(parts, header)
		parts = append(parts, strings.Join(lines[window.start:window.end], "\n"))
	}
	return strings.Join(parts, "\n"), nil
}

type lineWindow struct {
	start int
	end   int
}

func focusWindows(lines []string, focusAreas []string, maxLines int) []lineWindow {
	if len(focusAreas) == 0 {
		return nil
	}

	lowerAreas := make([]string, 0, len(focusAreas))
	for _, area := range focusAreas {
		if trimmed := strings.TrimSpace(strings.ToLower(area)); trimmed != "" {
			lowerAreas = append(lowerAreas, trimmed)
		}
	}
	if len(lowerAreas) == 0 {
		return nil
	}

	windows := make([]lineWindow, 0, len(lowerAreas))
	for idx, line := range lines {
		lowerLine := strings.ToLower(line)
		for _, area := range lowerAreas {
			if strings.Contains(lowerLine, area) {
				start := max(0, idx-20)
				end := min(len(lines), idx+80)
				windows = append(windows, lineWindow{start: start, end: end})
				break
			}
		}
	}
	if len(windows) == 0 {
		return nil
	}

	sort.Slice(windows, func(i, j int) bool {
		return windows[i].start < windows[j].start
	})

	merged := make([]lineWindow, 0, len(windows))
	for _, window := range windows {
		if len(merged) == 0 {
			merged = append(merged, window)
			continue
		}
		last := &merged[len(merged)-1]
		if window.start <= last.end {
			if window.end > last.end {
				last.end = window.end
			}
			continue
		}
		merged = append(merged, window)
	}

	totalLines := 0
	trimmed := make([]lineWindow, 0, len(merged))
	for _, window := range merged {
		length := window.end - window.start
		if totalLines+length > maxLines {
			window.end = min(window.start+(maxLines-totalLines), window.end)
			length = window.end - window.start
		}
		if length <= 0 {
			break
		}
		totalLines += length
		trimmed = append(trimmed, window)
		if totalLines >= maxLines {
			break
		}
	}
	return trimmed
}

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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
