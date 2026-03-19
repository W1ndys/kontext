package cache

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/w1ndys/kontext/internal/fileutil"
)

// ComputeProjectHash 计算项目目录结构的 hash，用于判断缓存是否过期。
// 基于文件列表的排序后 hash，如果目录结构发生变化则 hash 改变。
func ComputeProjectHash(projectDir string, maxDepth int) (string, error) {
	files, err := fileutil.ScanDirectoryTree(projectDir, maxDepth)
	if err != nil {
		return "", fmt.Errorf("扫描目录失败: %w", err)
	}

	// 规范化路径并排序
	normalized := make([]string, len(files))
	for i, f := range files {
		normalized[i] = filepath.ToSlash(f)
	}
	sort.Strings(normalized)

	// 计算 hash
	content := strings.Join(normalized, "\n")
	hash := sha256.Sum256([]byte(content))

	return fmt.Sprintf("%x", hash[:8]), nil // 取前 8 字节作为短 hash
}
