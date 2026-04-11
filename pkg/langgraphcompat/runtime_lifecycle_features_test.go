package langgraphcompat

import (
	"context"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestBeforeRunMemoryFeatureInjectsStoredMemory(t *testing.T) {
	store, err := pkgmemory.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if err := store.Save(context.Background(), pkgmemory.Document{
		SessionID: "agent:planner",
		User: pkgmemory.UserMemory{
			TopOfMind: "Need the assistant to prioritize release readiness.",
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	server := &Server{}
	state := &harness.RunState{
		ThreadID:  "thread-1",
		AgentName: "planner",
		Spec: harness.AgentSpec{
			SystemPrompt: "Base prompt",
		},
		Metadata: map[string]any{},
	}

	server.beforeRunMemoryFeature(context.Background(), harness.NewMemoryRuntime(store, nil), state)

	if got := stringValue(state.Metadata["memory_session_id"]); got != "agent:planner" {
		t.Fatalf("memory_session_id=%q want=%q", got, "agent:planner")
	}
	if !strings.Contains(state.Spec.SystemPrompt, "## User Memory") {
		t.Fatalf("system prompt missing memory injection: %q", state.Spec.SystemPrompt)
	}
	if !strings.Contains(state.Spec.SystemPrompt, "Top Of Mind: Need the assistant to prioritize release readiness.") {
		t.Fatalf("system prompt missing memory content: %q", state.Spec.SystemPrompt)
	}
}

func TestSummarizationLifecycleCompactsAndPersistsSummary(t *testing.T) {
	provider := &summaryProvider{response: "User is tracking deployment regressions and wants only the latest steps kept."}
	server := &Server{
		llmProvider:  provider,
		defaultModel: "summary-model",
		sessions:     map[string]*Session{},
		runs:         map[string]*Run{},
	}
	server.ensureSession("thread-1", nil)

	messages := make([]models.Message, 0, 34)
	for i := 0; i < 34; i++ {
		role := models.RoleHuman
		if i%2 == 1 {
			role = models.RoleAI
		}
		messages = append(messages, models.Message{
			ID:        "m" + string(rune('a'+(i%26))),
			SessionID: "thread-1",
			Role:      role,
			Content:   strings.Repeat("message content ", 20),
		})
	}

	state := &harness.RunState{
		ThreadID: "thread-1",
		Model:    "run-model",
		Messages: append([]models.Message(nil), messages...),
		Metadata: map[string]any{},
	}

	if err := server.beforeRunSummarizationFeature(context.Background(), state); err != nil {
		t.Fatalf("beforeRunSummarizationFeature() error = %v", err)
	}
	if len(state.Messages) != defaultSummaryKeepMessages {
		t.Fatalf("kept messages=%d want=%d", len(state.Messages), defaultSummaryKeepMessages)
	}
	if got := stringValue(state.Metadata[historySummaryMetadataKey]); got != provider.response {
		t.Fatalf("summary=%q want=%q", got, provider.response)
	}

	server.afterRunSummarizationFeature(state)
	if got := server.threadHistorySummary("thread-1"); got != provider.response {
		t.Fatalf("persisted summary=%q want=%q", got, provider.response)
	}
}
