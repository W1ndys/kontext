package fileutil

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

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
