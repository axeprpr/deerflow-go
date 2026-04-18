package langgraphcompat

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestPinnedFactsFromAnyNormalizesMap(t *testing.T) {
	got := pinnedFactsFromAny(map[string]any{
		" tender_id ": " TB-2026-041 ",
		"deadline":    "2026-05-30 17:00 CST",
		"empty":       " ",
	})
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got["tender_id"] != "TB-2026-041" {
		t.Fatalf("tender_id=%q want TB-2026-041", got["tender_id"])
	}
	if got["deadline"] != "2026-05-30 17:00 CST" {
		t.Fatalf("deadline=%q want 2026-05-30 17:00 CST", got["deadline"])
	}
}

func TestSaveSessionStoresPinnedFacts(t *testing.T) {
	dataRoot := t.TempDir()
	server := &Server{
		dataRoot:    dataRoot,
		sessions:    map[string]*Session{},
		runs:        map[string]*Run{},
		runRegistry: newRunRegistry(),
	}
	server.saveSession("thread-1", []models.Message{{
		ID:        "m1",
		SessionID: "thread-1",
		Role:      models.RoleHuman,
		Content:   "hello",
	}}, map[string]string{
		"tender_id": "TB-2026-041",
	})
	session := server.ensureSession("thread-1", nil)
	facts := pinnedFactsFromValues(session.Values)
	if facts["tender_id"] != "TB-2026-041" {
		t.Fatalf("facts=%#v want tender_id", facts)
	}
}
