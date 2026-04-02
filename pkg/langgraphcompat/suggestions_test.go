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

func TestParseJSONStringListAcceptsWrappedObject(t *testing.T) {
	got := parseJSONStringList(`{"suggestions":["先总结一下","给我下一步建议"]}`)

	if len(got) != 2 {
		t.Fatalf("len=%d want=2 (%v)", len(got), got)
	}
	if got[0] != "先总结一下" || got[1] != "给我下一步建议" {
		t.Fatalf("got=%v", got)
	}
}

func TestParseJSONStringListAcceptsWrappedObjectArrayOfObjects(t *testing.T) {
	got := parseJSONStringList(`{"suggestions":[{"text":"先总结一下"},{"question":"给我下一步建议"}]}`)

	if len(got) != 2 {
		t.Fatalf("len=%d want=2 (%v)", len(got), got)
	}
	if got[0] != "先总结一下" || got[1] != "给我下一步建议" {
		t.Fatalf("got=%v", got)
	}
}

func TestParseJSONStringListAcceptsSingleObjectSuggestion(t *testing.T) {
	got := parseJSONStringList(`{"question":"先总结一下"}`)

	if len(got) != 1 {
		t.Fatalf("len=%d want=1 (%v)", len(got), got)
	}
	if got[0] != "先总结一下" {
		t.Fatalf("got=%v", got)
	}
}

func TestParseJSONStringListFallsBackToBulletList(t *testing.T) {
	got := parseJSONStringList("Here are some ideas:\n- 先总结当前方案\n- 给我列出风险点\n")

	if len(got) != 2 {
		t.Fatalf("len=%d want=2 (%v)", len(got), got)
	}
	if got[0] != "先总结当前方案" || got[1] != "给我列出风险点" {
		t.Fatalf("got=%v", got)
	}
}

func TestParseJSONStringListAcceptsFencedWrappedObject(t *testing.T) {
	got := parseJSONStringList("```json\n{\"questions\":[\"先总结当前方案\",\"给我列出风险点\"]}\n```")

	if len(got) != 2 {
		t.Fatalf("len=%d want=2 (%v)", len(got), got)
	}
	if got[0] != "先总结当前方案" || got[1] != "给我列出风险点" {
		t.Fatalf("got=%v", got)
	}
}

func TestParseJSONStringListFallsBackToParenthesizedNumberedList(t *testing.T) {
	got := parseJSONStringList("1) 先总结当前方案\n2) 给我列出风险点\n")

	if len(got) != 2 {
		t.Fatalf("len=%d want=2 (%v)", len(got), got)
	}
	if got[0] != "先总结当前方案" || got[1] != "给我列出风险点" {
		t.Fatalf("got=%v", got)
	}
}

func TestFallbackSuggestionsUseThreadContextWhenConversationHasNoUserTurn(t *testing.T) {
	got := fallbackSuggestions([]struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{
		{Role: "assistant", Content: "我已经生成了初稿。"},
	}, suggestionContext{
		Title:     "需求整理",
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

func TestFallbackSuggestionsUseEnglishThreadContextWhenConversationHasNoUserTurn(t *testing.T) {
	got := fallbackSuggestions([]struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{
		{Role: "assistant", Content: "I already drafted the migration summary."},
	}, suggestionContext{
		Title:     "Migration planning",
		AgentName: "writer-bot",
		Uploads:   []string{"requirements.pdf"},
		Artifacts: []string{"migration-summary.md"},
	}, 3)

	if len(got) != 3 {
		t.Fatalf("len=%d want=3 (%v)", len(got), got)
	}
	if got[0] != "Summarize the key points from these uploaded files first." {
		t.Fatalf("first=%q", got[0])
	}
	if strings.Contains(got[0], "这些上传文件") {
		t.Fatalf("first=%q unexpectedly Chinese", got[0])
	}
}

func TestFallbackSuggestionsUseAssistantLanguageWithoutThreadContext(t *testing.T) {
	got := fallbackSuggestions([]struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{
		{Role: "assistant", Content: "要点を整理しました。次は優先順位を詰めましょう。"},
	}, suggestionContext{}, 3)

	if len(got) != 3 {
		t.Fatalf("len=%d want=3 (%v)", len(got), got)
	}
	if got[0] != "現在のスレッド文脈をもとに、次のステップを整理してください。" {
		t.Fatalf("first=%q", got[0])
	}
	if strings.Contains(strings.ToLower(got[0]), "thread context") {
		t.Fatalf("first=%q unexpectedly English", got[0])
	}
}

func TestFallbackSuggestionsFillContextCandidatesUpToRequestedCount(t *testing.T) {
	got := fallbackSuggestions([]struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{
		{Role: "assistant", Content: "I can help you continue with the writer workflow."},
	}, suggestionContext{
		AgentName: "writer-bot",
	}, 3)

	if len(got) != 3 {
		t.Fatalf("len=%d want=3 (%v)", len(got), got)
	}
	if got[0] != "Use this agent to refine the next steps for me." {
		t.Fatalf("first=%q", got[0])
	}
	if got[1] != "Based on the current thread context, what should I do next?" {
		t.Fatalf("second=%q", got[1])
	}
	if got[2] != "Summarize the key conclusions and open questions in this thread." {
		t.Fatalf("third=%q", got[2])
	}
}

func TestLocalizedContextFallbackSuggestionsDeduplicatesGenericFillers(t *testing.T) {
	got := localizedContextFallbackSuggestions(suggestionContext{}, "Please summarize the thread context", 2)

	if len(got) != 2 {
		t.Fatalf("len=%d want=2 (%v)", len(got), got)
	}
	if got[0] != "Based on the current thread context, what should I do next?" {
		t.Fatalf("first=%q", got[0])
	}
	if got[1] != "Summarize the key conclusions and open questions in this thread." {
		t.Fatalf("second=%q", got[1])
	}
}
