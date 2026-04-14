package langgraphcompat

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func deriveRuntimeTaskState(result *agent.RunResult) (harness.TaskState, bool) {
	return harnessruntime.DeriveTaskStateFromResult(result)
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
