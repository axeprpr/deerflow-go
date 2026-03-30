package langgraphcompat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
	toolctx "github.com/axeprpr/deerflow-go/pkg/tools"
)

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

			s.setThreadTodos(threadID, todos)
			data := map[string]any{
				"thread_id": threadID,
				"todos":     todosToAny(todos),
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

func (s *Server) setThreadTodos(threadID string, todos []Todo) {
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
	session.UpdatedAt = time.Now().UTC()
	snapshot = cloneSession(session)
	s.sessionsMu.Unlock()
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
