package updater

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/schema"
)

const detectScanDepth = 8

// DetectChanges 检测当前代码与 .kontext 物料之间的偏差。
func DetectChanges(kontextDir, projectDir, since string) (*ChangeReport, error) {
	bundle, err := schema.LoadBundle(kontextDir)
	if err != nil {
		return nil, err
	}

	allFiles, err := fileutil.ScanDirectoryTree(projectDir, detectScanDepth)
	if err != nil {
		return nil, err
	}

	packages := actualPackages(allFiles)
	modules := actualModules(allFiles)
	moduleSummaries := collectModuleSummaries(projectDir, modules)

	report := &ChangeReport{
		PackagePaths:    sortedKeys(packages),
		ModuleSummaries: moduleSummaries,
	}

	recordedPackages := make(map[string]bool)
	for _, layer := range bundle.Architecture.Layers {
		for _, pkg := range layer.Packages {
			if strings.TrimSpace(pkg) != "" {
				recordedPackages[filepath.ToSlash(pkg)] = true
			}
		}
	}

	for path := range packages {
		if !recordedPackages[path] {
			report.DirectoryChanges = append(report.DirectoryChanges, DirectoryChange{Path: path, Type: "added"})
		}
	}
	for path := range recordedPackages {
		if !packages[path] {
			report.DirectoryChanges = append(report.DirectoryChanges, DirectoryChange{Path: path, Type: "removed"})
		}
	}
	sort.Slice(report.DirectoryChanges, func(i, j int) bool {
		if report.DirectoryChanges[i].Type == report.DirectoryChanges[j].Type {
			return report.DirectoryChanges[i].Path < report.DirectoryChanges[j].Path
		}
		return report.DirectoryChanges[i].Type < report.DirectoryChanges[j].Type
	})

	existingContracts := make(map[string]schema.ModuleContract, len(bundle.Contracts))
	for _, contract := range bundle.Contracts {
		existingContracts[contract.Module.Name] = contract
	}

	for moduleName := range modules {
		contract, ok := existingContracts[moduleName]
		if !ok {
			report.ContractChanges = append(report.ContractChanges, ContractChange{
				Module:  moduleName,
				Type:    "new_module",
				Details: "代码中存在该模块，但 .kontext/module_contracts 中缺少对应契约",
			})
			continue
		}

		if details := detectStaleContract(contract, moduleSummaries[moduleName]); details != "" {
			report.ContractChanges = append(report.ContractChanges, ContractChange{
				Module:  moduleName,
				Type:    "stale_contract",
				Details: details,
			})
		}
	}

	for moduleName := range existingContracts {
		if _, ok := modules[moduleName]; !ok {
			report.ContractChanges = append(report.ContractChanges, ContractChange{
				Module:  moduleName,
				Type:    "deleted_module",
				Details: "契约存在，但代码中已找不到该模块",
			})
		}
	}
	sort.Slice(report.ContractChanges, func(i, j int) bool {
		if report.ContractChanges[i].Type == report.ContractChanges[j].Type {
			return report.ContractChanges[i].Module < report.ContractChanges[j].Module
		}
		return report.ContractChanges[i].Type < report.ContractChanges[j].Type
	})

	if since != "" {
		changedFiles, diffErr := gitChangedFiles(projectDir, since)
		if diffErr != nil {
			return nil, diffErr
		}
		report.GitChangedFiles = changedFiles
		report.AffectedModules = affectedModulesFromFiles(changedFiles)
		report.ManifestReasons = append(report.ManifestReasons, manifestReasonsFromFiles(changedFiles)...)
	}

	report.ManifestReasons = append(report.ManifestReasons, manifestReasonsFromSignals(bundle, allFiles)...)
	report.ManifestReasons = uniqueStrings(report.ManifestReasons)
	report.ManifestLikelyStale = len(report.ManifestReasons) > 0

	return report, nil
}

// actualPackages 从文件列表中提取所有包含源码文件的目录路径。
func actualPackages(files []string) map[string]bool {
	result := make(map[string]bool)
	for _, relPath := range files {
		if !isSourceFile(relPath) {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(relPath))
		if dir == "." || dir == "" {
			continue
		}
		result[dir] = true
	}
	return result
}

// actualModules 从文件列表中提取模块名到文件列表的映射。
func actualModules(files []string) map[string][]string {
	result := make(map[string][]string)
	for _, relPath := range files {
		if !isSourceFile(relPath) {
			continue
		}
		moduleName := deriveModuleName(relPath)
		if moduleName == "" {
			continue
		}
		result[moduleName] = append(result[moduleName], relPath)
	}
	for moduleName := range result {
		sort.Strings(result[moduleName])
	}
	return result
}

// collectModuleSummaries 为每个模块收集代码摘要（每模块最多 8 个文件）。
func collectModuleSummaries(projectDir string, modules map[string][]string) map[string]string {
	result := make(map[string]string, len(modules))
	for moduleName, files := range modules {
		var parts []string
		for i, relPath := range files {
			if i >= 8 {
				break
			}
			summary, err := fileutil.ExtractFileSummary(filepath.Join(projectDir, relPath))
			if err != nil {
				continue
			}
			parts = append(parts, fmt.Sprintf("## %s\n%s", relPath, summary))
		}
		result[moduleName] = strings.Join(parts, "\n\n")
	}
	return result
}

// detectStaleContract 通过比对契约中 owns 条目和代码导出符号检测契约是否过期。
func detectStaleContract(contract schema.ModuleContract, summary string) string {
	if strings.TrimSpace(summary) == "" {
		return "当前代码摘要为空，无法验证契约内容"
	}

	lowerSummary := strings.ToLower(summary)
	missingOwns := 0
	for _, item := range contract.Owns {
		normalized, ok := normalizedOwnsProbe(item)
		if !ok {
			continue
		}
		if !strings.Contains(lowerSummary, normalized) {
			missingOwns++
		}
	}

	exported := extractExportedSymbols(summary)
	extra := 0
	for _, symbol := range exported {
		if !contractMentionsSymbol(contract, symbol) {
			extra++
		}
	}

	var reasons []string
	if missingOwns > 0 {
		reasons = append(reasons, fmt.Sprintf("%d 个 owns 条目在当前代码摘要中未命中", missingOwns))
	}
	if extra >= 3 {
		reasons = append(reasons, fmt.Sprintf("检测到 %d 个未记录的导出符号", extra))
	}
	return strings.Join(reasons, "；")
}

// normalizedOwnsProbe 将 owns 条目规范化为可用于代码匹配的小写探针。
func normalizedOwnsProbe(item string) (string, bool) {
	trimmed := strings.TrimSpace(strings.ToLower(item))
	if trimmed == "" {
		return "", false
	}

	// `owns` 通常是职责描述，不应要求逐字出现在源码摘要里。
	// 仅对路径、模块名、符号名这类可从代码直接验证的 ASCII 短条目做匹配。
	if len([]rune(trimmed)) > 48 {
		return "", false
	}
	if strings.Contains(trimmed, " ") {
		return "", false
	}
	if !regexp.MustCompile(`^[a-z0-9_./-]+$`).MatchString(trimmed) {
		return "", false
	}

	return trimmed, true
}

// extractExportedSymbols 从代码摘要中提取导出的函数和类型名。
func extractExportedSymbols(summary string) []string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`func\s+(?:\([^)]+\)\s+)?([A-Z]\w*)\s*\(`),
		regexp.MustCompile(`type\s+([A-Z]\w*)\s+(?:struct|interface)`),
	}

	seen := make(map[string]bool)
	var symbols []string
	for _, pattern := range patterns {
		matches := pattern.FindAllStringSubmatch(summary, -1)
		for _, match := range matches {
			if len(match) < 2 || seen[match[1]] {
				continue
			}
			seen[match[1]] = true
			symbols = append(symbols, match[1])
		}
	}
	sort.Strings(symbols)
	return symbols
}

// contractMentionsSymbol 检查契约中是否提及指定的导出符号。
func contractMentionsSymbol(contract schema.ModuleContract, symbol string) bool {
	lowerSymbol := strings.ToLower(symbol)
	for _, item := range contract.Owns {
		if strings.Contains(strings.ToLower(item), lowerSymbol) {
			return true
		}
	}
	for _, item := range contract.PublicInterface {
		if strings.EqualFold(item.Name, symbol) || strings.Contains(strings.ToLower(item.Signature), lowerSymbol) {
			return true
		}
	}
	return false
}

// gitChangedFiles 通过 git diff 获取指定 commit 以来的变更文件列表。
func gitChangedFiles(projectDir, since string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", fmt.Sprintf("%s..HEAD", since))
	cmd.Dir = projectDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("获取 git diff 失败: %w", err)
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, filepath.ToSlash(line))
		}
	}
	sort.Strings(files)
	return files, nil
}

// affectedModulesFromFiles 从变更文件列表推导受影响的模块。
func affectedModulesFromFiles(files []string) []string {
	var modules []string
	seen := make(map[string]bool)
	for _, relPath := range files {
		moduleName := deriveModuleName(relPath)
		if moduleName == "" || seen[moduleName] {
			continue
		}
		seen[moduleName] = true
		modules = append(modules, moduleName)
	}
	sort.Strings(modules)
	return modules
}

// manifestReasonsFromFiles 根据变更文件列表生成 Manifest 更新原因。
func manifestReasonsFromFiles(files []string) []string {
	signals := map[string]string{
		"go.mod":       "go.mod 发生变化，技术栈或依赖可能需要更新",
		"go.sum":       "go.sum 发生变化，依赖集合可能需要更新",
		"package.json": "package.json 发生变化，技术栈或脚本可能需要更新",
		"Taskfile.yml": "Taskfile.yml 发生变化，命令清单可能需要更新",
		"Dockerfile":   "Dockerfile 发生变化，部署/运行方式可能需要更新",
	}

	var reasons []string
	for _, relPath := range files {
		if reason, ok := signals[relPath]; ok {
			reasons = append(reasons, reason)
		}
	}
	return reasons
}

// manifestReasonsFromSignals 根据项目信号（语言检测不匹配等）生成 Manifest 更新原因。
func manifestReasonsFromSignals(bundle *schema.Bundle, files []string) []string {
	var reasons []string

	hasGoMod := containsFile(files, "go.mod")
	hasPackageJSON := containsFile(files, "package.json")
	language := strings.ToLower(bundle.Manifest.TechStack.Language)

	if hasGoMod && !strings.Contains(language, "go") {
		reasons = append(reasons, "项目存在 go.mod，但 PROJECT_MANIFEST 的 language 未体现 Go")
	}
	if hasPackageJSON && !strings.Contains(language, "js") && !strings.Contains(language, "ts") && !strings.Contains(language, "node") {
		reasons = append(reasons, "项目存在 package.json，但 PROJECT_MANIFEST 的 language 未体现 JS/TS")
	}

	return reasons
}

// containsFile 检查文件列表中是否包含指定文件。
func containsFile(files []string, target string) bool {
	for _, relPath := range files {
		if filepath.ToSlash(relPath) == target {
			return true
		}
	}
	return false
}

// deriveModuleName 从文件相对路径推导模块名（cmd->cmd, internal/x->x 等）。
func deriveModuleName(relPath string) string {
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

// isSourceFile 判断文件是否为源码文件。
func isSourceFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".java", ".rs", ".c", ".cpp", ".h", ".hpp", ".rb", ".php", ".swift", ".kt", ".scala":
		return true
	default:
		return false
	}
}

// sortedKeys 将 map 的键排序后返回切片。
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// uniqueStrings 对字符串切片去重。
func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	var result []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
