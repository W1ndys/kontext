package updater

import (
	"fmt"
	"sort"
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
