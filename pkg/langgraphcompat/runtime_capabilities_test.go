package langgraphcompat

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestRuntimeCapabilitiesExposeSummaryAccessors(t *testing.T) {
	server := &Server{
		sessions: map[string]*Session{},
		runs:     map[string]*Run{},
	}
	server.ensureSession("thread-1", nil)

	server.PersistHistorySummary("thread-1", "summary-1")

	if got := server.HistorySummary("thread-1"); got != "summary-1" {
		t.Fatalf("HistorySummary() = %q, want %q", got, "summary-1")
	}
}

func TestRuntimeCapabilitiesResolveMemorySessionID(t *testing.T) {
	server := &Server{}
	if got := server.ResolveMemorySessionID("thread-1", "planner"); got != "agent:planner" {
		t.Fatalf("ResolveMemorySessionID() = %q, want %q", got, "agent:planner")
	}
}

func TestRuntimeCapabilitiesCompactConversationUsesExistingImplementation(t *testing.T) {
	server := &Server{
		sessions: map[string]*Session{},
		runs:     map[string]*Run{},
	}
	server.ensureSession("thread-1", nil)

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

	compacted := server.CompactConversation(context.Background(), "thread-1", "model-1", "", messages)
	if !compacted.Changed {
		t.Fatalf("CompactConversation() did not mark result as changed")
	}
	if len(compacted.Messages) != defaultSummaryKeepMessages {
		t.Fatalf("kept messages = %d, want %d", len(compacted.Messages), defaultSummaryKeepMessages)
	}
}
