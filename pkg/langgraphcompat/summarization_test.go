package langgraphcompat

import (
	"context"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type summaryProvider struct {
	response string
	lastReq  llm.ChatRequest
}

func (p *summaryProvider) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	p.lastReq = req
	return llm.ChatResponse{
		Model: req.Model,
		Message: models.Message{
			ID:        "summary-response",
			SessionID: "thread-summary",
			Role:      models.RoleAI,
			Content:   p.response,
		},
	}, nil
}

func (p *summaryProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func TestCompactConversationHistoryCondensesOlderMessages(t *testing.T) {
	provider := &summaryProvider{response: "User is debugging a flaky deploy and wants the incident steps preserved."}
	server := &Server{
		llmProvider:  provider,
		defaultModel: "summary-model",
		sessions:     map[string]*Session{},
		runs:         map[string]*Run{},
	}

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

	result := server.compactConversationHistory(context.Background(), "thread-1", "run-model", "", messages)
	if !result.Changed {
		t.Fatal("expected history to be compacted")
	}
	if len(result.Messages) != defaultSummaryKeepMessages {
		t.Fatalf("kept messages=%d want=%d", len(result.Messages), defaultSummaryKeepMessages)
	}
	if result.Summary != provider.response {
		t.Fatalf("summary=%q want=%q", result.Summary, provider.response)
	}
	if provider.lastReq.Model != "run-model" {
		t.Fatalf("summary model=%q want=%q", provider.lastReq.Model, "run-model")
	}
	if len(provider.lastReq.Messages) != 1 || !strings.Contains(provider.lastReq.Messages[0].Content, "New conversation segment:") {
		t.Fatalf("unexpected summary prompt: %#v", provider.lastReq.Messages)
	}
}

func TestCompactConversationHistoryPreservesToolCallBoundary(t *testing.T) {
	server := &Server{sessions: map[string]*Session{}, runs: map[string]*Run{}}
	messages := make([]models.Message, 0, 31)
	for i := 0; i < 19; i++ {
		messages = append(messages, models.Message{
			ID:        "msg",
			SessionID: "thread-1",
			Role:      models.RoleHuman,
			Content:   "history",
		})
	}
	messages = append(messages,
		models.Message{
			ID:        "ai-tool",
			SessionID: "thread-1",
			Role:      models.RoleAI,
			Content:   "calling tools",
			ToolCalls: []models.ToolCall{{ID: "call-1", Name: "bash", Status: models.CallStatusPending}},
		},
		models.Message{
			ID:        "tool-1",
			SessionID: "thread-1",
			Role:      models.RoleTool,
			Content:   "tool output",
			ToolResult: &models.ToolResult{
				CallID:   "call-1",
				ToolName: "bash",
				Status:   models.CallStatusCompleted,
				Content:  "tool output",
			},
		},
		models.Message{
			ID:        "tail",
			SessionID: "thread-1",
			Role:      models.RoleHuman,
			Content:   "latest",
		},
	)
	for i := 0; i < 9; i++ {
		messages = append(messages, models.Message{
			ID:        "recent",
			SessionID: "thread-1",
			Role:      models.RoleAI,
			Content:   "recent context",
		})
	}

	result := server.compactConversationHistory(context.Background(), "thread-1", "", "", messages)
	if len(result.Messages) == 0 {
		t.Fatal("expected kept messages")
	}
	if result.Messages[0].ID != "ai-tool" {
		t.Fatalf("expected compaction boundary to keep ai/tool pair, first kept id=%q", result.Messages[0].ID)
	}
}

func TestFilterTransientMessagesDropsInjectedSummaryPrompt(t *testing.T) {
	msgs := []models.Message{
		{ID: "m1", SessionID: "thread-1", Role: models.RoleHuman, Content: "hello"},
		conversationSummaryMessage("thread-1", "summary"),
	}

	filtered := filterTransientMessages(msgs)
	if len(filtered) != 1 {
		t.Fatalf("filtered len=%d want=1", len(filtered))
	}
	if filtered[0].ID != "m1" {
		t.Fatalf("kept id=%q want=m1", filtered[0].ID)
	}
}

func TestSetThreadHistorySummaryStoresMetadata(t *testing.T) {
	server := &Server{
		sessions: map[string]*Session{},
		runs:     map[string]*Run{},
	}
	server.ensureSession("thread-1", nil)

	server.setThreadHistorySummary("thread-1", "important summary")

	session := server.ensureSession("thread-1", nil)
	if got := stringValue(session.Metadata[historySummaryMetadataKey]); got != "important summary" {
		t.Fatalf("history_summary=%q want=%q", got, "important summary")
	}
	if stringValue(session.Metadata[historySummaryUpdatedAtMetadataKey]) == "" {
		t.Fatal("expected history_summary_updated_at metadata")
	}
}
