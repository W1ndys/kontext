package promptdoc

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/w1ndys/kontext/internal/fileutil"
)

const maxFilenameBaseRunes = 50

// GenerateFilename 根据任务描述生成文件名，格式为 "20060102150405-任务摘要.md"。
func GenerateFilename(task, title string) string {
	return generateFilenameAt(time.Now(), task, title)
}

// 根据指定时间生成带时间戳前缀的文件名
func generateFilenameAt(now time.Time, task, title string) string {
	base := task
	if trimmedTitle := strings.TrimSpace(title); trimmedTitle != "" {
		base = trimmedTitle
	}

	filenameBase := sanitizeFilenameBase(base)
	if filenameBase == "" {
		filenameBase = "prompt"
	}

	ts := now.Format("20060102150405")
	return fmt.Sprintf("%s-%s.md", ts, filenameBase)
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

// 将字符串清理为安全的文件名片段，去除特殊字符并用连字符连接
func sanitizeFilenameBase(s string) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if s == "" {
		return ""
	}

	var buf strings.Builder
	lastWasSeparator := false

	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			buf.WriteRune(unicode.ToLower(r))
			lastWasSeparator = false
		case unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r):
			if buf.Len() == 0 || lastWasSeparator {
				continue
			}
			buf.WriteRune('-')
			lastWasSeparator = true
		}
	}

	cleaned := strings.Trim(buf.String(), "-_. ")
	if cleaned == "" {
		return ""
	}

	return truncateFilenameBase(cleaned, maxFilenameBaseRunes)
}

// 将文件名片段截断到指定的最大 rune 数，尽量在连字符处断开
func truncateFilenameBase(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}

	cut := maxRunes
	for i := maxRunes; i > maxRunes/2; i-- {
		if runes[i-1] == '-' {
			cut = i - 1
			break
		}
	}

	return strings.Trim(string(runes[:cut]), "-_. ")
}
