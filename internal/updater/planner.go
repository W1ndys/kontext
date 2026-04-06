package updater

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/w1ndys/kontext/internal/schema"
)

// PlanUpdates 根据 ChangeReport 生成更新动作列表。
func PlanUpdates(report *ChangeReport) []UpdateAction {
	var actions []UpdateAction

	if len(report.DirectoryChanges) > 0 {
		actions = append(actions, UpdateAction{
			Target:   "architecture",
			Reason:   fmt.Sprintf("检测到 %d 处目录/包变更", len(report.DirectoryChanges)),
			Priority: 1,
		})
	}

	for _, change := range report.ContractChanges {
		actions = append(actions, UpdateAction{
			Target:     "contract:" + change.Module,
			Reason:     change.Details,
			Priority:   2,
			Module:     change.Module,
			ChangeType: change.Type,
		})
	}

	if report.ManifestLikelyStale {
		reason := "检测到 Manifest 可能过期"
		if len(report.ManifestReasons) > 0 {
			reason = report.ManifestReasons[0]
		}
		actions = append(actions, UpdateAction{
			Target:   "manifest",
			Reason:   reason,
			Priority: 3,
		})
	}

	sort.SliceStable(actions, func(i, j int) bool {
		if actions[i].Priority == actions[j].Priority {
			return actions[i].Target < actions[j].Target
		}
		return actions[i].Priority < actions[j].Priority
	})
	return actions
}

// PlanForceUpdateAll 生成强制更新所有制品的动作列表，无视变更检测。
func PlanForceUpdateAll(report *ChangeReport) []UpdateAction {
	var actions []UpdateAction

	// 架构图
	actions = append(actions, UpdateAction{
		Target:   "architecture",
		Reason:   "强制更新",
		Priority: 1,
	})

	// 所有模块契约（从代码中检测到的模块）
	modules := make(map[string]bool)
	for _, change := range report.ContractChanges {
		modules[change.Module] = true
	}
	// 补充已存在的契约模块
	for moduleName := range report.ModuleSummaries {
		modules[moduleName] = true
	}
	for moduleName := range modules {
		if moduleName == "" {
			continue
		}
		actions = append(actions, UpdateAction{
			Target:     "contract:" + moduleName,
			Reason:     "强制更新",
			Priority:   2,
			Module:     moduleName,
			ChangeType: "stale_contract",
		})
	}

	// 项目清单
	actions = append(actions, UpdateAction{
		Target:   "manifest",
		Reason:   "强制更新",
		Priority: 3,
	})

	sort.SliceStable(actions, func(i, j int) bool {
		if actions[i].Priority == actions[j].Priority {
			return actions[i].Target < actions[j].Target
		}
		return actions[i].Priority < actions[j].Priority
	})
	return actions
}

// FilterActionsByModules 过滤更新动作，只保留指定模块的契约更新。
// 非契约类型（architecture、manifest）在指定模块时不包含。
func FilterActionsByModules(actions []UpdateAction, modules []string) []UpdateAction {
	moduleSet := make(map[string]bool, len(modules))
	for _, m := range modules {
		moduleSet[filepath.ToSlash(strings.TrimSpace(m))] = true
	}

	var filtered []UpdateAction
	for _, action := range actions {
		if action.Module == "" {
			// 非契约类型（architecture/manifest），跳过
			continue
		}
		if moduleSet[filepath.ToSlash(action.Module)] {
			filtered = append(filtered, action)
		}
	}
	return filtered
}

// FilterActionsExcluding 过滤更新动作，排除指定目录下的模块契约更新。
// 非契约类型（architecture、manifest）始终保留。
// 排除逻辑：若模块路径以任一排除目录为前缀，则排除该契约更新。
func FilterActionsExcluding(actions []UpdateAction, excludes []string) []UpdateAction {
	normalized := make([]string, 0, len(excludes))
	for _, e := range excludes {
		p := filepath.ToSlash(strings.TrimSpace(e))
		if p != "" {
			normalized = append(normalized, p)
		}
	}

	var filtered []UpdateAction
	for _, action := range actions {
		if action.Module == "" {
			// 非契约类型（architecture/manifest），始终保留
			filtered = append(filtered, action)
			continue
		}
		modulePath := filepath.ToSlash(action.Module)
		excluded := false
		for _, exc := range normalized {
			if modulePath == exc || strings.HasPrefix(modulePath, exc+"/") {
				excluded = true
				break
			}
		}
		if !excluded {
			filtered = append(filtered, action)
		}
	}
	return filtered
}

// ListAvailableTargets 列出所有可更新的目标。
func ListAvailableTargets(kontextDir string) []string {
	targets := []string{"architecture", "manifest"}

	bundle, err := schema.LoadBundle(kontextDir)
	if err != nil {
		return targets
	}

	for _, c := range bundle.Contracts {
		key := schema.ContractModuleKey(c)
		if key != "" {
			targets = append(targets, "contract:"+key)
		}
	}
	sort.Strings(targets[2:])
	return targets
}
