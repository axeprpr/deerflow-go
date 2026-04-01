package langgraphcompat

import (
	"context"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type titleProvider struct {
	response string
	err      error
	lastReq  llm.ChatRequest
}

func (p *titleProvider) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	p.lastReq = req
	if p.err != nil {
		return llm.ChatResponse{}, p.err
	}
	return llm.ChatResponse{
		Model: req.Model,
		Message: models.Message{
			ID:        "title-response",
			SessionID: "title",
			Role:      models.RoleAI,
			Content:   p.response,
		},
	}, nil
}

func (p *titleProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func TestMaybeGenerateThreadTitleUsesLLMResult(t *testing.T) {
	provider := &titleProvider{response: "  \"Plan trip to Japan\"  "}
	server := &Server{
		llmProvider:  provider,
		defaultModel: "default-model",
		sessions:     make(map[string]*Session),
		runs:         make(map[string]*Run),
	}
	server.ensureSession("thread-1", nil)

	messages := []models.Message{
		{ID: "u1", SessionID: "thread-1", Role: models.RoleHuman, Content: "Help me plan a 7-day trip to Japan in April."},
		{ID: "a1", SessionID: "thread-1", Role: models.RoleAI, Content: "I can help build an itinerary, budget, and route."},
	}

	server.maybeGenerateThreadTitle(context.Background(), "thread-1", "run-model", messages)

	state := server.getThreadState("thread-1")
	if got := state.Values["title"]; got != "Plan trip to Japan" {
		t.Fatalf("title = %v, want %q", got, "Plan trip to Japan")
	}
	if provider.lastReq.Model != "run-model" {
		t.Fatalf("title model = %q, want %q", provider.lastReq.Model, "run-model")
	}
	if len(provider.lastReq.Messages) != 1 {
		t.Fatalf("title prompt messages = %d, want 1", len(provider.lastReq.Messages))
	}
	if !strings.Contains(provider.lastReq.Messages[0].Content, "Help me plan") {
		t.Fatalf("title prompt missing user content: %q", provider.lastReq.Messages[0].Content)
	}
}

func TestMaybeGenerateThreadTitleFallsBackWhenLLMFails(t *testing.T) {
	provider := &titleProvider{err: context.DeadlineExceeded}
	server := &Server{
		llmProvider: provider,
		sessions:    make(map[string]*Session),
		runs:        make(map[string]*Run),
	}
	server.ensureSession("thread-2", nil)

	messages := []models.Message{
		{ID: "u1", SessionID: "thread-2", Role: models.RoleHuman, Content: "Summarize the incident response checklist for on-call engineers."},
		{ID: "a1", SessionID: "thread-2", Role: models.RoleAI, Content: "Here is a checklist covering triage, mitigation, and follow-up."},
	}

	server.maybeGenerateThreadTitle(context.Background(), "thread-2", "", messages)

	state := server.getThreadState("thread-2")
	if got := state.Values["title"]; got != "Summarize the incident response checklist for..." {
		t.Fatalf("fallback title = %v, want %q", got, "Summarize the incident response checklist for...")
	}
}

func TestMaybeGenerateThreadTitleDoesNotOverrideExistingTitle(t *testing.T) {
	provider := &titleProvider{response: "New title"}
	server := &Server{
		llmProvider: provider,
		sessions:    make(map[string]*Session),
		runs:        make(map[string]*Run),
	}
	server.ensureSession("thread-3", map[string]any{"title": "Existing title"})

	messages := []models.Message{
		{ID: "u1", SessionID: "thread-3", Role: models.RoleHuman, Content: "Explain Redis persistence modes."},
		{ID: "a1", SessionID: "thread-3", Role: models.RoleAI, Content: "RDB and AOF solve different durability tradeoffs."},
	}

	server.maybeGenerateThreadTitle(context.Background(), "thread-3", "", messages)

	state := server.getThreadState("thread-3")
	if got := state.Values["title"]; got != "Existing title" {
		t.Fatalf("title = %v, want %q", got, "Existing title")
	}
	if provider.lastReq.Model != "" {
		t.Fatalf("provider should not be called when title exists")
	}
}

func TestGenerateThreadTitleTruncatesLongLLMTitleWithEllipsis(t *testing.T) {
	provider := &titleProvider{response: "This is a very long generated conversation title that should be truncated cleanly"}
	server := &Server{
		llmProvider: provider,
	}

	got := server.generateThreadTitle(context.Background(), "run-model", []models.Message{
		{ID: "u1", SessionID: "thread-4", Role: models.RoleHuman, Content: "Please summarize the deployment plan."},
		{ID: "a1", SessionID: "thread-4", Role: models.RoleAI, Content: "Here is the rollout summary."},
	})

	if got != "This is a very long generated conversation title that sho..." {
		t.Fatalf("title = %q, want %q", got, "This is a very long generated conversation title that sho...")
	}
}

func TestFallbackTitleUsesEllipsisForLongSingleWordInput(t *testing.T) {
	input := strings.Repeat("迁", 55)
	got := fallbackTitle(input)
	if got != strings.Repeat("迁", 47)+"..." {
		t.Fatalf("title = %q, want %q", got, strings.Repeat("迁", 47)+"...")
	}
}
