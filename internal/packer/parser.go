package packer

import (
	"encoding/json"
	"regexp"
	"strings"
)

// stripCodeBlock 去除 LLM 返回内容中可能包裹的 markdown 代码块标记。
// 复制自 internal/generator/parser.go，避免跨模块耦合。
func stripCodeBlock(s string) string {
	s = strings.TrimSpace(s)
	// 匹配 ```json ... ``` 或 ``` ... ```
	re := regexp.MustCompile("(?s)^```(?:json)?\\s*\n?(.*?)\\s*```$")
	if m := re.FindStringSubmatch(s); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return s
}

// ParseMentionedFiles 解析 LLM 文件识别阶段的 JSON 响应。
func ParseMentionedFiles(raw string) (*MentionedFiles, error) {
	cleaned := stripCodeBlock(raw)

	var result MentionedFiles
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}

	return &result, nil
}