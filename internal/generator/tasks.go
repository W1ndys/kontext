package generator

import (
	"fmt"
	"path/filepath"

	"github.com/w1ndys/kontext/internal/agent"
	"github.com/w1ndys/kontext/internal/schema"
	"github.com/w1ndys/kontext/templates"
)

const (
	// 任务 ID 常量
	TaskIDManifest     = "manifest"
	TaskIDArchitecture = "architecture"
	TaskIDConventions  = "conventions"
)

// ContractTaskID 返回模块契约任务的 ID。
func ContractTaskID(moduleName string) string {
	return "contract:" + moduleName
}

// InitTaskOptions 是构建 init 任务时的选项。
type InitTaskOptions struct {
	Summary      string // 需求摘要
	Conversation string // 完整对话记录
	KontextDir   string // .kontext 目录路径
}

// BuildInitTasks 构建 interactive init 流程的阶段 1 任务（manifest → architecture → conventions）。
// 返回的任务 DAG：
//
//	Layer 0: manifest
//	Layer 1: architecture (依赖 manifest)
//	Layer 2: conventions  (依赖 manifest + architecture)
func BuildInitTasks(opts InitTaskOptions) []*agent.AgentTask {
	kontextDir := opts.KontextDir

	manifestTask := &agent.AgentTask{
		ID:           TaskIDManifest,
		Label:        "生成 PROJECT_MANIFEST.json",
		SystemPrompt: templates.InitScanManifestSystem,
		BuildUserMsg: func(_ map[string]string) (string, error) {
			return RenderTemplate(templates.InitGenerateManifestUser, map[string]string{
				"Summary":      opts.Summary,
				"Conversation": opts.Conversation,
			})
		},
		Validate:    ValidateJSON,
		PostProcess: FormatJSON,
		OutputPath:  filepath.Join(kontextDir, "PROJECT_MANIFEST.json"),
	}

	architectureTask := &agent.AgentTask{
		ID:           TaskIDArchitecture,
		DependsOn:    []string{TaskIDManifest},
		Label:        "生成 ARCHITECTURE_MAP.json",
		SystemPrompt: templates.InitScanArchitectureSystem,
		BuildUserMsg: func(resolved map[string]string) (string, error) {
			return RenderTemplate(templates.InitGenerateArchitectureUser, map[string]string{
				"Summary":      opts.Summary,
				"Conversation": opts.Conversation,
				"Manifest":     resolved[TaskIDManifest],
			})
		},
		Validate:    ValidateJSON,
		PostProcess: FormatJSON,
		OutputPath:  filepath.Join(kontextDir, "ARCHITECTURE_MAP.json"),
	}

	conventionsTask := &agent.AgentTask{
		ID:           TaskIDConventions,
		DependsOn:    []string{TaskIDManifest, TaskIDArchitecture},
		Label:        "生成 CONVENTIONS.json",
		SystemPrompt: templates.InitScanConventionsSystem,
		BuildUserMsg: func(resolved map[string]string) (string, error) {
			return RenderTemplate(templates.InitGenerateConventionsUser, map[string]string{
				"Summary":      opts.Summary,
				"Manifest":     resolved[TaskIDManifest],
				"Architecture": resolved[TaskIDArchitecture],
			})
		},
		Validate:    ValidateJSON,
		PostProcess: FormatJSON,
		OutputPath:  filepath.Join(kontextDir, "CONVENTIONS.json"),
	}

	return []*agent.AgentTask{manifestTask, architectureTask, conventionsTask}
}

// BuildContractTasks 构建模块契约生成任务列表。
// 所有契约任务互相独立（同一 Layer），可完全并行。
// manifestContent 和 archContent 是阶段 1 的输出，通过闭包捕获。
func BuildContractTasks(opts InitTaskOptions, modules []string, manifestContent, archContent string) []*agent.AgentTask {
	kontextDir := opts.KontextDir
	contractDir := filepath.Join(kontextDir, "module_contracts")

	tasks := make([]*agent.AgentTask, 0, len(modules))
	for _, mod := range modules {
		tasks = append(tasks, &agent.AgentTask{
			ID:           ContractTaskID(mod),
			Label:        fmt.Sprintf("生成模块契约 %s", mod),
			SystemPrompt: templates.InitScanContractSystem,
			BuildUserMsg: func(_ map[string]string) (string, error) {
				return RenderTemplate(templates.InitGenerateContractUser, map[string]string{
					"Summary":      opts.Summary,
					"Manifest":     manifestContent,
					"Architecture": archContent,
					"ModuleName":   mod,
				})
			},
			Validate: ValidateJSON,
			PostProcess: func(content string) (string, error) {
				return schema.NormalizeContractJSON(content)
			},
			OutputPath: filepath.Join(contractDir, schema.ContractFilename(mod)),
		})
	}

	return tasks
}
