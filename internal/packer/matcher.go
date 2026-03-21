package packer

import (
	"strings"
	"unicode"

	"github.com/w1ndys/kontext/internal/schema"
)

// MatchContracts 根据任务描述中的关键词匹配相关的模块契约。
// 匹配范围：Module、Description、Owns 字段。
func MatchContracts(task string, contracts []schema.ModuleContract) []schema.ModuleContract {
	keywords := extractKeywords(task)
	if len(keywords) == 0 {
		return contracts
	}

	var matched []schema.ModuleContract
	for _, c := range contracts {
		searchable := strings.ToLower(c.Module.Name + " " + c.Module.Purpose + " " + strings.Join(c.Owns, " "))
		for _, kw := range keywords {
			if strings.Contains(searchable, kw) {
				matched = append(matched, c)
				break
			}
		}
	}
	return matched
}

// extractKeywords 从任务描述中提取关键词，兼容中英文输入。
func extractKeywords(task string) []string {
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "shall": true, "can": true, "need": true,
		"to": true, "of": true, "in": true, "for": true, "on": true,
		"with": true, "at": true, "by": true, "from": true, "and": true,
		"or": true, "not": true, "but": true, "if": true, "then": true,
		"that": true, "this": true, "it": true, "its": true, "as": true,
		"implement": true, "add": true, "create": true, "update": true,
		"fix": true, "refactor": true, "remove": true, "delete": true,
	}

	fields := strings.FieldsFunc(strings.ToLower(task), func(r rune) bool {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Han, r) {
			return false
		}
		return true
	})

	seen := make(map[string]bool)
	var keywords []string
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}

		if isHanString(field) {
			for _, token := range expandHanKeywords(field) {
				if !seen[token] {
					seen[token] = true
					keywords = append(keywords, token)
				}
			}
			continue
		}

		if len(field) >= 2 && !stopWords[field] && !seen[field] {
			seen[field] = true
			keywords = append(keywords, field)
		}
	}
	return keywords
}

func isHanString(s string) bool {
	for _, r := range s {
		if !unicode.Is(unicode.Han, r) {
			return false
		}
	}
	return s != ""
}

func expandHanKeywords(s string) []string {
	runes := []rune(s)
	if len(runes) == 0 {
		return nil
	}

	var result []string
	if len(runes) >= 2 {
		result = append(result, string(runes))
	}

	maxWindow := 4
	for size := 2; size <= maxWindow && size <= len(runes); size++ {
		for i := 0; i+size <= len(runes); i++ {
			result = append(result, string(runes[i:i+size]))
		}
	}
	return result
}
