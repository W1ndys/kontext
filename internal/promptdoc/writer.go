package promptdoc

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gosimple/slug"
	"github.com/w1ndys/kontext/internal/fileutil"
)

// GenerateFilename 根据任务描述生成文件名，格式为 "20060102-150405_任务摘要.md"。
func GenerateFilename(task, hint string) string {
	ts := time.Now().Format("20060102-150405")

	base := truncateForSlug(task, 200)
	if trimmedHint := strings.TrimSpace(hint); trimmedHint != "" {
		base = trimmedHint
	}

	slugValue := slugify(base)
	if len(slugValue) > 50 {
		slugValue = slugValue[:50]
	}
	return fmt.Sprintf("%s_%s.md", ts, slugValue)
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

func truncateForSlug(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}

	runes := []rune(s)
	cut := maxRunes
	for cut > maxRunes/2 && cut < len(runes) && !isWordBoundary(runes[cut]) {
		cut--
	}
	return strings.TrimSpace(string(runes[:cut]))
}

func isWordBoundary(r rune) bool {
	switch r {
	case ' ', '\n', '\r', '\t', '-', '_', '.', ',', ':', ';':
		return true
	default:
		return false
	}
}

func slugify(s string) string {
	s = slug.Make(s)
	if s == "" {
		return "prompt"
	}
	return s
}
