package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// CacheDir 是缓存目录相对于 .kontext 的路径
	CacheDir = ".kontext/.cache"

	// CheckpointFile 是检查点元数据文件名
	CheckpointFile = "scan_checkpoint.json"

	// CacheVersion 是当前缓存格式版本
	CacheVersion = "1.0"

	// CacheMaxAge 是缓存最大有效期
	CacheMaxAge = 24 * time.Hour
)

// Checkpoint 保存扫描流程的检查点信息。
type Checkpoint struct {
	Version         string          `json:"version"`
	CreatedAt       time.Time       `json:"created_at"`
	ProjectHash     string          `json:"project_hash"`
	CompletedStages []int           `json:"completed_stages"`
	CurrentStage    int             `json:"current_stage"`
	GeneratedFiles  map[string]bool `json:"generated_files"`
}

// SaveCheckpoint 保存检查点元数据到磁盘。
func SaveCheckpoint(cp *Checkpoint) error {
	if err := ensureCacheDir(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化检查点失败: %w", err)
	}

	path := filepath.Join(CacheDir, CheckpointFile)
	return os.WriteFile(path, data, 0644)
}

// LoadCheckpoint 从磁盘加载检查点元数据。
func LoadCheckpoint() (*Checkpoint, error) {
	path := filepath.Join(CacheDir, CheckpointFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("反序列化检查点失败: %w", err)
	}

	return &cp, nil
}

// SaveStageResult 保存某个阶段的结果到缓存文件。
func SaveStageResult(stage int, data interface{}) error {
	if err := ensureCacheDir(); err != nil {
		return err
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化阶段 %d 结果失败: %w", stage, err)
	}

	path := stageResultPath(stage)
	return os.WriteFile(path, jsonData, 0644)
}

// SaveStageResultPart 保存某个阶段的附属结果到缓存文件。
func SaveStageResultPart(stage int, part int, data interface{}) error {
	if err := ensureCacheDir(); err != nil {
		return err
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化阶段 %d-%d 结果失败: %w", stage, part, err)
	}

	path := stageResultPath(stage, part)
	return os.WriteFile(path, jsonData, 0644)
}

// LoadStageResult 从缓存文件加载某个阶段的结果。
func LoadStageResult(stage int, out interface{}) error {
	data, err := os.ReadFile(stageResultPath(stage))
	if err != nil {
		return fmt.Errorf("读取阶段 %d 缓存失败: %w", stage, err)
	}

	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("反序列化阶段 %d 结果失败: %w", stage, err)
	}

	return nil
}

// LoadStageResultPart 从缓存文件加载某个阶段的附属结果。
func LoadStageResultPart(stage int, part int, out interface{}) error {
	data, err := os.ReadFile(stageResultPath(stage, part))
	if err != nil {
		return fmt.Errorf("读取阶段 %d-%d 缓存失败: %w", stage, part, err)
	}

	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("反序列化阶段 %d-%d 结果失败: %w", stage, part, err)
	}

	return nil
}

// 根据阶段编号生成缓存结果文件的路径
func stageResultPath(parts ...int) string {
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		segments = append(segments, fmt.Sprintf("%d", part))
	}
	filename := fmt.Sprintf("stage-%s_result.json", strings.Join(segments, "-"))
	return filepath.Join(CacheDir, filename)
}

// IsCheckpointValid 检查缓存是否有效。
// 返回缓存是否有效、检查点信息和可能的错误。
func IsCheckpointValid(projectDir string, maxDepth int) (bool, *Checkpoint, error) {
	cp, err := LoadCheckpoint()
	if err != nil {
		return false, nil, nil // 没有缓存，不是错误
	}

	// 检查版本兼容性
	if cp.Version != CacheVersion {
		return false, cp, nil
	}

	// 检查是否过期
	if time.Since(cp.CreatedAt) > CacheMaxAge {
		return false, cp, nil
	}

	// 检查项目结构是否变化
	currentHash, err := ComputeProjectHash(projectDir, maxDepth)
	if err != nil {
		return false, cp, fmt.Errorf("计算项目 hash 失败: %w", err)
	}

	if cp.ProjectHash != currentHash {
		return false, cp, nil
	}

	return true, cp, nil
}

// UpdateCheckpointStage 更新检查点的当前阶段。
func UpdateCheckpointStage(cp *Checkpoint, stage int) error {
	// 将当前阶段标记为已完成
	found := false
	for _, s := range cp.CompletedStages {
		if s == stage {
			found = true
			break
		}
	}
	if !found {
		cp.CompletedStages = append(cp.CompletedStages, stage)
	}
	cp.CurrentStage = stage + 1

	return SaveCheckpoint(cp)
}

// UpdateGeneratedFile 更新检查点中已生成文件的状态。
func UpdateGeneratedFile(cp *Checkpoint, filename string, done bool) error {
	if cp.GeneratedFiles == nil {
		cp.GeneratedFiles = make(map[string]bool)
	}
	cp.GeneratedFiles[filename] = done
	return SaveCheckpoint(cp)
}

// ClearCache 清除所有缓存文件。
func ClearCache() error {
	return os.RemoveAll(CacheDir)
}

// NewCheckpoint 创建一个新的检查点。
func NewCheckpoint(projectHash string) *Checkpoint {
	return &Checkpoint{
		Version:         CacheVersion,
		CreatedAt:       time.Now(),
		ProjectHash:     projectHash,
		CompletedStages: []int{},
		CurrentStage:    1,
		GeneratedFiles:  make(map[string]bool),
	}
}

// ensureCacheDir 确保缓存目录存在。
func ensureCacheDir() error {
	return os.MkdirAll(CacheDir, 0755)
}
