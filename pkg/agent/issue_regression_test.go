package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestIssue2123ToolCallsRemainPairedWithToolResults(t *testing.T) {
	registry := tools.NewRegistry()
	for _, name := range []string{"read_file", "write_file"} {
		toolName := name
		if err := registry.Register(models.Tool{
			Name:        toolName,
			Description: toolName,
			Handler: func(_ context.Context, call models.ToolCall) (models.ToolResult, error) {
				return models.ToolResult{
					CallID:   call.ID,
					ToolName: call.Name,
					Status:   models.CallStatusCompleted,
					Content:  toolName + " ok",
				}, nil
			},
		}); err != nil {
			t.Fatalf("register %s: %v", toolName, err)
		}
	}

	var chatCalls int
	provider := chatOnlyProvider{t: t, chat: func(req llm.ChatRequest) llm.ChatResponse {
		chatCalls++
		switch chatCalls {
		case 1:
			return llm.ChatResponse{
				Model: "test-model",
				Message: models.Message{
					Role: models.RoleAI,
					ToolCalls: []models.ToolCall{{
						ID:   "call-1",
						Name: "read_file",
						Arguments: map[string]any{
							"path": "/mnt/skills/public/frontend-design/SKILL.md",
						},
					}},
				},
				Stop: "tool_calls",
			}
		case 2:
			assertLastToolRoundTrip(t, req.Messages, "call-1", "read_file")
			return llm.ChatResponse{
				Model: "test-model",
				Message: models.Message{
					Role: models.RoleAI,
					ToolCalls: []models.ToolCall{{
						ID:   "call-2",
						Name: "write_file",
						Arguments: map[string]any{
							"path":    "/mnt/user-data/outputs/index.html",
							"content": "<html></html>",
						},
					}},
				},
				Stop: "tool_calls",
			}
		case 3:
			assertLastToolRoundTrip(t, req.Messages, "call-2", "write_file")
			return llm.ChatResponse{
				Model: "test-model",
				Message: models.Message{
					Role:    models.RoleAI,
					Content: "done",
				},
				Stop: "stop",
			}
		default:
			t.Fatalf("unexpected Chat call %d", chatCalls)
			return llm.ChatResponse{}
		}
	}}

	runAgent := New(AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    6,
	})

	result, err := runAgent.Run(context.Background(), "issue-2123", []models.Message{{
		ID:        "m1",
		SessionID: "issue-2123",
		Role:      models.RoleHuman,
		Content:   "build the page",
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "done" {
		t.Fatalf("FinalOutput=%q want done", result.FinalOutput)
	}
}

func TestIssue2124ProviderErrorsRemainDiagnosable(t *testing.T) {
	runAgent := New(AgentConfig{
		LLMProvider: issue2124FailingProvider{err: errors.New("provider returned 403: code 30001 insufficient balance")},
		MaxTurns:    2,
	})

	_, err := runAgent.Run(context.Background(), "issue-2124", []models.Message{{
		ID:        "m1",
		SessionID: "issue-2124",
		Role:      models.RoleHuman,
		Content:   "hi",
	}})
	if err == nil {
		t.Fatal("Run() error = nil want provider failure")
	}
	if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "30001") {
		t.Fatalf("error=%q want provider diagnostic details", err.Error())
	}
	if strings.Contains(strings.ToLower(err.Error()), "network error") {
		t.Fatalf("error=%q should not collapse into generic network error", err.Error())
	}
}

func TestIssue2139WebSearchTextSimulationTriggersRetryAndRealToolCall(t *testing.T) {
	registry := tools.NewRegistry()
	webSearchCalls := 0
	if err := registry.Register(models.Tool{
		Name:        "web_search",
		Description: "search the web",
		Handler: func(_ context.Context, call models.ToolCall) (models.ToolResult, error) {
			webSearchCalls++
			query := strings.TrimSpace(stringFromAny(call.Arguments["query"]))
			if query == "" {
				return models.ToolResult{}, errors.New("missing required argument \"query\"")
			}
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusCompleted,
				Content:  "found pricing page for " + query,
			}, nil
		},
	}); err != nil {
		t.Fatalf("register web_search: %v", err)
	}

	provider := &issue2139WebSearchSimulationProvider{t: t}
	runAgent := New(AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    4,
	})

	result, err := runAgent.Run(context.Background(), "issue-2139", []models.Message{{
		ID:        "m1",
		SessionID: "issue-2139",
		Role:      models.RoleHuman,
		Content:   "联网搜索今天的 OpenAI API 价格页",
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "done" {
		t.Fatalf("FinalOutput=%q want done", result.FinalOutput)
	}
	if webSearchCalls != 1 {
		t.Fatalf("web_search calls=%d want 1", webSearchCalls)
	}
	if provider.calls != 3 {
		t.Fatalf("provider calls=%d want 3", provider.calls)
	}
	if !messagesContainToolResult(result.Messages, "web_search") {
		t.Fatalf("messages=%#v want web_search tool result", result.Messages)
	}
	if !messagesContainHumanText(result.Messages, "Do not simulate search results in plain text.") {
		t.Fatalf("messages=%#v want retry prompt", result.Messages)
	}
}

func TestIssueC07UploadTextSimulationTriggersRetryAndReadFileToolCall(t *testing.T) {
	registry := tools.NewRegistry()
	readFileCalls := 0
	if err := registry.Register(models.Tool{
		Name:        "read_file",
		Description: "read uploaded file",
		Handler: func(_ context.Context, call models.ToolCall) (models.ToolResult, error) {
			readFileCalls++
			path := strings.TrimSpace(stringFromAny(call.Arguments["path"]))
			if path == "" {
				return models.ToolResult{}, errors.New("missing required argument \"path\"")
			}
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusCompleted,
				Content:  "风险1: 供应延期\n风险2: 范围变更\n风险3: 预算收紧",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register read_file: %v", err)
	}

	provider := &issueC07UploadReadFileProvider{t: t}
	runAgent := New(AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    4,
	})

	result, err := runAgent.Run(context.Background(), "issue-c07", []models.Message{{
		ID:        "m1",
		SessionID: "issue-c07",
		Role:      models.RoleHuman,
		Content:   "<uploaded_files>\n- weekly_note.txt (2.0 KB)\n  Path: /mnt/user-data/uploads/weekly_note.txt\n</uploaded_files>\n\n基于我上传的 weekly_note.txt，输出 3 条风险和 2 条建议。",
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "done" {
		t.Fatalf("FinalOutput=%q want done", result.FinalOutput)
	}
	if readFileCalls != 1 {
		t.Fatalf("read_file calls=%d want 1", readFileCalls)
	}
	if provider.calls != 3 {
		t.Fatalf("provider calls=%d want 3", provider.calls)
	}
	if !messagesContainToolResult(result.Messages, "read_file") {
		t.Fatalf("messages=%#v want read_file tool result", result.Messages)
	}
	if !messagesContainHumanText(result.Messages, "Do not answer from filename metadata only. Call `read_file` now") {
		t.Fatalf("messages=%#v want read_file retry prompt", result.Messages)
	}
}

func TestIssue2123ToolCallContinuitySurvivesFailedToolRetry(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name:        "read_file",
		Description: "read file",
		Handler: func(_ context.Context, call models.ToolCall) (models.ToolResult, error) {
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusCompleted,
				Content:  "skill loaded",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register read_file: %v", err)
	}

	webSearchAttempts := 0
	if err := registry.Register(models.Tool{
		Name:        "web_search",
		Description: "search web",
		Handler: func(_ context.Context, call models.ToolCall) (models.ToolResult, error) {
			webSearchAttempts++
			if webSearchAttempts == 1 {
				return models.ToolResult{
					CallID:   call.ID,
					ToolName: call.Name,
					Status:   models.CallStatusFailed,
					Content:  "temporary upstream timeout",
				}, nil
			}
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusCompleted,
				Content:  "search ok",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register web_search: %v", err)
	}

	provider := &issue2123RetryProvider{t: t}
	runAgent := New(AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    6,
	})

	result, err := runAgent.Run(context.Background(), "issue-2123-retry", []models.Message{{
		ID:        "m1",
		SessionID: "issue-2123-retry",
		Role:      models.RoleHuman,
		Content:   "先读取 skill，再联网查价格，失败请重试一次",
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "done" {
		t.Fatalf("FinalOutput=%q want done", result.FinalOutput)
	}
	if webSearchAttempts != 2 {
		t.Fatalf("web_search attempts=%d want 2", webSearchAttempts)
	}
}

func assertLastToolRoundTrip(t *testing.T, messages []models.Message, callID, toolName string) {
	t.Helper()
	if len(messages) < 2 {
		t.Fatalf("messages=%d want at least 2", len(messages))
	}
	toolMsg := messages[len(messages)-1]
	assistantMsg := messages[len(messages)-2]
	if assistantMsg.Role != models.RoleAI || len(assistantMsg.ToolCalls) != 1 {
		t.Fatalf("assistant message=%#v want single tool call", assistantMsg)
	}
	if assistantMsg.ToolCalls[0].ID != callID || assistantMsg.ToolCalls[0].Name != toolName {
		t.Fatalf("assistant tool call=%#v want %s/%s", assistantMsg.ToolCalls[0], toolName, callID)
	}
	if toolMsg.Role != models.RoleTool || toolMsg.ToolResult == nil {
		t.Fatalf("tool message=%#v want tool result", toolMsg)
	}
	if toolMsg.ToolResult.CallID != callID || toolMsg.ToolResult.ToolName != toolName {
		t.Fatalf("tool result=%#v want %s/%s", toolMsg.ToolResult, toolName, callID)
	}
}

func assertToolResultStatus(t *testing.T, messages []models.Message, callID, toolName string, status models.CallStatus) {
	t.Helper()
	for _, msg := range messages {
		if msg.Role != models.RoleTool || msg.ToolResult == nil {
			continue
		}
		if msg.ToolResult.CallID == callID && msg.ToolResult.ToolName == toolName {
			if msg.ToolResult.Status != status {
				t.Fatalf("tool result %s/%s status=%s want %s", toolName, callID, msg.ToolResult.Status, status)
			}
			return
		}
	}
	t.Fatalf("tool result %s/%s not found in messages=%#v", toolName, callID, messages)
}

type issue2124FailingProvider struct {
	err error
}

func (p issue2124FailingProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, p.err
}

func (p issue2124FailingProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Err: p.err, Done: true}
	close(ch)
	return ch, nil
}

type issue2139WebSearchSimulationProvider struct {
	t     *testing.T
	calls int
}

type issueC07UploadReadFileProvider struct {
	t     *testing.T
	calls int
}

func (p *issueC07UploadReadFileProvider) PrefersStructuredToolCalls() bool { return true }

func (p *issueC07UploadReadFileProvider) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	p.calls++
	switch p.calls {
	case 1:
		if !toolListContains(req.Tools, "read_file") {
			p.t.Fatalf("tools=%v want read_file", toolNames(req.Tools))
		}
		return llm.ChatResponse{
			Model: "test-model",
			Message: models.Message{
				Role:    models.RoleAI,
				Content: "我会基于你上传的 weekly_note.txt 给出风险和建议。",
			},
			Stop: "stop",
		}, nil
	case 2:
		if len(req.Messages) == 0 || req.Messages[len(req.Messages)-1].Role != models.RoleHuman {
			p.t.Fatalf("last message=%#v want human retry prompt", req.Messages)
		}
		if !strings.Contains(req.Messages[len(req.Messages)-1].Content, "Do not answer from filename metadata only. Call `read_file` now") {
			p.t.Fatalf("retry prompt=%q", req.Messages[len(req.Messages)-1].Content)
		}
		return llm.ChatResponse{
			Model: "test-model",
			Message: models.Message{
				Role: models.RoleAI,
				ToolCalls: []models.ToolCall{{
					ID:   "read-1",
					Name: "read_file",
					Arguments: map[string]any{
						"path": "/mnt/user-data/uploads/weekly_note.txt",
					},
				}},
			},
			Stop: "tool_calls",
		}, nil
	case 3:
		if !messagesContainToolResult(req.Messages, "read_file") {
			p.t.Fatalf("messages=%#v want read_file result in history", req.Messages)
		}
		return llm.ChatResponse{
			Model: "test-model",
			Message: models.Message{
				Role:    models.RoleAI,
				Content: "done",
			},
			Stop: "stop",
		}, nil
	default:
		p.t.Fatalf("unexpected Chat call %d", p.calls)
		return llm.ChatResponse{}, nil
	}
}

func (p *issueC07UploadReadFileProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	if p.t != nil {
		p.t.Fatal("Stream() should not be used for structured tool turns")
	}
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (p *issue2139WebSearchSimulationProvider) PrefersStructuredToolCalls() bool { return true }

func (p *issue2139WebSearchSimulationProvider) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	p.calls++
	switch p.calls {
	case 1:
		if !toolListContains(req.Tools, "web_search") {
			p.t.Fatalf("tools=%v want web_search", toolNames(req.Tools))
		}
		return llm.ChatResponse{
			Model: "test-model",
			Message: models.Message{
				Role:    models.RoleAI,
				Content: "我会调用 web_search 来联网搜索 OpenAI API 价格页。",
			},
			Stop: "stop",
		}, nil
	case 2:
		if len(req.Messages) == 0 || req.Messages[len(req.Messages)-1].Role != models.RoleHuman {
			p.t.Fatalf("last message=%#v want human retry prompt", req.Messages)
		}
		if !strings.Contains(req.Messages[len(req.Messages)-1].Content, "Do not simulate search results in plain text.") {
			p.t.Fatalf("retry prompt=%q", req.Messages[len(req.Messages)-1].Content)
		}
		return llm.ChatResponse{
			Model: "test-model",
			Message: models.Message{
				Role: models.RoleAI,
				ToolCalls: []models.ToolCall{{
					ID:   "web-1",
					Name: "web_search",
					Arguments: map[string]any{
						"query": "OpenAI API pricing",
					},
				}},
			},
			Stop: "tool_calls",
		}, nil
	case 3:
		if !messagesContainToolResult(req.Messages, "web_search") {
			p.t.Fatalf("messages=%#v want web_search result in history", req.Messages)
		}
		return llm.ChatResponse{
			Model: "test-model",
			Message: models.Message{
				Role:    models.RoleAI,
				Content: "done",
			},
			Stop: "stop",
		}, nil
	default:
		p.t.Fatalf("unexpected Chat call %d", p.calls)
		return llm.ChatResponse{}, nil
	}
}

func (p *issue2139WebSearchSimulationProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	if p.t != nil {
		p.t.Fatal("Stream() should not be used for structured tool turns")
	}
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

type issue2123RetryProvider struct {
	t     *testing.T
	calls int
}

func (p *issue2123RetryProvider) PrefersStructuredToolCalls() bool { return true }

func (p *issue2123RetryProvider) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	p.calls++
	switch p.calls {
	case 1:
		return llm.ChatResponse{
			Model: "test-model",
			Message: models.Message{
				Role: models.RoleAI,
				ToolCalls: []models.ToolCall{
					{
						ID:   "read-1",
						Name: "read_file",
						Arguments: map[string]any{
							"path": "/mnt/skills/public/frontend-design/SKILL.md",
						},
					},
					{
						ID:   "web-1",
						Name: "web_search",
						Arguments: map[string]any{
							"query": "OpenAI API pricing",
						},
					},
				},
			},
			Stop: "tool_calls",
		}, nil
	case 2:
		assertToolResultStatus(p.t, req.Messages, "read-1", "read_file", models.CallStatusCompleted)
		assertToolResultStatus(p.t, req.Messages, "web-1", "web_search", models.CallStatusFailed)
		return llm.ChatResponse{
			Model: "test-model",
			Message: models.Message{
				Role: models.RoleAI,
				ToolCalls: []models.ToolCall{
					{
						ID:   "web-2",
						Name: "web_search",
						Arguments: map[string]any{
							"query": "OpenAI API pricing",
						},
					},
				},
			},
			Stop: "tool_calls",
		}, nil
	case 3:
		assertToolResultStatus(p.t, req.Messages, "web-1", "web_search", models.CallStatusFailed)
		assertToolResultStatus(p.t, req.Messages, "web-2", "web_search", models.CallStatusCompleted)
		return llm.ChatResponse{
			Model: "test-model",
			Message: models.Message{
				Role:    models.RoleAI,
				Content: "done",
			},
			Stop: "stop",
		}, nil
	default:
		p.t.Fatalf("unexpected Chat call %d", p.calls)
		return llm.ChatResponse{}, nil
	}
}

func (p *issue2123RetryProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	if p.t != nil {
		p.t.Fatal("Stream() should not be used for structured tool turns")
	}
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func toolListContains(items []models.Tool, name string) bool {
	name = strings.TrimSpace(name)
	for _, item := range items {
		if strings.TrimSpace(item.Name) == name {
			return true
		}
	}
	return false
}

func messagesContainToolResult(messages []models.Message, toolName string) bool {
	toolName = strings.TrimSpace(toolName)
	for _, msg := range messages {
		if msg.Role != models.RoleTool || msg.ToolResult == nil {
			continue
		}
		if strings.TrimSpace(msg.ToolResult.ToolName) == toolName {
			return true
		}
	}
	return false
}

func messagesContainHumanText(messages []models.Message, needle string) bool {
	needle = strings.TrimSpace(needle)
	for _, msg := range messages {
		if msg.Role != models.RoleHuman {
			continue
		}
		if strings.Contains(msg.Content, needle) {
			return true
		}
	}
	return false
}
