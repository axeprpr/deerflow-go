package langgraphcompat

import (
	"context"
	"os"
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

func TestLoadTitleConfigDefaults(t *testing.T) {
	t.Setenv(titleEnabledEnv, "")
	t.Setenv(titleMaxWordsEnv, "")
	t.Setenv(titleMaxCharsEnv, "")
	t.Setenv(titleModelEnv, "")

	cfg := loadTitleConfig()
	if !cfg.Enabled {
		t.Fatalf("enabled = false, want true")
	}
	if cfg.MaxWords != defaultTitleMaxWords {
		t.Fatalf("max words = %d, want %d", cfg.MaxWords, defaultTitleMaxWords)
	}
	if cfg.MaxChars != defaultTitleMaxChars {
		t.Fatalf("max chars = %d, want %d", cfg.MaxChars, defaultTitleMaxChars)
	}
	if cfg.Model != "" {
		t.Fatalf("model = %q, want empty", cfg.Model)
	}
}

func TestLoadTitleConfigHonorsEnvOverrides(t *testing.T) {
	t.Setenv(titleEnabledEnv, "false")
	t.Setenv(titleMaxWordsEnv, "8")
	t.Setenv(titleMaxCharsEnv, "72")
	t.Setenv(titleModelEnv, "title-model")

	cfg := loadTitleConfig()
	if cfg.Enabled {
		t.Fatalf("enabled = true, want false")
	}
	if cfg.MaxWords != 8 {
		t.Fatalf("max words = %d, want 8", cfg.MaxWords)
	}
	if cfg.MaxChars != 72 {
		t.Fatalf("max chars = %d, want 72", cfg.MaxChars)
	}
	if cfg.Model != "title-model" {
		t.Fatalf("model = %q, want %q", cfg.Model, "title-model")
	}
}

func TestLoadTitleConfigIgnoresInvalidBounds(t *testing.T) {
	t.Setenv(titleMaxWordsEnv, "0")
	t.Setenv(titleMaxCharsEnv, "500")

	cfg := loadTitleConfig()
	if cfg.MaxWords != defaultTitleMaxWords {
		t.Fatalf("max words = %d, want %d", cfg.MaxWords, defaultTitleMaxWords)
	}
	if cfg.MaxChars != defaultTitleMaxChars {
		t.Fatalf("max chars = %d, want %d", cfg.MaxChars, defaultTitleMaxChars)
	}
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

func TestMaybeGenerateThreadTitleSkipsWhenDisabled(t *testing.T) {
	t.Setenv(titleEnabledEnv, "false")

	provider := &titleProvider{response: "Unused"}
	server := &Server{
		llmProvider: provider,
		sessions:    make(map[string]*Session),
		runs:        make(map[string]*Run),
	}
	server.ensureSession("thread-disabled", nil)

	server.maybeGenerateThreadTitle(context.Background(), "thread-disabled", "run-model", []models.Message{
		{ID: "u1", SessionID: "thread-disabled", Role: models.RoleHuman, Content: "Outline a launch checklist."},
		{ID: "a1", SessionID: "thread-disabled", Role: models.RoleAI, Content: "Here is a draft checklist."},
	})

	if provider.lastReq.Model != "" {
		t.Fatalf("provider should not be called when titles are disabled")
	}
	state := server.getThreadState("thread-disabled")
	if got := state.Values["title"]; got != "" {
		t.Fatalf("title = %v, want empty", got)
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
	}, loadTitleConfig())

	if got != "This is a very long generated conversation title that sho..." {
		t.Fatalf("title = %q, want %q", got, "This is a very long generated conversation title that sho...")
	}
}

func TestGenerateThreadTitleUsesConfiguredModelOverride(t *testing.T) {
	t.Setenv(titleModelEnv, "env-title-model")

	provider := &titleProvider{response: "Launch checklist"}
	server := &Server{
		llmProvider:  provider,
		defaultModel: "default-model",
	}

	got := server.generateThreadTitle(context.Background(), "run-model", []models.Message{
		{ID: "u1", SessionID: "thread-5", Role: models.RoleHuman, Content: "Create a launch checklist."},
		{ID: "a1", SessionID: "thread-5", Role: models.RoleAI, Content: "Here is a launch checklist."},
	}, loadTitleConfig())

	if got != "Launch checklist" {
		t.Fatalf("title = %q, want %q", got, "Launch checklist")
	}
	if provider.lastReq.Model != "env-title-model" {
		t.Fatalf("title model = %q, want %q", provider.lastReq.Model, "env-title-model")
	}
}

func TestFallbackTitleUsesEllipsisForLongSingleWordInput(t *testing.T) {
	input := strings.Repeat("迁", 55)
	got := fallbackTitle(input)
	if got != strings.Repeat("迁", 47)+"..." {
		t.Fatalf("title = %q, want %q", got, strings.Repeat("迁", 47)+"...")
	}
}

func TestBuildTitlePromptUsesConfiguredMaxWords(t *testing.T) {
	prompt := buildTitlePrompt("User question", "Assistant answer", titleConfig{Enabled: true, MaxWords: 9, MaxChars: defaultTitleMaxChars})
	if !strings.Contains(prompt, "at most 9 words") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestMain(m *testing.M) {
	for _, env := range []string{titleEnabledEnv, titleMaxWordsEnv, titleMaxCharsEnv, titleModelEnv} {
		_ = os.Unsetenv(env)
	}
	os.Exit(m.Run())
}
