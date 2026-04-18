package agent

import (
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestRunFactStoreStateExtractsJSONFactsAndBuildsPrompt(t *testing.T) {
	state := newRunFactStoreState(runFactStorePolicy{
		Enabled:        true,
		MaxEntries:     8,
		MaxValueChars:  128,
		MaxPromptChars: 2048,
	}, nil)
	state.observeAssistantMessage(models.Message{
		Role: models.RoleAI,
		Content: "```json\n" +
			"{\n" +
			`  "tender_id": "TB-2026-041",` + "\n" +
			`  "deadline": "2026-05-30 17:00 CST"` + "\n" +
			"}\n```",
	})
	prompt := state.prompt()
	if !strings.Contains(prompt, "<run_fact_store>") {
		t.Fatalf("prompt=%q want run_fact_store section", prompt)
	}
	if !strings.Contains(prompt, "tender_id: TB-2026-041") {
		t.Fatalf("prompt=%q want tender_id fact", prompt)
	}
	if !strings.Contains(prompt, "deadline: 2026-05-30 17:00 CST") {
		t.Fatalf("prompt=%q want deadline fact", prompt)
	}
}

func TestExtractFactCandidatesFromMarkedLines(t *testing.T) {
	facts := extractFactCandidates(
		"noise\nCRITICAL_FACT:tender_id=TB-2026-041\nCRITICAL_FACT:payment_terms=30/60/10\n",
		128,
	)
	if got := facts["tender_id"]; got != "TB-2026-041" {
		t.Fatalf("tender_id=%q want TB-2026-041", got)
	}
	if got := facts["payment_terms"]; got != "30/60/10" {
		t.Fatalf("payment_terms=%q want 30/60/10", got)
	}
}

func TestRunFactStoreStateRespectsMaxEntries(t *testing.T) {
	state := newRunFactStoreState(runFactStorePolicy{
		Enabled:        true,
		MaxEntries:     1,
		MaxValueChars:  128,
		MaxPromptChars: 1024,
	}, nil)
	state.observeAssistantMessage(models.Message{
		Role:    models.RoleAI,
		Content: `{"a":"1","b":"2"}`,
	})
	if len(state.facts) != 1 {
		t.Fatalf("fact count=%d want 1", len(state.facts))
	}
}

func TestRunFactStoreStateSeedsPinnedFacts(t *testing.T) {
	state := newRunFactStoreState(runFactStorePolicy{
		Enabled:        true,
		MaxEntries:     8,
		MaxValueChars:  128,
		MaxPromptChars: 2048,
	}, map[string]string{
		"tender_id": "TB-2026-041",
	})
	state.observeAssistantMessage(models.Message{
		Role:    models.RoleAI,
		Content: `{"deadline":"2026-05-30 17:00 CST"}`,
	})

	pinned := state.snapshot()
	if pinned["tender_id"] != "TB-2026-041" {
		t.Fatalf("tender_id=%q want TB-2026-041", pinned["tender_id"])
	}
	if pinned["deadline"] != "2026-05-30 17:00 CST" {
		t.Fatalf("deadline=%q want 2026-05-30 17:00 CST", pinned["deadline"])
	}
}
