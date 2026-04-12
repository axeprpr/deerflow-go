package langgraphcompat

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestRuntimeConversationAdapterSummaryAccessors(t *testing.T) {
	server := &Server{
		sessions: map[string]*Session{},
		runs:     map[string]*Run{},
	}
	server.ensureSession("thread-1", nil)
	adapter := server.runtimeConversationAdapter()

	adapter.PersistHistorySummary("thread-1", "summary-1")
	if got := adapter.HistorySummary("thread-1"); got != "summary-1" {
		t.Fatalf("HistorySummary() = %q, want %q", got, "summary-1")
	}
}

func TestRuntimeMemoryAdapterResolveMemorySessionID(t *testing.T) {
	adapter := (&Server{}).runtimeMemoryAdapter()
	if got := adapter.ResolveMemorySessionID("thread-1", "planner"); got != "agent:planner" {
		t.Fatalf("ResolveMemorySessionID() = %q, want %q", got, "agent:planner")
	}
	scope := adapter.ResolveMemoryScope("thread-1", "planner")
	if scope.Key() != "agent:planner" {
		t.Fatalf("ResolveMemoryScope().Key() = %q, want %q", scope.Key(), "agent:planner")
	}
}

func TestRuntimeConversationAdapterCompactConversationUsesExistingImplementation(t *testing.T) {
	server := &Server{
		sessions: map[string]*Session{},
		runs:     map[string]*Run{},
	}
	server.ensureSession("thread-1", nil)
	adapter := server.runtimeConversationAdapter()

	messages := make([]models.Message, 0, 34)
	for i := 0; i < 34; i++ {
		role := models.RoleHuman
		if i%2 == 1 {
			role = models.RoleAI
		}
		messages = append(messages, models.Message{
			ID:        "m",
			SessionID: "thread-1",
			Role:      role,
			Content:   "message content message content message content",
		})
	}

	compacted := adapter.CompactConversation(context.Background(), "thread-1", "model-1", "", messages)
	if !compacted.Changed {
		t.Fatalf("CompactConversation() did not mark result as changed")
	}
	if len(compacted.Messages) != defaultSummaryKeepMessages {
		t.Fatalf("kept messages = %d, want %d", len(compacted.Messages), defaultSummaryKeepMessages)
	}
}
