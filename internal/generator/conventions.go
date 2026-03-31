package generator

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/w1ndys/kontext/internal/llm"
	"github.com/w1ndys/kontext/internal/schema"
	"github.com/w1ndys/kontext/templates"
)

// conventionsSection 定义 CONVENTIONS.json 中的一个 section。
type conventionsSection struct {
	Name       string // JSON 字段名，如 "coding"
	Label      string // 中文描述，如 "编码规范"
	SchemaHint string // 该 section 的 schema 示例
}

var conventionsSections = []conventionsSection{
	{
		Name:  "coding",
		Label: "编码规范",
		SchemaHint: `[{"rule": "编码规则", "example": "示例"}]
每条规则描述一个具体的编码规范，example 字段提供代码示例。`,
	},
	{
		Name:  "error_handling",
		Label: "错误处理",
		SchemaHint: `[{"rule": "错误处理规则", "example": "示例"}]
每条规则描述一个错误处理模式，example 字段提供代码示例。`,
	},
	{
		Name:  "forbidden",
		Label: "禁止事项",
		SchemaHint: `[{"rule": "禁止事项", "reason": "原因"}]
每条规则描述一个禁止行为，reason 字段解释为何禁止。`,
	},
	{
		Name:  "ai_rules",
		Label: "AI 协作规则",
		SchemaHint: `[{"rule": "AI 协作规则", "reason": "原因"}]
每条规则描述一条 AI 辅助开发的约束，reason 字段解释规则的必要性。`,
	},
}

// ConventionsSectionProgress 报告分 section 生成的进度。
type ConventionsSectionProgress struct {
	SectionIndex int    // 当前 section 索引（0-based）
	SectionTotal int    // section 总数
	SectionLabel string // 当前 section 中文名
	Done         bool   // 该 section 是否完成
	Err          error  // 该 section 是否出错
}

// conventionsSectionResult 保存单个 section 的生成结果。
type conventionsSectionResult struct {
	index   int
	name    string
	content string
	err     error
}

// GenerateConventionsInSections 分 section 并行生成 CONVENTIONS.json，避免单次输出过长被截断。
// 每个 section（coding、error_handling、forbidden、ai_rules）独立调用 LLM 生成，最后合并为完整 JSON。
func GenerateConventionsInSections(client llm.Client, userMsg string, onProgress func(ConventionsSectionProgress)) (string, error) {
	results := make([]conventionsSectionResult, len(conventionsSections))
	var wg sync.WaitGroup

	for i, sec := range conventionsSections {
		wg.Add(1)
		go func(idx int, sec conventionsSection) {
			defer wg.Done()

			if onProgress != nil {
				onProgress(ConventionsSectionProgress{
					SectionIndex: idx,
					SectionTotal: len(conventionsSections),
					SectionLabel: sec.Label,
				})
			}

			systemPrompt, err := RenderTemplate(templates.InitScanConventionsSectionSystem, map[string]string{
				"SectionName":   sec.Name,
				"SectionSchema": sec.SchemaHint,
			})
			if err != nil {
				results[idx] = conventionsSectionResult{index: idx, name: sec.Name, err: fmt.Errorf("渲染 %s 模板失败: %w", sec.Label, err)}
				if onProgress != nil {
					onProgress(ConventionsSectionProgress{
						SectionIndex: idx,
						SectionTotal: len(conventionsSections),
						SectionLabel: sec.Label,
						Done:         true,
						Err:          results[idx].err,
					})
				}
				return
			}

			sectionUserMsg := userMsg + fmt.Sprintf("\n\n请只生成 CONVENTIONS.json 中 \"%s\"（%s）部分的 JSON 数组。", sec.Name, sec.Label)

			content, err := GenerateSingleJSON(client, systemPrompt, sectionUserMsg)
			if err != nil {
				results[idx] = conventionsSectionResult{index: idx, name: sec.Name, err: fmt.Errorf("生成 %s 失败: %w", sec.Label, err)}
			} else {
				results[idx] = conventionsSectionResult{index: idx, name: sec.Name, content: content}
			}

			if onProgress != nil {
				onProgress(ConventionsSectionProgress{
					SectionIndex: idx,
					SectionTotal: len(conventionsSections),
					SectionLabel: sec.Label,
					Done:         true,
					Err:          results[idx].err,
				})
			}
		}(i, sec)
	}

	wg.Wait()

	// 收集错误
	var errs []string
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err.Error())
		}
	}
	if len(errs) > 0 {
		return "", fmt.Errorf("生成 CONVENTIONS 部分 section 失败:\n  %s", strings.Join(errs, "\n  "))
	}

	// 合并各 section 为完整的 Conventions JSON
	return mergeConventionsSections(results)
}

// mergeConventionsSections 将各 section 的 JSON 数组合并为完整的 Conventions JSON。
func mergeConventionsSections(results []conventionsSectionResult) (string, error) {
	conv := schema.Conventions{}

	for _, r := range results {
		// 每个 section 的 content 可能是纯 JSON 数组，也可能被包了一层对象
		items, err := parseConventionItems(r.content)
		if err != nil {
			return "", fmt.Errorf("解析 %s 的输出失败: %w", r.name, err)
		}

		switch r.name {
		case "coding":
			conv.Coding = items
		case "error_handling":
			conv.ErrorHandling = items
		case "forbidden":
			conv.Forbidden = items
		case "ai_rules":
			conv.AIRules = items
		}
	}

	// 序列化为格式化 JSON
	data, err := json.MarshalIndent(conv, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化 CONVENTIONS.json 失败: %w", err)
	}
	return string(data), nil
}

// parseConventionItems 解析 LLM 返回的 section 内容为 ConventionItem 数组。
// 支持两种格式：纯数组 [{"rule":...}] 或被对象包裹 {"coding": [{"rule":...}]}。
func parseConventionItems(content string) ([]schema.ConventionItem, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil
	}

	// 尝试直接解析为数组
	var items []schema.ConventionItem
	if err := json.Unmarshal([]byte(content), &items); err == nil {
		return items, nil
	}

	// 尝试解析为对象（LLM 可能返回了完整的 section 对象）
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &obj); err == nil {
		// 取第一个字段的值作为数组
		for _, val := range obj {
			if err := json.Unmarshal(val, &items); err == nil {
				return items, nil
			}
		}
	}

	return nil, fmt.Errorf("无法解析为 ConventionItem 数组: %s", truncate(content, 200))
}

// truncate 截断字符串到指定长度。
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
