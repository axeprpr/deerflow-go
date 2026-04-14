package langgraphcompat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/models"
	toolctx "github.com/axeprpr/deerflow-go/pkg/tools"
)

const transientTodoReminderMetadataKey = "transient_todo_reminder"

var allowedTodoStatuses = map[string]struct{}{
	"pending":     {},
	"in_progress": {},
	"completed":   {},
}

func (s *Server) todoTool() models.Tool {
	return models.Tool{
		Name:        "write_todos",
		Description: "Create or update the current todo list so the UI can show progress on multi-step work.",
		Groups:      []string{"builtin", "planning"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"todos": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"content": map[string]any{"type": "string", "description": "Short task description"},
							"status":  map[string]any{"type": "string", "enum": []any{"pending", "in_progress", "completed"}},
						},
						"required": []any{"content", "status"},
					},
				},
				"expected_outputs": map[string]any{
					"type":        "array",
					"description": "Optional absolute output paths that must be delivered before the task is complete. Only declare final deliverables under /mnt/user-data/outputs.",
					"items":       map[string]any{"type": "string"},
				},
			},
			"required": []any{"todos"},
		},
		Handler: func(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
			threadID := toolctx.ThreadIDFromContext(ctx)
			if threadID == "" {
				err := fmt.Errorf("thread context is required")
				return models.ToolResult{
					CallID:   call.ID,
					ToolName: call.Name,
					Status:   models.CallStatusFailed,
					Error:    err.Error(),
				}, err
			}

			todos, err := decodeTodos(call.Arguments["todos"])
			if err != nil {
				return models.ToolResult{
					CallID:   call.ID,
					ToolName: call.Name,
					Status:   models.CallStatusFailed,
					Error:    err.Error(),
				}, err
			}
			taskState, err := taskStateFromTodoArguments(call.Arguments, todos)
			if err != nil {
				return models.ToolResult{
					CallID:   call.ID,
					ToolName: call.Name,
					Status:   models.CallStatusFailed,
					Error:    err.Error(),
				}, err
			}

			s.setThreadTaskState(threadID, taskState)
			data := map[string]any{
				"thread_id":  threadID,
				"todos":      todosToAny(todos),
				"task_state": taskState.Value(),
			}
			content := fmt.Sprintf("Updated todo list with %d item(s)", len(todos))
			if len(todos) == 0 {
				content = "Cleared todo list"
			}
			return models.ToolResult{
				CallID:      call.ID,
				ToolName:    call.Name,
				Status:      models.CallStatusCompleted,
				Content:     content,
				Data:        data,
				CompletedAt: time.Now().UTC(),
			}, nil
		},
	}
}

func taskStateFromTodoArguments(args map[string]any, todos []Todo) (harness.TaskState, error) {
	items := make([]harness.TaskItem, 0, len(todos))
	for _, todo := range todos {
		items = append(items, harness.TaskItem{
			Text:   strings.TrimSpace(todo.Content),
			Status: strings.TrimSpace(todo.Status),
		})
	}
	expectedOutputs, err := harness.NormalizeTaskState(harness.TaskState{
		ExpectedOutputs: anyStringSlice(args["expected_outputs"]),
	})
	if err != nil {
		return harness.TaskState{}, err
	}
	return harness.NormalizeTaskState(harness.TaskState{
		Items:           items,
		ExpectedOutputs: expectedOutputs.ExpectedOutputs,
	})
}

func decodeTodos(raw any) ([]Todo, error) {
	switch items := raw.(type) {
	case []any:
		todos := make([]Todo, 0, len(items))
		for idx, item := range items {
			obj, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("todos[%d] must be an object", idx)
			}
			todo, err := decodeTodoObject(obj)
			if err != nil {
				return nil, fmt.Errorf("todos[%d]: %w", idx, err)
			}
			todos = append(todos, todo)
		}
		return todos, nil
	case []map[string]any:
		todos := make([]Todo, 0, len(items))
		for idx, item := range items {
			todo, err := decodeTodoObject(item)
			if err != nil {
				return nil, fmt.Errorf("todos[%d]: %w", idx, err)
			}
			todos = append(todos, todo)
		}
		return todos, nil
	case nil:
		return nil, fmt.Errorf("todos is required")
	default:
		var arr []map[string]any
		buf, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("todos must be an array")
		}
		if err := json.Unmarshal(buf, &arr); err != nil {
			return nil, fmt.Errorf("todos must be an array")
		}
		todos := make([]Todo, 0, len(arr))
		for idx, item := range arr {
			todo, err := decodeTodoObject(item)
			if err != nil {
				return nil, fmt.Errorf("todos[%d]: %w", idx, err)
			}
			todos = append(todos, todo)
		}
		return todos, nil
	}
}

func decodeTodoObject(obj map[string]any) (Todo, error) {
	content := strings.Join(strings.Fields(stringFromAny(obj["content"])), " ")
	if content == "" {
		return Todo{}, fmt.Errorf("content is required")
	}
	status := strings.TrimSpace(stringFromAny(obj["status"]))
	if _, ok := allowedTodoStatuses[status]; !ok {
		return Todo{}, fmt.Errorf("invalid status %q", status)
	}
	return Todo{Content: content, Status: status}, nil
}

func (s *Server) setThreadTaskState(threadID string, taskState harness.TaskState) {
	todos := make([]Todo, 0, len(taskState.Items))
	for _, item := range taskState.Items {
		todos = append(todos, Todo{
			Content: item.Text,
			Status:  item.Status,
		})
	}
	s.sessionsMu.Lock()
	var snapshot *Session
	session, exists := s.sessions[threadID]
	if !exists {
		session = &Session{
			ThreadID:     threadID,
			Messages:     []models.Message{},
			Todos:        nil,
			Metadata:     map[string]any{},
			Status:       "idle",
			PresentFiles: nil,
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		s.sessions[threadID] = session
	}
	session.Todos = append([]Todo(nil), todos...)
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	if taskState.IsZero() {
		delete(session.Metadata, harnessruntime.DefaultTaskStateMetadataKey)
	} else {
		session.Metadata[harnessruntime.DefaultTaskStateMetadataKey] = taskState.Value()
	}
	session.UpdatedAt = time.Now().UTC()
	snapshot = cloneSession(session)
	s.sessionsMu.Unlock()
	if store := s.ensureThreadStateStore(); store != nil {
		if taskState.IsZero() {
			store.ClearThreadMetadata(threadID, harnessruntime.DefaultTaskStateMetadataKey)
		} else {
			store.SetThreadMetadata(threadID, harnessruntime.DefaultTaskStateMetadataKey, taskState.Value())
		}
	}
	_ = s.persistSessionSnapshot(snapshot)
}

func todosToAny(todos []Todo) []map[string]any {
	if len(todos) == 0 {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(todos))
	for _, todo := range todos {
		out = append(out, map[string]any{
			"content": todo.Content,
			"status":  todo.Status,
		})
	}
	return out
}

func injectTodoReminder(threadID string, messages []models.Message, todos []Todo) []models.Message {
	if len(todos) == 0 || todoWriteVisible(messages) || todoReminderVisible(messages) {
		return messages
	}
	out := append([]models.Message(nil), messages...)
	out = append(out, todoReminderMessage(threadID, todos))
	return out
}

func todoWriteVisible(messages []models.Message) bool {
	for _, msg := range messages {
		if msg.Role != models.RoleAI || len(msg.ToolCalls) == 0 {
			continue
		}
		for _, call := range msg.ToolCalls {
			if call.Name == "write_todos" {
				return true
			}
		}
	}
	return false
}

func todoReminderVisible(messages []models.Message) bool {
	for _, msg := range messages {
		if isTransientTodoReminderMessage(msg) {
			return true
		}
	}
	return false
}

func todoReminderMessage(threadID string, todos []Todo) models.Message {
	return models.Message{
		ID:        fmt.Sprintf("todo-reminder-%d", time.Now().UTC().UnixNano()),
		SessionID: threadID,
		Role:      models.RoleHuman,
		Content: "<system_reminder>\n" +
			"Your todo list from earlier is no longer visible in the current context window, but it is still active. Here is the current state:\n\n" +
			formatTodosForReminder(todos) +
			"\n\nContinue tracking and updating this todo list as you work. Call `write_todos` whenever the status of any item changes.\n" +
			"</system_reminder>",
		Metadata: map[string]string{transientTodoReminderMetadataKey: "true"},
	}
}

func formatTodosForReminder(todos []Todo) string {
	if len(todos) == 0 {
		return ""
	}
	lines := make([]string, 0, len(todos))
	for _, todo := range todos {
		status := strings.TrimSpace(todo.Status)
		if status == "" {
			status = "pending"
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", status, strings.TrimSpace(todo.Content)))
	}
	return strings.Join(lines, "\n")
}
