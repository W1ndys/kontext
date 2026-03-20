package updater

import (
	"fmt"
	"sort"
)

// PlanUpdates 根据 ChangeReport 生成更新动作列表。
func PlanUpdates(report *ChangeReport, filter string) []UpdateAction {
	var actions []UpdateAction

	if allowTarget(filter, "architecture") && (len(report.DirectoryChanges) > 0 || filter == "architecture") {
		actions = append(actions, UpdateAction{
			Target:   "architecture",
			Reason:   fmt.Sprintf("检测到 %d 处目录/包变更", len(report.DirectoryChanges)),
			Priority: 1,
		})
	}

	if allowTarget(filter, "contracts") {
		for _, change := range report.ContractChanges {
			actions = append(actions, UpdateAction{
				Target:     "contract:" + change.Module,
				Reason:     change.Details,
				Priority:   2,
				Module:     change.Module,
				ChangeType: change.Type,
			})
		}
	}

	if allowTarget(filter, "manifest") && (report.ManifestLikelyStale || filter == "manifest") {
		reason := "用户显式请求更新 PROJECT_MANIFEST"
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

func allowTarget(filter, target string) bool {
	return filter == "" || filter == "all" || filter == target
}
