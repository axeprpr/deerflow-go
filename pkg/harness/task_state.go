package harness

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	TaskStatusPending    = "pending"
	TaskStatusInProgress = "in_progress"
	TaskStatusCompleted  = "completed"
)

type TaskItem struct {
	Text   string `json:"text,omitempty"`
	Status string `json:"status,omitempty"`
}

type TaskState struct {
	Items           []TaskItem `json:"items,omitempty"`
	ExpectedOutputs []string   `json:"expected_outputs,omitempty"`
	VerifiedOutputs []string   `json:"verified_outputs,omitempty"`
}

func NormalizeTaskStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case TaskStatusPending:
		return TaskStatusPending
	case TaskStatusInProgress:
		return TaskStatusInProgress
	case TaskStatusCompleted:
		return TaskStatusCompleted
	default:
		return ""
	}
}

func NormalizeTaskState(state TaskState) (TaskState, error) {
	normalized := TaskState{
		Items:           make([]TaskItem, 0, len(state.Items)),
		ExpectedOutputs: normalizeTaskStrings(state.ExpectedOutputs),
		VerifiedOutputs: normalizeTaskStrings(state.VerifiedOutputs),
	}
	inProgress := 0
	for _, item := range state.Items {
		text := strings.Join(strings.Fields(strings.TrimSpace(item.Text)), " ")
		status := NormalizeTaskStatus(item.Status)
		if text == "" {
			return TaskState{}, fmt.Errorf("task item text must not be empty")
		}
		if status == "" {
			return TaskState{}, fmt.Errorf("invalid task status %q", item.Status)
		}
		if status == TaskStatusInProgress {
			inProgress++
		}
		normalized.Items = append(normalized.Items, TaskItem{
			Text:   text,
			Status: status,
		})
	}
	if inProgress > 1 {
		return TaskState{}, fmt.Errorf("only one task item can be in_progress")
	}
	return normalized, nil
}

func ParseTaskState(raw any) (TaskState, bool) {
	switch value := raw.(type) {
	case nil:
		return TaskState{}, false
	case TaskState:
		state, err := NormalizeTaskState(value)
		return state, err == nil
	case *TaskState:
		if value == nil {
			return TaskState{}, false
		}
		state, err := NormalizeTaskState(*value)
		return state, err == nil
	case map[string]any:
		state, err := taskStateFromMap(value)
		return state, err == nil
	default:
		var decoded TaskState
		buf, err := json.Marshal(raw)
		if err != nil {
			return TaskState{}, false
		}
		if err := json.Unmarshal(buf, &decoded); err != nil {
			return TaskState{}, false
		}
		state, err := NormalizeTaskState(decoded)
		return state, err == nil
	}
}

func (s TaskState) IsZero() bool {
	return len(s.Items) == 0 && len(s.ExpectedOutputs) == 0 && len(s.VerifiedOutputs) == 0
}

func (s TaskState) PendingTexts() []string {
	if len(s.Items) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.Items))
	for _, item := range s.Items {
		if item.Status == TaskStatusCompleted {
			continue
		}
		out = append(out, item.Text)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s TaskState) MissingExpectedOutputs() []string {
	if len(s.ExpectedOutputs) == 0 {
		return nil
	}
	verified := make(map[string]struct{}, len(s.VerifiedOutputs))
	for _, item := range s.VerifiedOutputs {
		verified[item] = struct{}{}
	}
	out := make([]string, 0, len(s.ExpectedOutputs))
	for _, item := range s.ExpectedOutputs {
		if _, ok := verified[item]; !ok {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s TaskState) Value() map[string]any {
	if s.IsZero() {
		return nil
	}
	items := make([]map[string]any, 0, len(s.Items))
	for _, item := range s.Items {
		items = append(items, map[string]any{
			"text":   item.Text,
			"status": item.Status,
		})
	}
	out := map[string]any{
		"items": items,
	}
	if len(s.ExpectedOutputs) > 0 {
		out["expected_outputs"] = append([]string(nil), s.ExpectedOutputs...)
	}
	if len(s.VerifiedOutputs) > 0 {
		out["verified_outputs"] = append([]string(nil), s.VerifiedOutputs...)
	}
	return out
}

func normalizeTaskStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		text := strings.Join(strings.Fields(strings.TrimSpace(item)), " ")
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func taskStateFromMap(raw map[string]any) (TaskState, error) {
	state := TaskState{}
	switch items := raw["items"].(type) {
	case []any:
		state.Items = make([]TaskItem, 0, len(items))
		for idx, item := range items {
			obj, ok := item.(map[string]any)
			if !ok {
				return TaskState{}, fmt.Errorf("items[%d] must be an object", idx)
			}
			state.Items = append(state.Items, TaskItem{
				Text:   stringFromAny(obj["text"]),
				Status: stringFromAny(obj["status"]),
			})
		}
	case []map[string]any:
		state.Items = make([]TaskItem, 0, len(items))
		for _, item := range items {
			state.Items = append(state.Items, TaskItem{
				Text:   stringFromAny(item["text"]),
				Status: stringFromAny(item["status"]),
			})
		}
	}
	state.ExpectedOutputs = anyStringSlice(raw["expected_outputs"])
	state.VerifiedOutputs = anyStringSlice(raw["verified_outputs"])
	return NormalizeTaskState(state)
}

func anyStringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text := stringFromAny(item); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func stringFromAny(raw any) string {
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}
