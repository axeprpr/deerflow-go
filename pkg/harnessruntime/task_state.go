package harnessruntime

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

const DefaultTaskStateMetadataKey = "task_state"

type TaskStateRuntime interface {
	LoadTaskState(threadID string) harness.TaskState
	DeriveTaskState(threadID string, result *agent.RunResult) harness.TaskState
}

type TaskStateProvider struct {
	Load   func(threadID string) harness.TaskState
	Derive func(threadID string, result *agent.RunResult) harness.TaskState
}

func NewTaskStateProvider(runtime TaskStateRuntime) TaskStateProvider {
	if runtime == nil {
		return TaskStateProvider{}
	}
	return TaskStateProvider{
		Load:   runtime.LoadTaskState,
		Derive: runtime.DeriveTaskState,
	}
}

func (p TaskStateProvider) LoadTaskState(state *harness.RunState) harness.TaskState {
	if state == nil {
		return harness.TaskState{}
	}
	if !state.TaskState.IsZero() {
		return state.TaskState
	}
	if p.Load != nil {
		if resolved, err := harness.NormalizeTaskState(p.Load(state.ThreadID)); err == nil {
			if !resolved.IsZero() {
				return resolved
			}
		}
	}
	if resolved, ok := resolveTaskStateFromRunState(state, nil, DefaultTaskStateMetadataKey); ok {
		return resolved
	}
	return harness.TaskState{}
}

func (p TaskStateProvider) DeriveTaskState(state *harness.RunState, result *agent.RunResult) harness.TaskState {
	if state == nil {
		return harness.TaskState{}
	}
	if p.Derive != nil {
		if resolved, err := harness.NormalizeTaskState(p.Derive(state.ThreadID, result)); err == nil {
			if !resolved.IsZero() {
				return resolved
			}
		}
	}
	if resolved, ok := resolveTaskStateFromRunState(state, result, DefaultTaskStateMetadataKey); ok {
		return resolved
	}
	return state.TaskState
}

func resolveTaskStateFromRunState(state *harness.RunState, result *agent.RunResult, metadataKey string) (harness.TaskState, bool) {
	if state != nil && !state.TaskState.IsZero() {
		return state.TaskState, true
	}
	if resolved, ok := deriveTaskStateFromResult(result); ok {
		return resolved, true
	}
	if resolved, ok := parseTaskStateMetadata(state, metadataKey); ok {
		return resolved, true
	}
	return harness.TaskState{}, false
}

func parseTaskStateMetadata(state *harness.RunState, metadataKey string) (harness.TaskState, bool) {
	if state == nil {
		return harness.TaskState{}, false
	}
	key := strings.TrimSpace(metadataKey)
	if key == "" {
		key = DefaultTaskStateMetadataKey
	}
	if resolved, ok := harness.ParseTaskState(state.Metadata[key]); ok {
		return resolved, true
	}
	return parseLegacyTodoTaskState(state.Metadata["todos"])
}

func deriveTaskStateFromResult(result *agent.RunResult) (harness.TaskState, bool) {
	if result == nil || len(result.Messages) == 0 {
		return harness.TaskState{}, false
	}
	for i := len(result.Messages) - 1; i >= 0; i-- {
		msg := result.Messages[i]
		if msg.ToolResult == nil || msg.ToolResult.Status != "completed" || strings.TrimSpace(msg.ToolResult.ToolName) != "write_todos" {
			continue
		}
		if resolved, ok := parseLegacyTodoTaskState(msg.ToolResult.Data["todos"]); ok {
			return resolved, true
		}
	}
	return harness.TaskState{}, false
}

func parseLegacyTodoTaskState(raw any) (harness.TaskState, bool) {
	items, ok := parseLegacyTodoItems(raw)
	if !ok {
		return harness.TaskState{}, false
	}
	state, err := harness.NormalizeTaskState(harness.TaskState{Items: items})
	return state, err == nil
}

func parseLegacyTodoItems(raw any) ([]harness.TaskItem, bool) {
	switch value := raw.(type) {
	case []map[string]any:
		items := make([]harness.TaskItem, 0, len(value))
		for _, item := range value {
			items = append(items, harness.TaskItem{
				Text:   strings.TrimSpace(anyString(item["content"])),
				Status: strings.TrimSpace(anyString(item["status"])),
			})
		}
		return items, len(items) > 0
	case []any:
		items := make([]harness.TaskItem, 0, len(value))
		for _, item := range value {
			obj, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			items = append(items, harness.TaskItem{
				Text:   strings.TrimSpace(anyString(obj["content"])),
				Status: strings.TrimSpace(anyString(obj["status"])),
			})
		}
		return items, len(items) > 0
	default:
		return nil, false
	}
}

func anyString(raw any) string {
	text, _ := raw.(string)
	return text
}
