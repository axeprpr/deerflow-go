package langgraphcompat

import (
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
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
