package langgraphcompat

import (
	"context"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestGenerateThreadTitleUsesMinimalReasoningEffortForBackgroundCall(t *testing.T) {
	t.Setenv("DEERFLOW_MODELS_JSON", `[{"name":"gpt-5","model":"openai/gpt-5","supports_thinking":true,"supports_reasoning_effort":true}]`)

	provider := &titleProvider{response: "Launch checklist"}
	server := &Server{
		llmProvider:  provider,
		defaultModel: "gpt-5",
	}

	got := server.generateThreadTitle(context.Background(), "gpt-5", []models.Message{
		{ID: "u1", SessionID: "thread-title", Role: models.RoleHuman, Content: "Create a launch checklist."},
		{ID: "a1", SessionID: "thread-title", Role: models.RoleAI, Content: "Here is a first draft."},
	}, loadTitleConfig())

	if got != "Launch checklist" {
		t.Fatalf("title=%q want=%q", got, "Launch checklist")
	}
	if provider.lastReq.ReasoningEffort != "minimal" {
		t.Fatalf("reasoning_effort=%q want=%q", provider.lastReq.ReasoningEffort, "minimal")
	}
}

func TestGenerateSuggestionsUsesMinimalReasoningEffortForBackgroundCall(t *testing.T) {
	t.Setenv("DEERFLOW_MODELS_JSON", `[{"name":"gpt-5","model":"openai/gpt-5","supports_thinking":true,"supports_reasoning_effort":true}]`)

	provider := &suggestionsProvider{response: `["先整理一个执行清单"]`}
	server := &Server{
		llmProvider:  provider,
		defaultModel: "gpt-5",
	}

	got := server.generateSuggestions(context.Background(), "", []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{
		{Role: "user", Content: "请帮我推进发布计划"},
		{Role: "assistant", Content: "我先整理关键步骤。"},
	}, 1, "gpt-5")

	if len(got) != 1 || got[0] != "先整理一个执行清单" {
		t.Fatalf("got=%v want=[先整理一个执行清单]", got)
	}
	if provider.lastReq.ReasoningEffort != "minimal" {
		t.Fatalf("reasoning_effort=%q want=%q", provider.lastReq.ReasoningEffort, "minimal")
	}
}

func TestGenerateConversationSummaryUsesMinimalReasoningEffortForBackgroundCall(t *testing.T) {
	t.Setenv("DEERFLOW_MODELS_JSON", `[{"name":"gpt-5","model":"openai/gpt-5","supports_thinking":true,"supports_reasoning_effort":true}]`)

	provider := &summaryProvider{response: "User needs a concise release summary."}
	server := &Server{
		llmProvider:  provider,
		defaultModel: "gpt-5",
	}

	got := server.generateConversationSummary(context.Background(), "thread-summary", "gpt-5", "", []models.Message{
		{ID: "u1", SessionID: "thread-summary", Role: models.RoleHuman, Content: "Summarize the release notes."},
		{ID: "a1", SessionID: "thread-summary", Role: models.RoleAI, Content: "Here is the draft summary."},
	})

	if got != "User needs a concise release summary." {
		t.Fatalf("summary=%q want=%q", got, "User needs a concise release summary.")
	}
	if provider.lastReq.ReasoningEffort != "minimal" {
		t.Fatalf("reasoning_effort=%q want=%q", provider.lastReq.ReasoningEffort, "minimal")
	}
}

func TestBackgroundReasoningEffortClearsForModelsWithoutSupport(t *testing.T) {
	t.Setenv("DEERFLOW_MODELS_JSON", `[{"name":"fast-model","model":"acme/fast-model","supports_thinking":false,"supports_reasoning_effort":false}]`)

	server := &Server{defaultModel: "fast-model"}
	if got := server.backgroundReasoningEffort("fast-model"); got != "" {
		t.Fatalf("reasoning_effort=%q want empty", got)
	}
	if got := server.backgroundReasoningEffort("acme/fast-model"); got != "" {
		t.Fatalf("reasoning_effort=%q want empty", got)
	}
}

func TestBackgroundReasoningEffortFallsBackToCapabilityInference(t *testing.T) {
	t.Setenv("DEERFLOW_MODELS_JSON", `[]`)

	server := &Server{defaultModel: "gpt-5"}
	if got := server.backgroundReasoningEffort("openai/gpt-5"); got != "minimal" {
		t.Fatalf("reasoning_effort=%q want=%q", got, "minimal")
	}
	if got := server.backgroundReasoningEffort("openai/gpt-4.1-mini"); got != "" {
		t.Fatalf("reasoning_effort=%q want empty", got)
	}
	if got := strings.TrimSpace(server.backgroundReasoningEffort("")); got != "minimal" {
		t.Fatalf("reasoning_effort=%q want=%q", got, "minimal")
	}
}
