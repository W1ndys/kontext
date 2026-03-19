package packer

import (
	"strings"

	"github.com/w1ndys/kontext/internal/schema"
)

// MatchContracts 根据任务描述中的关键词匹配相关的模块契约。
// 匹配范围：Module、Description、Owns 字段。
func MatchContracts(task string, contracts []schema.ModuleContract) []schema.ModuleContract {
	keywords := extractKeywords(task)
	if len(keywords) == 0 {
		return contracts // 无关键词时返回全部
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

// MatchFiles 根据任务描述中的关键词匹配相关的文件路径。
func MatchFiles(task string, files []string) []string {
	keywords := extractKeywords(task)
	if len(keywords) == 0 {
		return files
	}

	var matched []string
	for _, f := range files {
		lower := strings.ToLower(f)
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				matched = append(matched, f)
				break
			}
		}
	}
	return matched
}

// extractKeywords 从任务描述中提取有意义的关键词，过滤掉停用词。
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

	words := strings.Fields(strings.ToLower(task))
	var keywords []string
	for _, w := range words {
		w = strings.Trim(w, `"'.,:;!?()[]{}`)
		if len(w) >= 2 && !stopWords[w] {
			keywords = append(keywords, w)
		}
	}
	return keywords
}
