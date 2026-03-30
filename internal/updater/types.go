package updater

// DirectoryChange 描述目录或包的变更。
type DirectoryChange struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

// ContractChange 描述模块契约的变更。
type ContractChange struct {
	Module  string `json:"module"`
	Type    string `json:"type"`
	Details string `json:"details"`
}

// ChangeReport 汇总检测到的物料变化。
type ChangeReport struct {
	DirectoryChanges    []DirectoryChange
	ContractChanges     []ContractChange
	ManifestLikelyStale bool
	ManifestReasons     []string
	PackagePaths        []string
	ModuleSummaries     map[string]string
}

// UpdateAction 描述单个待执行的更新动作。
type UpdateAction struct {
	Target     string
	Reason     string
	Priority   int
	Module     string
	ChangeType string
}

// ProgressStage 描述 update 执行过程中的进度阶段。
type ProgressStage string

const (
	ProgressActionStart        ProgressStage = "action_start"
	ProgressLLMStart           ProgressStage = "llm_start"
	ProgressLLMTick            ProgressStage = "llm_tick"
	ProgressStructuredFallback ProgressStage = "structured_fallback"
	ProgressJSONRetry          ProgressStage = "json_retry"
	ProgressActionDone         ProgressStage = "action_done"
)

// ProgressEvent 描述一次 update 执行进度事件。
type ProgressEvent struct {
	Stage      ProgressStage
	Action     UpdateAction
	Index      int
	Total      int
	TargetPath string
	Message    string
}
