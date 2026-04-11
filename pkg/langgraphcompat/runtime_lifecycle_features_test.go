package langgraphcompat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
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

func TestAfterRunClarificationFeatureStoresInterruptMetadata(t *testing.T) {
	server := &Server{}
	state := &harness.RunState{
		ThreadID: "thread-clarify",
		Metadata: map[string]any{},
	}
	result := &agent.RunResult{
		Messages: []models.Message{{
			Role: models.RoleTool,
			ToolResult: &models.ToolResult{
				ToolName: "ask_clarification",
				Status:   models.CallStatusCompleted,
				Content:  "Need more detail",
				Data: map[string]any{
					"id":       "clarify-1",
					"question": "Need more detail?",
				},
			},
		}},
	}

	server.afterRunClarificationFeature(state, result)

	raw, _ := state.Metadata["clarification_interrupt"].(map[string]any)
	if stringValue(raw["id"]) != "clarify-1" {
		t.Fatalf("clarification id = %q", stringValue(raw["id"]))
	}
	if stringValue(raw["question"]) != "Need more detail?" {
		t.Fatalf("clarification question = %q", stringValue(raw["question"]))
	}
}

func TestAfterRunTitleFeatureStoresGeneratedTitle(t *testing.T) {
	provider := &titleProvider{response: "Release rollout plan"}
	server := &Server{
		llmProvider: provider,
		sessions:    map[string]*Session{},
		runs:        map[string]*Run{},
	}
	server.ensureSession("thread-title", nil)

	state := &harness.RunState{
		ThreadID: "thread-title",
		Model:    "run-model",
		Metadata: map[string]any{},
	}
	result := &agent.RunResult{
		Messages: []models.Message{
			{Role: models.RoleHuman, Content: "Plan the release rollout for sqlite migration"},
			{Role: models.RoleAI, Content: "I will draft the rollout plan and checkpoints."},
		},
	}

	server.afterRunTitleFeature(context.Background(), state, result)

	if got := stringValue(state.Metadata["generated_title"]); got != "Release rollout plan" {
		t.Fatalf("generated_title=%q want=%q", got, "Release rollout plan")
	}
}

func TestFinalizeCompletedRunUsesLifecycleMetadataForTitleAndInterrupt(t *testing.T) {
	server := &Server{
		sessions: map[string]*Session{},
		runs:     map[string]*Run{},
	}
	server.ensureSession("thread-finalize", nil)
	prepared := &preparedRunRequest{
		ThreadID: "thread-finalize",
		Run: &Run{
			RunID:     "run-1",
			ThreadID:  "thread-finalize",
			Status:    "running",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
		Lifecycle: &harness.RunState{
			ThreadID: "thread-finalize",
			Metadata: map[string]any{
				"generated_title": "Lifecycle title",
				"clarification_interrupt": map[string]any{
					"value": "Need input",
				},
			},
		},
	}

	server.finalizeCompletedRun(context.Background(), prepared, &agent.RunResult{
		Messages: []models.Message{{Role: models.RoleAI, Content: "Need input"}},
	})

	session := server.ensureSession("thread-finalize", nil)
	if got := stringValue(session.Metadata["title"]); got != "Lifecycle title" {
		t.Fatalf("title=%q want=%q", got, "Lifecycle title")
	}
	if got := session.Status; got != "interrupted" {
		t.Fatalf("status=%q want=%q", got, "interrupted")
	}
}
