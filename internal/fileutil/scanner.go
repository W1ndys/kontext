package fileutil

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// signaturePatterns 定义各语言的函数/类/方法签名匹配模式
var signaturePatterns = map[string]*regexp.Regexp{
	".go":   regexp.MustCompile(`^func\s+`),                                                                              // Go: func Xxx(
	".py":   regexp.MustCompile(`^(def|class|async def)\s+`),                                                             // Python: def/class
	".js":   regexp.MustCompile(`^(function|class|export\s+(default\s+)?(function|class|const)|const\s+\w+\s*=.*=>)`),    // JS
	".ts":   regexp.MustCompile(`^(function|class|export\s+(default\s+)?(function|class|const|interface|type)|interface|type\s+\w+)`), // TS
	".tsx":  regexp.MustCompile(`^(function|class|export\s+(default\s+)?(function|class|const|interface|type)|interface|type\s+\w+)`), // TSX
	".jsx":  regexp.MustCompile(`^(function|class|export\s+(default\s+)?(function|class|const)|const\s+\w+\s*=.*=>)`),    // JSX
	".java": regexp.MustCompile(`^\s*(public|private|protected)?\s*(static)?\s*(class|interface|void|int|String|boolean|long|double|float|byte|short|char|\w+)\s+\w+\s*[\(<]`), // Java
	".rs":   regexp.MustCompile(`^(pub\s+)?(fn|struct|enum|impl|trait|mod)\s+`),                                          // Rust
	".c":    regexp.MustCompile(`^\w+[\s\*]+\w+\s*\(`),                                                                    // C: int main(
	".cpp":  regexp.MustCompile(`^\w+[\s\*]+\w+\s*\(`),                                                                    // C++
	".h":    regexp.MustCompile(`^\w+[\s\*]+\w+\s*\(`),                                                                    // C header
	".hpp":  regexp.MustCompile(`^\w+[\s\*]+\w+\s*\(`),                                                                    // C++ header
	".rb":   regexp.MustCompile(`^\s*(def|class|module)\s+`),                                                              // Ruby
	".php":  regexp.MustCompile(`^\s*(function|class|interface|trait|public|private|protected)\s+`),                      // PHP
	".swift": regexp.MustCompile(`^\s*(func|class|struct|enum|protocol)\s+`),                                             // Swift
	".kt":   regexp.MustCompile(`^\s*(fun|class|interface|object|data class)\s+`),                                        // Kotlin
	".scala": regexp.MustCompile(`^\s*(def|class|object|trait|case class)\s+`),                                           // Scala
}

// ScanDirectoryTree 扫描 root 目录下的所有文件，返回相对路径列表。
// maxDepth 控制最大递归深度；自动跳过隐藏目录和常见的非源码目录。
func ScanDirectoryTree(root string, maxDepth int) ([]string, error) {
	var files []string
	skipDirs := map[string]bool{
		"vendor": true, "node_modules": true, "dist": true,
		"build": true, "__pycache__": true, ".git": true,
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 跳过无法访问的路径
		}

		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}

		depth := strings.Count(rel, string(filepath.Separator))
		if depth >= maxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		files = append(files, rel)
		return nil
	})

	return files, err
}

// ReadCodeSnippets 读取每个文件的前 maxLines 行内容。
// 返回一个 map，键为相对路径，值为文件内容字符串。
func ReadCodeSnippets(root string, paths []string, maxLines int) map[string]string {
	result := make(map[string]string, len(paths))

	for _, p := range paths {
		fullPath := filepath.Join(root, p)
		content, err := readFirstLines(fullPath, maxLines)
		if err != nil {
			continue
		}
		result[p] = content
	}

	return result
}

// readFirstLines 读取文件的前 maxLines 行。
func readFirstLines(path string, maxLines int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() && len(lines) < maxLines {
		lines = append(lines, scanner.Text())
	}

	return strings.Join(lines, "\n"), nil
}

// ExtractFileSummary 提取文件概要：文件头（前 20 行）+ 函数/类签名。
// 概要包含 package 声明、import 块和所有函数/方法/类的签名行。
func ExtractFileSummary(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(path))
	pattern := signaturePatterns[ext]

	var header []string
	var signatures []string
	lineNum := 0

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// 前 20 行作为文件头（包含 package/import 等）
		if lineNum <= 20 {
			header = append(header, line)
			continue
		}

		// 20 行之后，只提取签名行
		if pattern != nil {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && pattern.MatchString(trimmed) {
				signatures = append(signatures, trimmed)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("// === 文件头 (前20行) ===\n")
	sb.WriteString(strings.Join(header, "\n"))

	if len(signatures) > 0 {
		sb.WriteString("\n\n// === 函数签名 ===\n")
		sb.WriteString(strings.Join(signatures, "\n"))
	}

	return sb.String(), nil
}

// ExtractModulesFromArchAndFiles 从 ARCHITECTURE_MAP JSON 中提取模块候选，
// 并与实际文件列表做交叉验证。只保留在文件系统中真实存在的路径。
// 如果 ARCHITECTURE_MAP 解析失败或验证后结果为空，回退到 ExtractModulesFromFileList。
func ExtractModulesFromArchAndFiles(archJSON string, allFiles []string) []string {
	// 解析 ARCHITECTURE_MAP（使用局部结构体避免 import cycle）
	var arch struct {
		Layers []struct {
			Packages []string `json:"packages"`
		} `json:"layers"`
	}
	if err := json.Unmarshal([]byte(archJSON), &arch); err != nil {
		return ExtractModulesFromFileList(allFiles)
	}

	// 构建目录前缀集合，用于快速验证候选路径
	dirSet := buildDirPrefixSet(allFiles)

	moduleSet := make(map[string]bool)
	for _, layer := range arch.Layers {
		for _, pkg := range layer.Packages {
			sanitized := sanitizePackagePath(pkg)
			if sanitized == "" {
				continue
			}
			if dirSet[sanitized] {
				moduleSet[sanitized] = true
			}
		}
	}

	if len(moduleSet) == 0 {
		return ExtractModulesFromFileList(allFiles)
	}

	var modules []string
	for mod := range moduleSet {
		modules = append(modules, mod)
	}
	sort.Strings(modules)
	return modules
}

// ExtractModulesFromFileList 从文件路径列表中提取模块名（纯文件系统兜底方案）。
// 不依赖任何硬编码规则，通过文件所在目录层级推断模块边界：
// - 文件在第 1 层目录下：该目录即模块（如 cmd/root.go → cmd）
// - 文件在第 2+ 层目录下：取前两层作为模块（如 internal/config/config.go → internal/config）
func ExtractModulesFromFileList(allFiles []string) []string {
	moduleSet := make(map[string]bool)

	for _, f := range allFiles {
		dir := filepath.ToSlash(filepath.Dir(f))
		parts := strings.Split(dir, "/")
		if len(parts) == 0 || parts[0] == "." {
			continue
		}

		if len(parts) >= 2 {
			moduleSet[parts[0]+"/"+parts[1]] = true
		} else {
			moduleSet[parts[0]] = true
		}
	}

	var modules []string
	for mod := range moduleSet {
		modules = append(modules, mod)
	}
	sort.Strings(modules)
	return modules
}

// ScanProjectModules 扫描项目目录并结合 ARCHITECTURE_MAP 提取模块列表。
// archJSON 为 ARCHITECTURE_MAP 的 JSON 内容，用于 LLM 辅助判断模块边界。
func ScanProjectModules(root, archJSON string) ([]string, error) {
	files, err := ScanDirectoryTree(root, 5)
	if err != nil {
		return nil, err
	}
	return ExtractModulesFromArchAndFiles(archJSON, files), nil
}

// buildDirPrefixSet 从文件路径列表构建所有目录前缀集合。
// 例如 "internal/config/config.go" 会产生 "internal" 和 "internal/config" 两个前缀。
func buildDirPrefixSet(allFiles []string) map[string]bool {
	dirs := make(map[string]bool)
	for _, f := range allFiles {
		parts := strings.Split(filepath.ToSlash(f), "/")
		for i := 1; i < len(parts); i++ {
			dirs[strings.Join(parts[:i], "/")] = true
		}
	}
	return dirs
}

// sanitizePackagePath 清洗 LLM 输出的包路径，去除描述文字、通配符等非法字符。
// 例如：
//
//	"internal/config (配置管理)" → "internal/config"
//	"llm_*.tmpl (通用模板)" → ""（无效路径，返回空）
func sanitizePackagePath(pkg string) string {
	// 移除括号描述
	if idx := strings.IndexByte(pkg, '('); idx >= 0 {
		pkg = pkg[:idx]
	}
	// 移除常见描述分隔符后的内容
	for _, sep := range []string{" —", " –", " - "} {
		if idx := strings.Index(pkg, sep); idx >= 0 {
			pkg = pkg[:idx]
		}
	}
	pkg = strings.TrimSpace(pkg)

	// 移除通配符和文件系统非法字符
	pkg = strings.Map(func(r rune) rune {
		if r == '*' || r == '?' || r == '<' || r == '>' || r == '|' || r == '"' {
			return -1
		}
		return r
	}, pkg)

	// 规范化路径分隔符
	pkg = filepath.ToSlash(pkg)
	pkg = strings.TrimRight(pkg, "/")

	if pkg == "" || pkg == "." {
		return ""
	}

	// 包含非 ASCII 字符（如中文）的路径视为无效
	for _, r := range pkg {
		if r > unicode.MaxASCII {
			return ""
		}
	}

	return pkg
}

// FindGoFiles 递归查找 root 目录下所有 .go 文件，返回相对路径列表。
func FindGoFiles(root string) ([]string, error) {
	var goFiles []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(info.Name(), ".go") {
			rel, _ := filepath.Rel(root, path)
			goFiles = append(goFiles, rel)
		}
		return nil
	})

	return goFiles, err
}
