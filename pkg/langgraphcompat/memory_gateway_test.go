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

func TestGatewayMemoryFromDocumentPreservesFactSource(t *testing.T) {
	t.Parallel()

	doc := memory.Document{
		SessionID: "agent:writer-bot",
		Source:    "agent:writer-bot",
		Facts: []memory.Fact{{
			ID:         "fact-1",
			Content:    "User prefers concise summaries.",
			Category:   "preference",
			Confidence: 0.9,
			Source:     "thread-123",
			CreatedAt:  time.Date(2026, 4, 1, 4, 0, 0, 0, time.UTC),
			UpdatedAt:  time.Date(2026, 4, 1, 4, 0, 0, 0, time.UTC),
		}},
		UpdatedAt: time.Date(2026, 4, 1, 5, 0, 0, 0, time.UTC),
	}

	got := gatewayMemoryFromDocument(doc)
	if len(got.Facts) != 1 {
		t.Fatalf("facts len = %d", len(got.Facts))
	}
	if got.Facts[0].Source != "thread-123" {
		t.Fatalf("fact source = %q", got.Facts[0].Source)
	}
}
