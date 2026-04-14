package harnessruntime

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
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
	var base harness.TaskState
	if state != nil && !state.TaskState.IsZero() {
		base = state.TaskState
	} else if resolved, ok := parseTaskStateMetadata(state, metadataKey); ok {
		base = resolved
	}
	if derived, ok := DeriveTaskStateFromResult(result); ok {
		if base.IsZero() {
			return derived, true
		}
		if merged, err := harness.MergeTaskStates(base, derived); err == nil {
			return merged, true
		}
		return derived, true
	}
	if !base.IsZero() {
		return base, true
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

func DeriveTaskStateFromResult(result *agent.RunResult) (harness.TaskState, bool) {
	if result == nil || len(result.Messages) == 0 {
		return harness.TaskState{}, false
	}
	state := taskStateFromLatestTodoResult(result.Messages)
	if taskState, ok := taskStateFromLatestSubagentResult(result.Messages); ok {
		if merged, err := mergeTaskProgressState(state, taskState); err == nil {
			state = merged
		}
	}
	verified := deriveVerifiedOutputsFromMessages(result.Messages)
	if len(verified) > 0 {
		if merged, err := harness.MergeTaskStates(state, harness.TaskState{VerifiedOutputs: verified}); err == nil {
			state = merged
		}
	}
	if state.IsZero() {
		return harness.TaskState{}, false
	}
	return state, true
}

func deriveTaskStateFromResult(result *agent.RunResult) (harness.TaskState, bool) {
	return DeriveTaskStateFromResult(result)
}

func taskStateFromLatestTodoResult(messages []models.Message) harness.TaskState {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.ToolResult == nil || msg.ToolResult.Status != models.CallStatusCompleted {
			continue
		}
		if state, ok := parseTaskStatePayload(msg.ToolResult.Data); ok {
			return state
		}
		if strings.TrimSpace(msg.ToolResult.ToolName) != "write_todos" {
			continue
		}
		if resolved, ok := parseLegacyTodoTaskState(msg.ToolResult.Data["todos"]); ok {
			return resolved
		}
	}
	return harness.TaskState{}
}

func taskStateFromLatestSubagentResult(messages []models.Message) (harness.TaskState, bool) {
	if len(messages) == 0 {
		return harness.TaskState{}, false
	}
	calls := make(map[string]models.ToolCall, len(messages))
	for _, msg := range messages {
		for _, call := range msg.ToolCalls {
			if strings.TrimSpace(call.ID) == "" {
				continue
			}
			calls[strings.TrimSpace(call.ID)] = call
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.ToolResult == nil {
			continue
		}
		if strings.TrimSpace(msg.ToolResult.ToolName) != "task" {
			continue
		}
		state, ok := taskStateFromSubagentToolResult(msg.ToolResult, calls[strings.TrimSpace(msg.ToolResult.CallID)])
		if ok {
			return state, true
		}
	}
	return harness.TaskState{}, false
}

func taskStateFromSubagentToolResult(result *models.ToolResult, call models.ToolCall) (harness.TaskState, bool) {
	if result == nil {
		return harness.TaskState{}, false
	}
	description := strings.TrimSpace(anyString(result.Data["description"]))
	if description == "" {
		description = strings.TrimSpace(anyString(call.Arguments["description"]))
	}
	if description == "" {
		return harness.TaskState{}, false
	}
	status := harness.TaskStatusCompleted
	switch strings.TrimSpace(anyString(result.Data["status"])) {
	case "completed", "task_completed":
		status = harness.TaskStatusCompleted
	default:
		if result.Status == models.CallStatusFailed {
			status = harness.TaskStatusPending
		}
	}
	state, err := harness.NormalizeTaskState(harness.TaskState{
		Items: []harness.TaskItem{{
			Text:   description,
			Status: status,
		}},
	})
	if err != nil {
		return harness.TaskState{}, false
	}
	return state, true
}

func mergeTaskProgressState(base harness.TaskState, update harness.TaskState) (harness.TaskState, error) {
	if base.IsZero() {
		return harness.NormalizeTaskState(update)
	}
	if update.IsZero() {
		return harness.NormalizeTaskState(base)
	}
	merged := harness.TaskState{
		Items:           append([]harness.TaskItem(nil), base.Items...),
		ExpectedOutputs: append([]string(nil), base.ExpectedOutputs...),
		VerifiedOutputs: append([]string(nil), base.VerifiedOutputs...),
	}
	for _, item := range update.Items {
		replaced := false
		for i := range merged.Items {
			if merged.Items[i].Text == item.Text {
				merged.Items[i].Status = item.Status
				replaced = true
				break
			}
		}
		if !replaced {
			merged.Items = append(merged.Items, item)
		}
	}
	if len(update.ExpectedOutputs) > 0 {
		merged.ExpectedOutputs = append([]string(nil), update.ExpectedOutputs...)
	}
	if len(update.VerifiedOutputs) > 0 {
		merged.VerifiedOutputs = append(merged.VerifiedOutputs, update.VerifiedOutputs...)
	}
	return harness.NormalizeTaskState(merged)
}

func parseTaskStatePayload(data map[string]any) (harness.TaskState, bool) {
	if len(data) == 0 {
		return harness.TaskState{}, false
	}
	if state, ok := harness.ParseTaskState(data["task_state"]); ok && !state.IsZero() {
		return state, true
	}
	if state, ok := harness.ParseTaskState(data); ok && !state.IsZero() {
		return state, true
	}
	return harness.TaskState{}, false
}

func deriveVerifiedOutputsFromMessages(messages []models.Message) []string {
	if len(messages) == 0 {
		return nil
	}
	calls := make(map[string]models.ToolCall, len(messages))
	for _, msg := range messages {
		if len(msg.ToolCalls) == 0 {
			continue
		}
		for _, call := range msg.ToolCalls {
			if strings.TrimSpace(call.ID) == "" {
				continue
			}
			calls[call.ID] = call
		}
	}
	verified := make([]string, 0, 4)
	for _, msg := range messages {
		if msg.ToolResult == nil || msg.ToolResult.Status != models.CallStatusCompleted {
			continue
		}
		switch strings.TrimSpace(msg.ToolResult.ToolName) {
		case "present_file", "present_files":
			verified = append(verified, presentToolVerifiedOutputs(msg.ToolResult, calls[msg.ToolResult.CallID])...)
		case "write_file":
			verified = append(verified, writeFileVerifiedOutputs(msg.ToolResult, calls[msg.ToolResult.CallID])...)
		}
	}
	return uniqueTaskPaths(verified)
}

func presentToolVerifiedOutputs(result *models.ToolResult, call models.ToolCall) []string {
	out := append([]string(nil), stringSlice(result.Data["filepaths"])...)
	if path := strings.TrimSpace(anyString(result.Data["path"])); path != "" {
		out = append(out, path)
	}
	if len(out) > 0 {
		return filterOutputPaths(out)
	}
	if path := strings.TrimSpace(anyString(call.Arguments["path"])); path != "" {
		out = append(out, path)
	}
	out = append(out, stringSlice(call.Arguments["filepaths"])...)
	return filterOutputPaths(out)
}

func writeFileVerifiedOutputs(result *models.ToolResult, call models.ToolCall) []string {
	if path := strings.TrimSpace(anyString(call.Arguments["path"])); path != "" {
		return filterOutputPaths([]string{path})
	}
	return filterOutputPaths([]string{pathFromText(result.Content)})
}

func stringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text := strings.TrimSpace(anyString(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func filterOutputPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if strings.HasPrefix(path, "/mnt/user-data/outputs/") {
			out = append(out, path)
		}
	}
	return uniqueTaskPaths(out)
}

func uniqueTaskPaths(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func pathFromText(text string) string {
	for _, field := range strings.Fields(text) {
		field = strings.Trim(field, " \t\r\n.,;:()[]{}<>\"'")
		if strings.HasPrefix(field, "/mnt/user-data/outputs/") {
			return field
		}
	}
	return ""
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
