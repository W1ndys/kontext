package fileutil

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
