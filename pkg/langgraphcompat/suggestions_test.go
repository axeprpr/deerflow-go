package langgraphcompat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
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

	got := server.generateSuggestions(context.Background(), "", []struct {
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

func TestGenerateSuggestionsIncludesThreadContextInPrompt(t *testing.T) {
	provider := &suggestionsProvider{
		response: `["先总结这份需求文档"]`,
	}
	dataRoot := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", dataRoot)

	server := &Server{
		llmProvider: provider,
		dataRoot:    dataRoot,
		sessions: map[string]*Session{
			"thread-ctx": {
				ThreadID: "thread-ctx",
				Metadata: map[string]any{
					"title":      "客户门户改版",
					"agent_name": "writer-bot",
				},
				PresentFiles: tools.NewPresentFileRegistry(),
			},
		},
		agents: map[string]gatewayAgent{
			"writer-bot": {
				Name:        "writer-bot",
				Description: "擅长整理需求和生成交付文档",
			},
		},
	}
	outputDir := filepath.Join(server.threadRoot("thread-ctx"), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	summaryPath := filepath.Join(outputDir, "spec-summary.md")
	if err := os.WriteFile(summaryPath, []byte("# summary"), 0o644); err != nil {
		t.Fatalf("write summary: %v", err)
	}
	if err := server.sessions["thread-ctx"].PresentFiles.Register(tools.PresentFile{
		Path:       "/mnt/user-data/outputs/spec-summary.md",
		SourcePath: summaryPath,
	}); err != nil {
		t.Fatalf("register present file: %v", err)
	}

	uploadDir := server.uploadsDir("thread-ctx")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir upload dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "requirements.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}

	got := server.generateSuggestions(context.Background(), "thread-ctx", []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{
		{Role: "user", Content: "帮我梳理客户门户改版需求"},
		{Role: "assistant", Content: "我先看需求文档并整理范围。"},
	}, 1, "run-model")

	if len(got) != 1 || got[0] != "先总结这份需求文档" {
		t.Fatalf("got=%v want=[先总结这份需求文档]", got)
	}

	prompt := provider.lastReq.Messages[0].Content
	for _, want := range []string{
		"Thread title: 客户门户改版",
		"Custom agent: writer-bot - 擅长整理需求和生成交付文档",
		"Uploaded files: requirements.pdf",
		"Generated artifacts: spec-summary.md",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestFallbackSuggestionsUseThreadContextWhenConversationHasNoUserTurn(t *testing.T) {
	got := fallbackSuggestions([]struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{
		{Role: "assistant", Content: "我已经生成了初稿。"},
	}, suggestionContext{
		Uploads:   []string{"requirements.pdf"},
		Artifacts: []string{"spec-summary.md"},
	}, 3)

	if len(got) != 3 {
		t.Fatalf("len=%d want=3 (%v)", len(got), got)
	}
	if got[0] != "先概括这些上传文件的关键内容" {
		t.Fatalf("first=%q want=%q", got[0], "先概括这些上传文件的关键内容")
	}
}
