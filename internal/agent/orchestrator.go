package agent

import (
	"fmt"
	"sync"
	"time"

	"github.com/w1ndys/kontext/internal/llm"
)

// Orchestrator 编排一组 AgentTask 的 DAG 执行。
type Orchestrator struct {
	client         llm.Client
	maxConcurrency int
	onProgress     func(ProgressEvent)
}

// NewOrchestrator 创建一个新的编排器。
func NewOrchestrator(client llm.Client) *Orchestrator {
	return &Orchestrator{
		client:         client,
		maxConcurrency: 3,
	}
}

// SetMaxConcurrency 设置最大并发执行数。
func (o *Orchestrator) SetMaxConcurrency(n int) {
	if n > 0 {
		o.maxConcurrency = n
	}
}

// SetProgressHandler 设置进度回调。
func (o *Orchestrator) SetProgressHandler(h func(ProgressEvent)) {
	o.onProgress = h
}

// Run 执行任务 DAG，返回所有任务的结果。
// 1. 构建依赖图，拓扑排序分层
// 2. 同一层内的任务并行执行（受 maxConcurrency 限制）
// 3. 某任务失败时，所有直接/间接依赖它的任务跳过
// 4. 不依赖失败任务的其他任务继续执行
func (o *Orchestrator) Run(tasks []*AgentTask) *RunResult {
	start := time.Now()

	result := &RunResult{
		Results: make(map[string]*TaskResult),
	}

	if len(tasks) == 0 {
		result.Duration = time.Since(start)
		return result
	}

	// 构建 ID → task 索引
	taskMap := make(map[string]*AgentTask, len(tasks))
	for _, t := range tasks {
		taskMap[t.ID] = t
	}

	// 拓扑排序分层
	layers, err := topoSortLayers(tasks)
	if err != nil {
		result.Errors = append(result.Errors, err)
		result.Duration = time.Since(start)
		return result
	}

	// resolved 存储已完成任务的输出内容
	resolved := make(map[string]string)
	var resolvedMu sync.Mutex

	// failed 记录失败的任务 ID，用于跳过下游
	failed := make(map[string]bool)
	var failedMu sync.RWMutex

	completed := 0
	var completedMu sync.Mutex

	totalTasks := len(tasks)

	// 逐层执行
	for _, layer := range layers {
		sem := make(chan struct{}, o.maxConcurrency)
		var wg sync.WaitGroup

		for _, task := range layer {
			// 检查依赖是否有失败的
			if o.hasDependencyFailed(task, failed, &failedMu) {
				failedMu.Lock()
				failed[task.ID] = true
				failedMu.Unlock()

				skipErr := fmt.Errorf("任务 %s 因依赖任务失败而跳过", task.ID)
				result.Results[task.ID] = &TaskResult{
					ID:  task.ID,
					Err: skipErr,
				}
				result.Errors = append(result.Errors, skipErr)
				continue
			}

			wg.Add(1)
			go func(t *AgentTask) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				// 收集依赖结果
				resolvedMu.Lock()
				taskResolved := make(map[string]string, len(t.DependsOn))
				for _, dep := range t.DependsOn {
					taskResolved[dep] = resolved[dep]
				}
				resolvedMu.Unlock()

				exec := &taskExecutor{
					client:     o.client,
					onProgress: o.onProgress,
					total:      totalTasks,
					completed:  &completed,
					completedMu: &completedMu,
				}

				taskStart := time.Now()
				content, err := exec.execute(t, taskResolved)
				duration := time.Since(taskStart)

				tr := &TaskResult{
					ID:       t.ID,
					Content:  content,
					Duration: duration,
					Err:      err,
				}

				if err != nil {
					failedMu.Lock()
					failed[t.ID] = true
					failedMu.Unlock()

					o.emitProgress(ProgressEvent{
						Type:    ProgressTaskFailed,
						TaskID:  t.ID,
						Label:   t.Label,
						Message: err.Error(),
						Total:   totalTasks,
					})
				} else {
					resolvedMu.Lock()
					resolved[t.ID] = content
					resolvedMu.Unlock()
				}

				// 写入结果（result.Results 的写入需要同步）
				resolvedMu.Lock()
				result.Results[t.ID] = tr
				if err != nil {
					result.Errors = append(result.Errors, fmt.Errorf("任务 %s: %w", t.ID, err))
				}
				resolvedMu.Unlock()
			}(task)
		}

		wg.Wait()
	}

	result.Duration = time.Since(start)
	return result
}

// hasDependencyFailed 检查任务的依赖是否有失败的。
func (o *Orchestrator) hasDependencyFailed(task *AgentTask, failed map[string]bool, mu *sync.RWMutex) bool {
	mu.RLock()
	defer mu.RUnlock()
	for _, dep := range task.DependsOn {
		if failed[dep] {
			return true
		}
	}
	return false
}

func (o *Orchestrator) emitProgress(event ProgressEvent) {
	if o.onProgress != nil {
		o.onProgress(event)
	}
}

// topoSortLayers 对任务进行拓扑排序，返回分层的任务列表。
// 同一层的任务之间无依赖关系，可以并行执行。
func topoSortLayers(tasks []*AgentTask) ([][]*AgentTask, error) {
	taskMap := make(map[string]*AgentTask, len(tasks))
	inDegree := make(map[string]int, len(tasks))
	dependents := make(map[string][]string) // ID → 依赖此 ID 的任务列表

	for _, t := range tasks {
		taskMap[t.ID] = t
		inDegree[t.ID] = len(t.DependsOn)
		for _, dep := range t.DependsOn {
			dependents[dep] = append(dependents[dep], t.ID)
		}
	}

	// 找到所有入度为 0 的任务作为第一层
	var layers [][]*AgentTask
	var currentLayer []*AgentTask
	for _, t := range tasks {
		if inDegree[t.ID] == 0 {
			currentLayer = append(currentLayer, t)
		}
	}

	processed := 0
	for len(currentLayer) > 0 {
		layers = append(layers, currentLayer)
		processed += len(currentLayer)

		var nextLayer []*AgentTask
		for _, t := range currentLayer {
			for _, depID := range dependents[t.ID] {
				inDegree[depID]--
				if inDegree[depID] == 0 {
					nextLayer = append(nextLayer, taskMap[depID])
				}
			}
		}
		currentLayer = nextLayer
	}

	if processed != len(tasks) {
		return nil, fmt.Errorf("任务 DAG 存在循环依赖，已处理 %d/%d 个任务", processed, len(tasks))
	}

	return layers, nil
}
