package langgraphcompat

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

func deriveRuntimeTaskState(result *agent.RunResult) (harness.TaskState, bool) {
	if result == nil || len(result.Messages) == 0 {
		return harness.TaskState{}, false
	}
	for i := len(result.Messages) - 1; i >= 0; i-- {
		msg := result.Messages[i]
		if msg.ToolResult == nil || msg.ToolResult.Status != models.CallStatusCompleted || strings.TrimSpace(msg.ToolResult.ToolName) != "write_todos" {
			continue
		}
		if state, ok := taskStateFromTodosRaw(msg.ToolResult.Data["todos"]); ok {
			return state, true
		}
	}
	return harness.TaskState{}, false
}

func taskStateFromTodosRaw(raw any) (harness.TaskState, bool) {
	todos, err := decodeTodos(raw)
	if err != nil || len(todos) == 0 {
		return harness.TaskState{}, false
	}
	items := make([]harness.TaskItem, 0, len(todos))
	for _, todo := range todos {
		items = append(items, harness.TaskItem{
			Text:   strings.TrimSpace(todo.Content),
			Status: strings.TrimSpace(todo.Status),
		})
	}
	state, err := harness.NormalizeTaskState(harness.TaskState{Items: items})
	return state, err == nil
}
