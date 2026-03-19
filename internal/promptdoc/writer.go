package promptdoc

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/w1ndys/kontext/internal/fileutil"
)

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// GenerateFilename 根据任务描述生成文件名，格式为 "20060102-150405_任务摘要.md"。
func GenerateFilename(task string) string {
	ts := time.Now().Format("20060102-150405")
	slug := slugify(task)
	if len(slug) > 50 {
		slug = slug[:50]
	}
	return fmt.Sprintf("%s_%s.md", ts, slug)
}

// slugify 将字符串转换为 URL 友好的 slug 格式。
func slugify(s string) string {
	s = strings.ToLower(s)
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// SavePrompt 将生成的 Prompt 内容保存到 .kontext/prompts/ 目录。
func SavePrompt(kontextDir, filename, content string) (string, error) {
	promptsDir := filepath.Join(kontextDir, "prompts")
	if err := fileutil.EnsureDir(promptsDir); err != nil {
		return "", fmt.Errorf("创建 prompts 目录失败: %w", err)
	}
	outPath := filepath.Join(promptsDir, filename)
	if err := fileutil.WriteFile(outPath, []byte(content)); err != nil {
		return "", fmt.Errorf("写入 prompt 文件失败: %w", err)
	}
	return outPath, nil
}
