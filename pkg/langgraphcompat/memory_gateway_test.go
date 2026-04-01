package langgraphcompat

import (
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/memory"
)

func TestGatewayMemoryFromDocumentIncludesLongTermBackground(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, 4, 1, 3, 0, 0, 0, time.UTC)
	doc := memory.Document{
		SessionID: "thread-memory",
		History: memory.HistoryMemory{
			RecentMonths:       "Recent delivery work.",
			EarlierContext:     "Earlier product migration.",
			LongTermBackground: "Owns the DeerFlow Go rewrite over the long term.",
		},
		UpdatedAt: updatedAt,
	}

	got := gatewayMemoryFromDocument(doc)
	if got.History.LongTermBackground.Summary != doc.History.LongTermBackground {
		t.Fatalf("longTermBackground summary = %q", got.History.LongTermBackground.Summary)
	}
	if got.History.LongTermBackground.UpdatedAt != updatedAt.Format(time.RFC3339) {
		t.Fatalf("longTermBackground updatedAt = %q", got.History.LongTermBackground.UpdatedAt)
	}
}
