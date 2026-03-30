package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/w1ndys/kontext/internal/fileutil"
	"github.com/w1ndys/kontext/internal/schema"
)

const detectScanDepth = 8

// DetectChanges 检测当前代码与 .kontext 物料之间的偏差。
func DetectChanges(kontextDir, projectDir string) (*ChangeReport, error) {
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

		contractPath := filepath.Join(kontextDir, "module_contracts", moduleName+"_CONTRACT.yaml")
		contractFresh := isContractFresherThanSource(contractPath, projectDir, modules[moduleName])

		if details := detectStaleContract(contract, moduleSummaries[moduleName], contractFresh); details != "" {
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
// contractFresh 为 true 表示契约文件比所有源码文件都新，此时跳过"未记录导出符号"检测。
func detectStaleContract(contract schema.ModuleContract, summary string, contractFresh bool) string {
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

	var reasons []string
	if missingOwns > 0 {
		reasons = append(reasons, fmt.Sprintf("%d 个 owns 条目在当前代码摘要中未命中", missingOwns))
	}

	// 如果契约文件比所有源码文件都新，说明刚更新过，跳过"未记录导出符号"检测
	if !contractFresh {
		exported := extractExportedSymbols(summary)
		extra := 0
		for _, symbol := range exported {
			if !contractMentionsSymbol(contract, symbol) {
				extra++
			}
		}
		if extra*10 > len(exported)*3 && extra >= 5 {
			reasons = append(reasons, fmt.Sprintf("检测到 %d 个未记录的导出符号", extra))
		}
	}
	return strings.Join(reasons, "；")
}

// isContractFresherThanSource 判断契约文件是否比模块所有源码文件都新。
// 如果是，说明契约刚被更新过，无需再次检测"未记录导出符号"。
func isContractFresherThanSource(contractPath, projectDir string, sourceFiles []string) bool {
	contractInfo, err := os.Stat(contractPath)
	if err != nil {
		return false
	}
	contractMod := contractInfo.ModTime()

	for _, relPath := range sourceFiles {
		fi, err := os.Stat(filepath.Join(projectDir, relPath))
		if err != nil {
			continue
		}
		if fi.ModTime().After(contractMod) {
			return false
		}
	}
	return true
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
// 搜索范围：owns、public_interface（Name/Signature/Description）、modification_rules、module.Purpose。
func contractMentionsSymbol(contract schema.ModuleContract, symbol string) bool {
	lowerSymbol := strings.ToLower(symbol)

	// owns 条目
	for _, item := range contract.Owns {
		if strings.Contains(strings.ToLower(item), lowerSymbol) {
			return true
		}
	}

	// public_interface: Name、Signature、Description
	for _, item := range contract.PublicInterface {
		if strings.EqualFold(item.Name, symbol) ||
			strings.Contains(strings.ToLower(item.Signature), lowerSymbol) ||
			strings.Contains(strings.ToLower(item.Description), lowerSymbol) {
			return true
		}
	}

	// modification_rules: Rule、Reason
	for _, rule := range contract.ModificationRules {
		if strings.Contains(strings.ToLower(rule.Rule), lowerSymbol) ||
			strings.Contains(strings.ToLower(rule.Reason), lowerSymbol) {
			return true
		}
	}

	// module.Purpose
	if strings.Contains(strings.ToLower(contract.Module.Purpose), lowerSymbol) {
		return true
	}

	return false
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

// 命名空间目录：子目录名即模块名（适用于多种语言的项目结构）。
var namespaceDirectories = map[string]bool{
	"internal": true, "pkg": true, "src": true, "lib": true,
	"app": true, "packages": true, "apps": true, "modules": true, "crates": true,
}

// 直接模块目录：目录本身即模块名。
var directModuleDirectories = map[string]bool{
	"cmd": true, "bin": true, "scripts": true, "templates": true,
}

// deriveModuleName 从文件相对路径推导模块名。
func deriveModuleName(relPath string) string {
	normalized := filepath.ToSlash(relPath)
	parts := strings.Split(normalized, "/")
	if len(parts) == 0 {
		return ""
	}
	if directModuleDirectories[parts[0]] {
		return parts[0]
	}
	if namespaceDirectories[parts[0]] && len(parts) > 1 {
		return parts[1]
	}
	// 根目录下的源码文件归属 main 模块
	if len(parts) == 1 && isSourceFile(normalized) {
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
