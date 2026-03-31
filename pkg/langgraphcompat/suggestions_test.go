package langgraphcompat

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type suggestionsProvider struct {
	response string
	err      error
	lastReq  llm.ChatRequest
}

func (p *suggestionsProvider) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	p.lastReq = req
	if p.err != nil {
		return llm.ChatResponse{}, p.err
	}
	return llm.ChatResponse{
		Model: req.Model,
		Message: models.Message{
			ID:        "suggestions-response",
			SessionID: "suggestions",
			Role:      models.RoleAI,
			Content:   p.response,
		},
	}, nil
}

func (p *suggestionsProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func TestGenerateSuggestionsBackfillsMissingLLMItems(t *testing.T) {
	provider := &suggestionsProvider{
		response: `["先给我一个部署清单"]`,
	}
	server := &Server{
		llmProvider: provider,
	}

	got := server.generateSuggestions(context.Background(), []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{
		{Role: "user", Content: "请帮我分析部署方案"},
		{Role: "assistant", Content: "可以先确认目标环境和约束。"},
	}, 3, "run-model")

	if len(got) != 3 {
		t.Fatalf("len=%d want=3 (%v)", len(got), got)
	}
	if got[0] != "先给我一个部署清单" {
		t.Fatalf("first=%q want=%q", got[0], "先给我一个部署清单")
	}
	if got[1] == got[0] || got[2] == got[0] {
		t.Fatalf("expected fallback suggestions to avoid duplicates: %v", got)
	}
}

func TestFinalizeSuggestionsDeduplicatesAndFills(t *testing.T) {
	got := finalizeSuggestions(
		[]string{"Q1", " Q1 ", "", "Q2\n"},
		[]string{"Q2", "Q3", "Q4"},
		3,
	)

	if len(got) != 3 {
		t.Fatalf("len=%d want=3 (%v)", len(got), got)
	}
	if got[0] != "Q1" || got[1] != "Q2" || got[2] != "Q3" {
		t.Fatalf("got=%v want=[Q1 Q2 Q3]", got)
	}
}
