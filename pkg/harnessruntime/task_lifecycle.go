package harnessruntime

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
)

const DefaultTaskLifecycleMetadataKey = "task_lifecycle"

type TaskLifecycleDescriptor struct {
	Status            string   `json:"status,omitempty"`
	PendingTasks      []string `json:"pending_tasks,omitempty"`
	ExpectedArtifacts []string `json:"expected_artifacts,omitempty"`
	VerifiedArtifacts []string `json:"verified_artifacts,omitempty"`
}

type TaskLifecycleService struct{}

func NewTaskLifecycleService() TaskLifecycleService {
	return TaskLifecycleService{}
}

func (TaskLifecycleService) Describe(outcome RunOutcome, taskState harness.TaskState, paused bool) TaskLifecycleDescriptor {
	status := outcome.RunStatus
	switch {
	case paused:
		status = "paused"
	case status == "":
		status = "success"
	}
	return TaskLifecycleDescriptor{
		Status:            status,
		PendingTasks:      append([]string(nil), taskState.PendingTexts()...),
		ExpectedArtifacts: append([]string(nil), taskState.MissingExpectedOutputs()...),
		VerifiedArtifacts: append([]string(nil), taskState.VerifiedOutputs...),
	}
}

func (d TaskLifecycleDescriptor) IsZero() bool {
	return d.Status == "" && len(d.PendingTasks) == 0 && len(d.ExpectedArtifacts) == 0 && len(d.VerifiedArtifacts) == 0
}

func (d TaskLifecycleDescriptor) Value() map[string]any {
	if d.IsZero() {
		return nil
	}
	out := map[string]any{
		"status": d.Status,
	}
	if len(d.PendingTasks) > 0 {
		out["pending_tasks"] = append([]string(nil), d.PendingTasks...)
	}
	if len(d.ExpectedArtifacts) > 0 {
		out["expected_artifacts"] = append([]string(nil), d.ExpectedArtifacts...)
	}
	if len(d.VerifiedArtifacts) > 0 {
		out["verified_artifacts"] = append([]string(nil), d.VerifiedArtifacts...)
	}
	return out
}

func ParseTaskLifecycle(raw any) (TaskLifecycleDescriptor, bool) {
	value, ok := raw.(map[string]any)
	if !ok || len(value) == 0 {
		return TaskLifecycleDescriptor{}, false
	}
	status, _ := value["status"].(string)
	desc := TaskLifecycleDescriptor{
		Status:            strings.TrimSpace(status),
		PendingTasks:      normalizeLifecycleStrings(value["pending_tasks"]),
		ExpectedArtifacts: normalizeLifecycleStrings(value["expected_artifacts"]),
		VerifiedArtifacts: normalizeLifecycleStrings(value["verified_artifacts"]),
	}
	if desc.IsZero() {
		return TaskLifecycleDescriptor{}, false
	}
	return desc, true
}

type ThreadTaskLifecycleTracker struct {
	store    ThreadStateStore
	threadID string
}

func NewThreadTaskLifecycleTracker(store ThreadStateStore, threadID string) ThreadTaskLifecycleTracker {
	return ThreadTaskLifecycleTracker{store: store, threadID: strings.TrimSpace(threadID)}
}

func (t ThreadTaskLifecycleTracker) Observe(evt subagent.TaskEvent) {
	if t.store == nil || t.threadID == "" {
		return
	}
	taskState := t.loadTaskState()
	description := strings.Join(strings.Fields(strings.TrimSpace(evt.Description)), " ")
	switch strings.TrimSpace(evt.Type) {
	case "task_started", "task_running":
		taskState = upsertTaskStateItem(taskState, description, harness.TaskStatusInProgress)
	case "task_completed":
		taskState = upsertTaskStateItem(taskState, description, harness.TaskStatusCompleted)
	case "task_failed", "task_timed_out":
		taskState = upsertTaskStateItem(taskState, description, harness.TaskStatusPending)
	default:
		return
	}
	lifecycle := NewTaskLifecycleService().Describe(RunOutcome{RunStatus: "running"}, taskState, false)
	t.store.SetThreadMetadata(t.threadID, DefaultTaskLifecycleMetadataKey, lifecycle.Value())
	if taskState.IsZero() {
		t.store.ClearThreadMetadata(t.threadID, DefaultTaskStateMetadataKey)
	} else {
		t.store.SetThreadMetadata(t.threadID, DefaultTaskStateMetadataKey, taskState.Value())
	}
}

func (t ThreadTaskLifecycleTracker) loadTaskState() harness.TaskState {
	if t.store == nil || t.threadID == "" {
		return harness.TaskState{}
	}
	state, ok := t.store.LoadThreadRuntimeState(t.threadID)
	if !ok {
		return harness.TaskState{}
	}
	if taskState, ok := harness.ParseTaskState(state.Metadata[DefaultTaskStateMetadataKey]); ok {
		return taskState
	}
	return harness.TaskState{}
}

func normalizeLifecycleStrings(raw any) []string {
	switch value := raw.(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, _ := item.(string)
			text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
			if text != "" {
				out = append(out, text)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func upsertTaskStateItem(state harness.TaskState, text string, status string) harness.TaskState {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	status = harness.NormalizeTaskStatus(status)
	if text == "" || status == "" {
		return state
	}
	items := append([]harness.TaskItem(nil), state.Items...)
	found := false
	for i := range items {
		if items[i].Text == text {
			items[i].Status = status
			found = true
			break
		}
	}
	if !found {
		items = append(items, harness.TaskItem{Text: text, Status: status})
	}
	normalized, err := harness.NormalizeTaskState(harness.TaskState{
		Items:           items,
		ExpectedOutputs: state.ExpectedOutputs,
		VerifiedOutputs: state.VerifiedOutputs,
	})
	if err != nil {
		return state
	}
	return normalized
}
