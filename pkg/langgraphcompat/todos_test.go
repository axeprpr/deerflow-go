package langgraphcompat

import (
	"context"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/models"
	toolctx "github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestInjectTodoReminderAppendsReminderWhenWriteTodosLeftContext(t *testing.T) {
	threadID := "thread-todo-reminder"
	messages := []models.Message{{
		ID:        "user-1",
		SessionID: threadID,
		Role:      models.RoleHuman,
		Content:   "继续做下去",
	}}

	got := injectTodoReminder(threadID, messages, []Todo{
		{Content: "Inspect repo", Status: "completed"},
		{Content: "Implement feature", Status: "in_progress"},
	})

	if len(got) != 2 {
		t.Fatalf("len(messages)=%d want 2", len(got))
	}
	reminder := got[1]
	if !isTransientTodoReminderMessage(reminder) {
		t.Fatalf("reminder metadata=%#v want transient todo reminder", reminder.Metadata)
	}
	if !strings.Contains(reminder.Content, "<system_reminder>") {
		t.Fatalf("content=%q missing system reminder wrapper", reminder.Content)
	}
	if !strings.Contains(reminder.Content, "- [completed] Inspect repo") {
		t.Fatalf("content=%q missing completed todo", reminder.Content)
	}
	if !strings.Contains(reminder.Content, "- [in_progress] Implement feature") {
		t.Fatalf("content=%q missing in_progress todo", reminder.Content)
	}
}

func TestInjectTodoReminderSkipsWhenWriteTodosStillVisible(t *testing.T) {
	threadID := "thread-write-visible"
	messages := []models.Message{{
		ID:        "ai-1",
		SessionID: threadID,
		Role:      models.RoleAI,
		Content:   "tracking progress",
		ToolCalls: []models.ToolCall{{
			ID:     "call-1",
			Name:   "write_todos",
			Status: models.CallStatusCompleted,
		}},
	}}

	got := injectTodoReminder(threadID, messages, []Todo{{Content: "Implement feature", Status: "in_progress"}})
	if len(got) != 1 {
		t.Fatalf("len(messages)=%d want 1", len(got))
	}
}

func TestInjectTodoReminderSkipsWhenReminderAlreadyPresent(t *testing.T) {
	threadID := "thread-reminder-visible"
	reminder := todoReminderMessage(threadID, []Todo{{Content: "Implement feature", Status: "in_progress"}})
	messages := []models.Message{
		{
			ID:        "user-1",
			SessionID: threadID,
			Role:      models.RoleHuman,
			Content:   "继续",
		},
		reminder,
	}

	got := injectTodoReminder(threadID, messages, []Todo{{Content: "Implement feature", Status: "in_progress"}})
	if len(got) != 2 {
		t.Fatalf("len(messages)=%d want 2", len(got))
	}
}

func TestFilterTransientMessagesRemovesTodoReminder(t *testing.T) {
	threadID := "thread-filter-reminder"
	msgs := []models.Message{
		{
			ID:        "user-1",
			SessionID: threadID,
			Role:      models.RoleHuman,
			Content:   "hello",
		},
		todoReminderMessage(threadID, []Todo{{Content: "Implement feature", Status: "in_progress"}}),
	}

	filtered := filterTransientMessages(msgs)
	if len(filtered) != 1 {
		t.Fatalf("len(filtered)=%d want 1", len(filtered))
	}
	if filtered[0].Content != "hello" {
		t.Fatalf("filtered[0]=%q want hello", filtered[0].Content)
	}
}

func TestTodoToolPersistsStructuredTaskState(t *testing.T) {
	s, _ := newCompatTestServer(t)
	threadID := "thread-task-state"
	result, err := s.todoTool().Handler(toolctx.WithThreadID(context.Background(), threadID), models.ToolCall{
		ID:   "call-1",
		Name: "write_todos",
		Arguments: map[string]any{
			"todos": []any{
				map[string]any{"content": "draft report", "status": "completed"},
				map[string]any{"content": "present report", "status": "in_progress"},
			},
			"expected_outputs": []any{"/mnt/user-data/outputs/report.md"},
		},
	})
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	session := s.ensureSession(threadID, nil)
	taskState, ok := session.Metadata[harnessruntime.DefaultTaskStateMetadataKey].(map[string]any)
	if !ok {
		t.Fatalf("task_state metadata=%#v", session.Metadata[harnessruntime.DefaultTaskStateMetadataKey])
	}
	expected, _ := taskState["expected_outputs"].([]string)
	if len(expected) != 1 || expected[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("expected_outputs=%#v", taskState["expected_outputs"])
	}
	resultState, ok := result.Data["task_state"].(map[string]any)
	if !ok {
		t.Fatalf("result task_state=%#v", result.Data["task_state"])
	}
	if got, _ := resultState["expected_outputs"].([]string); len(got) != 1 || got[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("result expected_outputs=%#v", resultState["expected_outputs"])
	}
	storeState, ok := s.ensureThreadStateStore().LoadThreadRuntimeState(threadID)
	if !ok {
		t.Fatal("LoadThreadRuntimeState() ok = false, want true")
	}
	persisted, ok := storeState.Metadata[harnessruntime.DefaultTaskStateMetadataKey].(map[string]any)
	if !ok {
		t.Fatalf("thread store task_state=%#v", storeState.Metadata[harnessruntime.DefaultTaskStateMetadataKey])
	}
	if got, _ := persisted["expected_outputs"].([]string); len(got) != 1 || got[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("thread store expected_outputs=%#v", persisted["expected_outputs"])
	}
}
