package llm

import (
	"errors"
	"strings"
)

// ErrOutputTruncated 表示 LLM 输出因 token 限制被截断（finish_reason="length"）。
var ErrOutputTruncated = errors.New("LLM 输出因 token 限制被截断")

// IsStructuredOutputError 判断错误是否属于结构化输出本身的失败。
// 仅用于在调用方决定是否可以安全降级到普通 Chat 模式。
func IsStructuredOutputError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())
	structuredMarkers := []string{
		"结构化输出失败",
		"调用结构化输出失败",
		"解析结构化输出失败",
		"生成 json schema 失败",
		"解析 yaml 输出失败",
	}
	for _, marker := range structuredMarkers {
		if strings.Contains(errStr, marker) {
			return true
		}
	}

	return false
}
