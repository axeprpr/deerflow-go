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
	lifecycle := t.load()
	lifecycle.Status = "running"
	description := strings.Join(strings.Fields(strings.TrimSpace(evt.Description)), " ")
	switch strings.TrimSpace(evt.Type) {
	case "task_started", "task_running":
		lifecycle.PendingTasks = upsertLifecycleText(lifecycle.PendingTasks, description)
	case "task_completed":
		lifecycle.PendingTasks = removeLifecycleText(lifecycle.PendingTasks, description)
	case "task_failed", "task_timed_out":
		lifecycle.PendingTasks = upsertLifecycleText(lifecycle.PendingTasks, description)
	default:
		return
	}
	t.store.SetThreadMetadata(t.threadID, DefaultTaskLifecycleMetadataKey, lifecycle.Value())
}

func (t ThreadTaskLifecycleTracker) load() TaskLifecycleDescriptor {
	if t.store == nil || t.threadID == "" {
		return TaskLifecycleDescriptor{}
	}
	state, ok := t.store.LoadThreadRuntimeState(t.threadID)
	if !ok {
		return TaskLifecycleDescriptor{}
	}
	if lifecycle, ok := ParseTaskLifecycle(state.Metadata[DefaultTaskLifecycleMetadataKey]); ok {
		return lifecycle
	}
	return TaskLifecycleDescriptor{}
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

func upsertLifecycleText(items []string, text string) []string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return items
	}
	for _, item := range items {
		if item == text {
			return items
		}
	}
	return append(append([]string(nil), items...), text)
}

func removeLifecycleText(items []string, text string) []string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" || len(items) == 0 {
		return items
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item != text {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
