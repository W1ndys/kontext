package updater

import (
	"fmt"
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

// PlanTargetUpdate 为指定的目标生成更新动作。
// target 格式：architecture、manifest、contract:internal/config。
func PlanTargetUpdate(target string) ([]UpdateAction, error) {
	switch {
	case target == "architecture":
		return []UpdateAction{{
			Target:   "architecture",
			Reason:   "用户指定更新",
			Priority: 1,
		}}, nil
	case target == "manifest":
		return []UpdateAction{{
			Target:   "manifest",
			Reason:   "用户指定更新",
			Priority: 3,
		}}, nil
	case strings.HasPrefix(target, "contract:"):
		modulePath := strings.TrimPrefix(target, "contract:")
		if modulePath == "" {
			return nil, fmt.Errorf("请指定模块路径，例如 contract:internal/config")
		}
		return []UpdateAction{{
			Target:     target,
			Reason:     "用户指定更新",
			Priority:   2,
			Module:     modulePath,
			ChangeType: "stale_contract",
		}}, nil
	default:
		// 检查是否直接输入了模块路径（不带 contract: 前缀）
		if !strings.Contains(target, ":") {
			return nil, fmt.Errorf("未知的更新目标: %s\n支持的格式: architecture, manifest, contract:<模块路径>\n例如: contract:internal/config", target)
		}
		return nil, fmt.Errorf("未知的更新目标: %s", target)
	}
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
