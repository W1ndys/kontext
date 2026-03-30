package packer

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/w1ndys/kontext/internal/schema"
)

// BuildTemplateData 将 Bundle 和 CandidateContext 转换为模板渲染所需的 TemplateData。
func BuildTemplateData(task string, bundle *schema.Bundle, ctx *CandidateContext) *TemplateData {
	data := &TemplateData{
		Task:           task,
		ProjectName:    bundle.Manifest.Project.Name,
		ProjectOneLine: bundle.Manifest.Project.OneLine,
		ProjectType:    bundle.Manifest.Project.Type,
		Phase:          bundle.Manifest.Scale.Phase,
	}

	// 构建技术栈摘要
	ts := bundle.Manifest.TechStack
	data.TechStack = fmt.Sprintf("Language: %s, CLI: %s, Data Format: %s",
		ts.Language, ts.CLIFramework, ts.DataFormat)

	// 业务上下文
	data.BusinessContext = bundle.Manifest.Project.BusinessContext

	// 架构信息
	if len(bundle.Architecture.Layers) > 0 || len(bundle.Architecture.Rules) > 0 {
		var parts []string
		for _, l := range bundle.Architecture.Layers {
			parts = append(parts, fmt.Sprintf("- **%s**: %s (packages: %s)",
				l.Name, l.Description, strings.Join(l.Packages, ", ")))
		}
		for _, r := range bundle.Architecture.Rules {
			parts = append(parts, fmt.Sprintf("- 规则: %s (原因: %s)", r.Rule, r.Reason))
		}
		data.Architecture = strings.Join(parts, "\n")
	}

	// 编码规范
	if len(bundle.Conventions.Coding) > 0 || len(bundle.Conventions.Forbidden) > 0 {
		var parts []string
		for _, c := range bundle.Conventions.Coding {
			parts = append(parts, fmt.Sprintf("- %s", c.Rule))
		}
		if len(bundle.Conventions.Forbidden) > 0 {
			parts = append(parts, "\n**禁止项:**")
			for _, f := range bundle.Conventions.Forbidden {
				parts = append(parts, fmt.Sprintf("- %s", f.Rule))
			}
		}
		if len(bundle.Conventions.AIRules) > 0 {
			parts = append(parts, "\n**AI 规则:**")
			for _, r := range bundle.Conventions.AIRules {
				parts = append(parts, fmt.Sprintf("- %s", r.Rule))
			}
		}
		data.Conventions = strings.Join(parts, "\n")
	}

	// 模块契约
	if len(ctx.Contracts) > 0 {
		var parts []string
		for _, c := range ctx.Contracts {
			var deps []string
			for _, d := range c.DependsOn {
				deps = append(deps, d.Module)
			}
			parts = append(parts, fmt.Sprintf("### %s\n- 职责: %s\n- 拥有: %s\n- 依赖: %s",
				c.Module.Name, c.Module.Purpose,
				strings.Join(c.Owns, ", "),
				strings.Join(deps, ", ")))
		}
		data.Contracts = strings.Join(parts, "\n\n")
	}

	if len(ctx.IdentifiedFiles) > 0 {
		var parts []string
		for _, file := range ctx.IdentifiedFiles {
			line := fmt.Sprintf("- `%s`: %s", file.Path, file.Reason)
			parts = append(parts, line)
		}
		data.RelevantFiles = strings.Join(parts, "\n")
		data.IdentifiedFiles = ctx.IdentifiedFiles
	}

	if len(ctx.DirectoryTree) > 0 {
		data.DirectoryTree = strings.Join(ctx.DirectoryTree, "\n")
	}

	if len(ctx.CodeSnippets) > 0 {
		var parts []string
		paths := make([]string, 0, len(ctx.CodeSnippets))
		for path := range ctx.CodeSnippets {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			content := ctx.CodeSnippets[path]
			language := markdownFenceLanguage(path)
			if language == "" {
				parts = append(parts, fmt.Sprintf("### `%s`\n```\n%s\n```", path, content))
				continue
			}
			parts = append(parts, fmt.Sprintf("### `%s`\n```%s\n%s\n```", path, language, content))
		}
		data.RelevantCode = strings.Join(parts, "\n\n")
	}

	return data
}

// 根据文件扩展名返回 Markdown 代码块的语言标识
func markdownFenceLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".jsx":
		return "jsx"
	case ".java":
		return "java"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt":
		return "kotlin"
	case ".scala":
		return "scala"
	default:
		return ""
	}
}
