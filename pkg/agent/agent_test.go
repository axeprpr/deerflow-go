package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/guardrails"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"github.com/axeprpr/deerflow-go/pkg/tools/builtin"
)

func TestAgentConfig_Defaults(t *testing.T) {
	cfg := AgentConfig{
		MaxTurns: 0,
	}

	agent := New(cfg)

	if agent.maxTurns != defaultMaxTurns {
		t.Errorf("Expected default MaxTurns=%d, got %d", defaultMaxTurns, agent.maxTurns)
	}
}

func TestAgentConfig_CustomMaxTurns(t *testing.T) {
	cfg := AgentConfig{
		MaxTurns: 20,
	}

	agent := New(cfg)

	if agent.maxTurns != 20 {
		t.Errorf("Expected MaxTurns=20, got %d", agent.maxTurns)
	}
}

func TestAgent_Events(t *testing.T) {
	cfg := AgentConfig{
		MaxTurns: 5,
	}

	agent := New(cfg)
	events := agent.Events()

	if events == nil {
		t.Error("Events channel should not be nil")
	}
}

func TestAgent_Run_NoLLMProvider(t *testing.T) {
	cfg := AgentConfig{
		MaxTurns: 5,
	}

	agent := New(cfg)
	_, err := agent.Run(context.Background(), "session_1", []models.Message{
		{ID: "m1", SessionID: "s1", Role: models.RoleHuman, Content: "Hello"},
	})

	if err == nil {
		t.Error("Expected error when LLM provider is nil")
	}
}

func TestAgent_New_WithTools(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(models.Tool{
		Name:        "test",
		Description: "Test tool",
		Handler: func(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
			return models.ToolResult{}, nil
		},
	})

	cfg := AgentConfig{
		MaxTurns: 5,
		Tools:    registry,
	}

	agent := New(cfg)

	if agent.tools != registry {
		t.Error("Tools registry not set correctly")
	}
}

func TestCloneRegistryWithPresentFileToolSupportsLegacySingleFileAlias(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", dataRoot)

	threadID := "thread-present-alias"
	outputsDir := filepath.Join(dataRoot, "threads", threadID, "user-data", "outputs")
	if err := os.MkdirAll(outputsDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	sourcePath := filepath.Join(outputsDir, "report.md")
	if err := os.WriteFile(sourcePath, []byte("# report\n"), 0o644); err != nil {
		t.Fatalf("write output file: %v", err)
	}

	presentRegistry := tools.NewPresentFileRegistry()
	registry := cloneRegistryWithPresentFileTool(nil, presentRegistry)

	if registry.Get("present_file") == nil {
		t.Fatal("expected present_file alias to be registered")
	}
	if registry.Get("present_files") == nil {
		t.Fatal("expected present_files tool to be registered")
	}

	content, err := registry.Call(
		tools.WithThreadID(context.Background(), threadID),
		"present_file",
		map[string]any{"path": sourcePath},
		nil,
	)
	if err != nil {
		t.Fatalf("present_file alias failed: %v", err)
	}
	if content != "Successfully presented files" {
		t.Fatalf("content=%q want success message", content)
	}

	files := presentRegistry.List()
	if len(files) != 1 {
		t.Fatalf("registered files=%d want=1", len(files))
	}
	if files[0].Path != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("registered path=%q want=%q", files[0].Path, "/mnt/user-data/outputs/report.md")
	}
}

func TestCloneRegistryWithPresentFileToolPlacesPresentFilesBeforeClarification(t *testing.T) {
	base := tools.NewRegistry()
	for _, name := range []string{"ls", "read_file", "glob", "grep", "write_file", "str_replace", "bash", "ask_clarification", "task"} {
		if err := base.Register(models.Tool{
			Name: name,
			Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) {
				return models.ToolResult{}, nil
			},
		}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}

	registry := cloneRegistryWithPresentFileTool(base, tools.NewPresentFileRegistry())
	got := make([]string, 0, len(registry.List()))
	for _, tool := range registry.List() {
		got = append(got, tool.Name)
	}
	wantPrefix := []string{"ls", "read_file", "glob", "grep", "write_file", "str_replace", "bash", "present_files", "ask_clarification", "task"}
	if len(got) < len(wantPrefix) {
		t.Fatalf("tool order=%v want prefix=%v", got, wantPrefix)
	}
	for i := range wantPrefix {
		if got[i] != wantPrefix[i] {
			t.Fatalf("tool order=%v want prefix=%v", got, wantPrefix)
		}
	}
}

func TestAgentVisibleToolsHidesLegacyPresentFileAlias(t *testing.T) {
	agent := New(AgentConfig{
		Tools:        tools.NewRegistry(),
		PresentFiles: tools.NewPresentFileRegistry(),
	})

	got := make([]string, 0, len(agent.visibleTools(nil)))
	for _, tool := range agent.visibleTools(nil) {
		got = append(got, tool.Name)
		if tool.Name == "present_file" {
			t.Fatalf("visible tools unexpectedly exposed present_file alias: %v", got)
		}
	}
	if !slices.Contains(got, "present_files") {
		t.Fatalf("visible tools=%v want present_files", got)
	}
}

func TestAgent_BuildSystemPrompt(t *testing.T) {
	cfg := AgentConfig{
		MaxTurns:     5,
		SystemPrompt: "custom system prompt",
	}

	agent := New(cfg)
	ctx := context.Background()

	prompt := agent.BuildSystemPrompt(ctx, "test_session")

	if prompt == "" {
		t.Error("System prompt should not be empty")
	}
	if prompt != "custom system prompt" {
		t.Fatalf("BuildSystemPrompt()=%q want base prompt unchanged when no deferred tools are active", prompt)
	}
	if strings.Contains(prompt, "ReAct-style loop") {
		t.Fatalf("BuildSystemPrompt() unexpectedly included extra react guidance: %q", prompt)
	}
}

func TestAgent_BuildSystemPromptDoesNotInjectCustomToolSummary(t *testing.T) {
	registry := tools.NewRegistry()
	noop := func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }
	if err := registry.Register(models.Tool{Name: "write_file", Description: "write", Handler: noop}); err != nil {
		t.Fatalf("register write_file: %v", err)
	}
	if err := registry.Register(models.Tool{Name: "present_files", Description: "present", Handler: noop}); err != nil {
		t.Fatalf("register present_files: %v", err)
	}

	agent := New(AgentConfig{
		SystemPrompt: "custom system prompt",
		Tools:        registry,
	})

	prompt := agent.BuildSystemPrompt(context.Background(), "test_session")
	if strings.Contains(prompt, "<file_workflow>") {
		t.Fatalf("prompt unexpectedly included custom file workflow guidance: %q", prompt)
	}
	if strings.Contains(prompt, "Available Tools:") {
		t.Fatalf("prompt unexpectedly included tool summary: %q", prompt)
	}
}

func TestAgent_RetriesAfterRecoverableToolValidationFailure(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.WriteFileTool()); err != nil {
		t.Fatalf("register write_file: %v", err)
	}

	provider := &scriptedStreamProvider{
		t: t,
		steps: []streamStep{
			{
				content: "creating page",
				toolCalls: []models.ToolCall{{
					ID:        "call_missing_args",
					Name:      "write_file",
					Arguments: map[string]any{},
					Status:    models.CallStatusPending,
				}},
			},
			{},
			{
				content: "done",
				check: func(t *testing.T, req llm.ChatRequest) {
					if len(req.Messages) < 3 {
						t.Fatalf("third request messages = %d, want at least 3", len(req.Messages))
					}
					last := req.Messages[len(req.Messages)-1]
					if last.Role != models.RoleHuman {
						t.Fatalf("last retry prompt role = %q, want human", last.Role)
					}
					if !strings.Contains(last.Content, "/mnt/user-data/outputs/index.html") {
						t.Fatalf("retry prompt = %q, want write_file guidance", last.Content)
					}
				},
			},
		},
	}

	agent := New(AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    4,
	})

	result, err := agent.Run(context.Background(), "session-retry-tool-validation", []models.Message{{
		ID:        "msg-1",
		SessionID: "session-retry-tool-validation",
		Role:      models.RoleHuman,
		Content:   "帮我生成一个小鱼游泳的页面",
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "done" {
		t.Fatalf("final output = %q, want done", result.FinalOutput)
	}
}

func TestAgent_RunUsesStructuredChatForToolTurns(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name:        "echo_tool",
		Description: "echo content",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []any{"value"}},
		Handler: func(_ context.Context, call models.ToolCall) (models.ToolResult, error) {
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusCompleted,
				Content:  call.Arguments["value"].(string),
			}, nil
		},
	}); err != nil {
		t.Fatalf("register echo_tool: %v", err)
	}

	type structuredToolProvider struct {
		calls int
	}
	var provider structuredToolProvider
	providerChat := func(req llm.ChatRequest) llm.ChatResponse {
		provider.calls++
		switch provider.calls {
		case 1:
			return llm.ChatResponse{
				Message: models.Message{
					Role:    models.RoleAI,
					Content: "先调用工具。",
					ToolCalls: []models.ToolCall{{
						ID:        "call-1",
						Name:      "echo_tool",
						Arguments: map[string]any{"value": "ok"},
						Status:    models.CallStatusPending,
					}},
				},
				Stop: "tool_calls",
			}
		case 2:
			if len(req.Messages) < 3 {
				t.Fatalf("second chat request messages=%d want>=3", len(req.Messages))
			}
			if req.Messages[2].ToolResult == nil || req.Messages[2].ToolResult.Content != "ok" {
				t.Fatalf("tool result history=%#v", req.Messages[2].ToolResult)
			}
			return llm.ChatResponse{
				Message: models.Message{
					Role:    models.RoleAI,
					Content: "done",
				},
				Stop: "stop",
			}
		default:
			t.Fatalf("unexpected Chat call %d", provider.calls)
			return llm.ChatResponse{}
		}
	}

	agent := New(AgentConfig{
		LLMProvider: chatOnlyProvider{t: t, chat: providerChat},
		Tools:       registry,
		MaxTurns:    4,
	})

	result, err := agent.Run(context.Background(), "session_structured_chat", []models.Message{{
		ID:        "m1",
		SessionID: "session_structured_chat",
		Role:      models.RoleHuman,
		Content:   "run tool",
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "done" {
		t.Fatalf("final output=%q want done", result.FinalOutput)
	}
}

func TestAgent_RunKeepsStreamingToolTurnsForNonStructuredProviders(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name:        "echo_tool",
		Description: "echo content",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []any{"value"}},
		Handler: func(_ context.Context, call models.ToolCall) (models.ToolResult, error) {
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusCompleted,
				Content:  call.Arguments["value"].(string),
			}, nil
		},
	}); err != nil {
		t.Fatalf("register echo_tool: %v", err)
	}

	provider := &streamOnlyToolProvider{t: t}
	agent := New(AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    4,
	})

	result, err := agent.Run(context.Background(), "session_stream_only", []models.Message{{
		ID:        "m1",
		SessionID: "session_stream_only",
		Role:      models.RoleHuman,
		Content:   "run tool",
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "done" {
		t.Fatalf("final output=%q want done", result.FinalOutput)
	}
	if provider.streamCalls != 2 {
		t.Fatalf("stream calls=%d want 2", provider.streamCalls)
	}
}

func TestAgent_RunFallsBackToChatWhenToolStreamFailsMidTurn(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name:        "echo_tool",
		Description: "echo content",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []any{"value"}},
		Handler: func(_ context.Context, call models.ToolCall) (models.ToolResult, error) {
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusCompleted,
				Content:  call.Arguments["value"].(string),
			}, nil
		},
	}); err != nil {
		t.Fatalf("register echo_tool: %v", err)
	}

	provider := &streamThenChatFallbackProvider{t: t}
	agent := New(AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    4,
	})

	done := make(chan []AgentEvent, 1)
	go func() {
		var events []AgentEvent
		for evt := range agent.Events() {
			events = append(events, evt)
			if evt.Type == AgentEventEnd || evt.Type == AgentEventError {
				break
			}
		}
		done <- events
	}()

	result, err := agent.Run(context.Background(), "session_stream_chat_fallback", []models.Message{{
		ID:        "m1",
		SessionID: "session_stream_chat_fallback",
		Role:      models.RoleHuman,
		Content:   "run tool",
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "done" {
		t.Fatalf("final output=%q want done", result.FinalOutput)
	}
	if got := provider.streamCalls.Load(); got != 2 {
		t.Fatalf("stream calls=%d want 2", got)
	}
	if got := provider.chatCalls.Load(); got != 1 {
		t.Fatalf("chat calls=%d want 1", got)
	}
	if len(result.Messages) < 4 {
		t.Fatalf("messages=%d want>=4", len(result.Messages))
	}
	toolTurn := result.Messages[1]
	if toolTurn.Content != "先调用工具。" {
		t.Fatalf("tool turn content=%q want 先调用工具。", toolTurn.Content)
	}
	if len(toolTurn.ToolCalls) != 1 || toolTurn.ToolCalls[0].Arguments["value"] != "ok" {
		t.Fatalf("tool turn tool_calls=%#v want completed fallback arguments", toolTurn.ToolCalls)
	}

	events := <-done
	for _, evt := range events {
		if evt.Type == AgentEventError {
			t.Fatalf("unexpected error event: %s", evt.Err)
		}
	}
	var streamedText int
	for _, evt := range events {
		if evt.Type == AgentEventTextChunk && evt.Text == "先调用工具。" {
			streamedText++
		}
	}
	if streamedText != 1 {
		t.Fatalf("text chunk count=%d want 1", streamedText)
	}
}

func TestAgent_RunUsesStreamingForNonToolTurnsEvenWithStructuredProviders(t *testing.T) {
	provider := structuredNoToolProvider{t: t}
	agent := New(AgentConfig{
		LLMProvider: provider,
		MaxTurns:    2,
	})

	result, err := agent.Run(context.Background(), "session_no_tools_stream", []models.Message{{
		ID:        "m1",
		SessionID: "session_no_tools_stream",
		Role:      models.RoleHuman,
		Content:   "just answer",
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "done" {
		t.Fatalf("final output=%q want done", result.FinalOutput)
	}
}

func TestAgent_RunUsesFinalStreamToolCallArguments(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name:        "echo_tool",
		Description: "echo content",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []any{"value"}},
		Handler: func(_ context.Context, call models.ToolCall) (models.ToolResult, error) {
			value, _ := call.Arguments["value"].(string)
			if value == "" {
				return models.ToolResult{CallID: call.ID, ToolName: call.Name}, errors.New("value is required")
			}
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusCompleted,
				Content:  value,
			}, nil
		},
	}); err != nil {
		t.Fatalf("register echo_tool: %v", err)
	}

	provider := &scriptedStreamProvider{
		t: t,
		steps: []streamStep{
			{
				content: "先调用工具。",
				toolCalls: []models.ToolCall{{
					ID:     "call-1",
					Name:   "echo_tool",
					Status: models.CallStatusPending,
				}},
				finalToolCalls: []models.ToolCall{{
					ID:        "call-1",
					Name:      "echo_tool",
					Arguments: map[string]any{"value": "ok"},
					Status:    models.CallStatusPending,
				}},
			},
			{
				content: "done",
				check: func(t *testing.T, req llm.ChatRequest) {
					if len(req.Messages) < 3 {
						t.Fatalf("second request messages=%d want>=3", len(req.Messages))
					}
					if req.Messages[2].ToolResult == nil || req.Messages[2].ToolResult.Content != "ok" {
						t.Fatalf("tool result history=%#v", req.Messages[2].ToolResult)
					}
				},
			},
		},
	}

	agent := New(AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    4,
	})

	result, err := agent.Run(context.Background(), "session_stream_final_tool_args", []models.Message{{
		ID:        "m1",
		SessionID: "session_stream_final_tool_args",
		Role:      models.RoleHuman,
		Content:   "run tool",
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "done" {
		t.Fatalf("final output=%q want done", result.FinalOutput)
	}
}

func TestAgent_EinoAgent(t *testing.T) {
	cfg := AgentConfig{
		MaxTurns: 5,
	}

	agent := New(cfg)
	einoAgent := agent.EinoAgent()

	if einoAgent == nil {
		t.Error("EinoAgent should not return nil")
	}
}

func TestAgent_emit(t *testing.T) {
	cfg := AgentConfig{
		MaxTurns: 5,
	}

	agent := New(cfg)

	agent.emit(AgentEvent{
		Type:      AgentEventError,
		SessionID: "test_session",
		Err:       "test error",
	})
}

func TestResolveModel(t *testing.T) {
	// Clear the environment variable first
	os.Unsetenv("DEFAULT_LLM_MODEL")

	tests := []struct {
		input    string
		expected string
	}{
		{"gpt-4", "gpt-4"},
		{"claude-3-opus", "claude-3-opus"},
		{"", "gpt-4.1-mini"},
		{"qwen/qwen3-9b", "qwen/qwen3-9b"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := resolveModel(tt.input)
			if result != tt.expected {
				t.Errorf("resolveModel(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestUsage(t *testing.T) {
	usage := Usage{
		InputTokens:       100,
		OutputTokens:      50,
		TotalTokens:       150,
		ReasoningTokens:   7,
		CachedInputTokens: 11,
	}

	if usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", usage.OutputTokens)
	}
	if usage.TotalTokens != 150 {
		t.Errorf("TotalTokens = %d, want 150", usage.TotalTokens)
	}
	if usage.ReasoningTokens != 7 {
		t.Errorf("ReasoningTokens = %d, want 7", usage.ReasoningTokens)
	}
	if usage.CachedInputTokens != 11 {
		t.Errorf("CachedInputTokens = %d, want 11", usage.CachedInputTokens)
	}
}

func TestRunResult(t *testing.T) {
	result := RunResult{
		Messages: []models.Message{
			{ID: "m1", SessionID: "test_session", Role: models.RoleAI, Content: "Hello"},
		},
		FinalOutput: "Hello",
		Usage: &Usage{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
	}

	if len(result.Messages) != 1 {
		t.Errorf("Messages count = %d, want 1", len(result.Messages))
	}
	if result.FinalOutput != "Hello" {
		t.Errorf("FinalOutput = %s, want 'Hello'", result.FinalOutput)
	}
}

func TestAgentRunUsesRequestTimeout(t *testing.T) {
	runAgent := New(AgentConfig{
		LLMProvider:    timeoutProvider{},
		RequestTimeout: 20 * time.Millisecond,
	})

	_, err := runAgent.Run(context.Background(), "session_1", []models.Message{
		{ID: "m1", SessionID: "s1", Role: models.RoleHuman, Content: "Hello"},
	})
	if err == nil {
		t.Fatal("Run() error = nil, want timeout")
	}

	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("Run() error = %T, want *TimeoutError", err)
	}
}

func TestApplyAgentType(t *testing.T) {
	registry := tools.NewRegistry()
	_ = registry.Register(models.Tool{Name: "bash", Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }})
	_ = registry.Register(models.Tool{Name: "read_file", Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }})
	_ = registry.Register(models.Tool{Name: "write_file", Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }})
	_ = registry.Register(models.Tool{Name: "ask_clarification", Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }})

	cfg := AgentConfig{
		Tools:     registry,
		AgentType: AgentTypeCoder,
	}
	if err := ApplyAgentType(&cfg, cfg.AgentType); err != nil {
		t.Fatalf("ApplyAgentType() error = %v", err)
	}
	if cfg.SystemPrompt == "" {
		t.Fatal("ApplyAgentType() did not set system prompt")
	}
	if cfg.MaxTurns <= 0 {
		t.Fatal("ApplyAgentType() did not set max turns")
	}
	if cfg.Temperature == nil {
		t.Fatal("ApplyAgentType() did not set temperature")
	}
	if cfg.Tools.Get("bash") == nil {
		t.Fatal("ApplyAgentType() removed allowed tool bash")
	}
	if cfg.Tools.Get("read_file") == nil {
		t.Fatal("ApplyAgentType() removed allowed tool read_file")
	}
}

func TestAgentRunWarnsAndRecoversFromRepeatedToolCalls(t *testing.T) {
	var toolExecutions atomic.Int32
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name: "repeat_tool",
		Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) {
			toolExecutions.Add(1)
			return models.ToolResult{
				CallID:   "repeat-call",
				ToolName: "repeat_tool",
				Status:   models.CallStatusCompleted,
				Content:  "tool ok",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	provider := &scriptedStreamProvider{
		steps: []streamStep{
			{toolCalls: []models.ToolCall{{ID: "repeat-call-1", Name: "repeat_tool", Arguments: map[string]any{"path": "a.txt"}}}},
			{toolCalls: []models.ToolCall{{ID: "repeat-call-2", Name: "repeat_tool", Arguments: map[string]any{"path": "a.txt"}}}},
			{toolCalls: []models.ToolCall{{ID: "repeat-call-3", Name: "repeat_tool", Arguments: map[string]any{"path": "a.txt"}}}},
			{check: func(t *testing.T, req llm.ChatRequest) {
				if got := req.Messages[len(req.Messages)-1].Content; got != loopWarningMessage {
					t.Fatalf("last message = %q want %q", got, loopWarningMessage)
				}
			}, content: "Final answer after warning."},
		},
		t: t,
	}

	agent := New(AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    6,
	})

	result, err := agent.Run(context.Background(), "session-loop-warning", []models.Message{
		{ID: "m1", SessionID: "session-loop-warning", Role: models.RoleHuman, Content: "Do the thing"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "Final answer after warning." {
		t.Fatalf("FinalOutput = %q", result.FinalOutput)
	}
	if got := toolExecutions.Load(); got != 3 {
		t.Fatalf("tool executions = %d want 3", got)
	}
}

func TestAgentRunForceStopsRepeatedToolCalls(t *testing.T) {
	var toolExecutions atomic.Int32
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name: "repeat_tool",
		Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) {
			toolExecutions.Add(1)
			return models.ToolResult{
				CallID:   "repeat-call",
				ToolName: "repeat_tool",
				Status:   models.CallStatusCompleted,
				Content:  "tool ok",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	provider := &scriptedStreamProvider{
		steps: []streamStep{
			{toolCalls: []models.ToolCall{{ID: "repeat-call-1", Name: "repeat_tool", Arguments: map[string]any{"path": "a.txt"}}}},
			{toolCalls: []models.ToolCall{{ID: "repeat-call-2", Name: "repeat_tool", Arguments: map[string]any{"path": "a.txt"}}}},
			{toolCalls: []models.ToolCall{{ID: "repeat-call-3", Name: "repeat_tool", Arguments: map[string]any{"path": "a.txt"}}}},
			{toolCalls: []models.ToolCall{{ID: "repeat-call-4", Name: "repeat_tool", Arguments: map[string]any{"path": "a.txt"}}}},
			{toolCalls: []models.ToolCall{{ID: "repeat-call-5", Name: "repeat_tool", Arguments: map[string]any{"path": "a.txt"}}}},
		},
		t: t,
	}

	agent := New(AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    8,
	})

	result, err := agent.Run(context.Background(), "session-loop-hard-stop", []models.Message{
		{ID: "m1", SessionID: "session-loop-hard-stop", Role: models.RoleHuman, Content: "Do the thing"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(result.FinalOutput, loopHardStopMessage) {
		t.Fatalf("FinalOutput = %q want hard stop message", result.FinalOutput)
	}
	if got := toolExecutions.Load(); got != 4 {
		t.Fatalf("tool executions = %d want 4", got)
	}
}

func TestAgentRunBlocksDeniedToolCallsWithGuardrails(t *testing.T) {
	var toolExecutions atomic.Int32
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name: "bash",
		Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) {
			toolExecutions.Add(1)
			return models.ToolResult{
				CallID:   "guardrail-call",
				ToolName: "bash",
				Status:   models.CallStatusCompleted,
				Content:  "should not run",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	provider := &scriptedStreamProvider{
		steps: []streamStep{
			{toolCalls: []models.ToolCall{{ID: "guardrail-call-1", Name: "bash", Arguments: map[string]any{"command": "rm -rf /"}}}},
			{check: func(t *testing.T, req llm.ChatRequest) {
				last := req.Messages[len(req.Messages)-1]
				if last.Role != models.RoleTool || last.ToolResult == nil {
					t.Fatalf("last message = %#v want tool result", last)
				}
				if last.ToolResult.Status != models.CallStatusFailed {
					t.Fatalf("tool result status = %q want failed", last.ToolResult.Status)
				}
				if !strings.Contains(last.Content, "Guardrail denied") {
					t.Fatalf("tool content = %q want guardrail denial", last.Content)
				}
			}, content: "Used fallback after guardrail block."},
		},
		t: t,
	}

	failClosed := true
	agent := New(AgentConfig{
		LLMProvider:         provider,
		Tools:               registry,
		MaxTurns:            4,
		GuardrailProvider:   guardrails.NewAllowlistProvider([]string{"web_search"}, nil),
		GuardrailFailClosed: &failClosed,
	})

	result, err := agent.Run(context.Background(), "session-guardrail-deny", []models.Message{
		{ID: "m1", SessionID: "session-guardrail-deny", Role: models.RoleHuman, Content: "Delete everything"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "Used fallback after guardrail block." {
		t.Fatalf("FinalOutput = %q", result.FinalOutput)
	}
	if got := toolExecutions.Load(); got != 0 {
		t.Fatalf("tool executions = %d want 0", got)
	}
}

func TestAgentRunFailsOpenWhenGuardrailProviderErrors(t *testing.T) {
	var toolExecutions atomic.Int32
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name: "bash",
		Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) {
			toolExecutions.Add(1)
			return models.ToolResult{
				CallID:   "guardrail-call",
				ToolName: "bash",
				Status:   models.CallStatusCompleted,
				Content:  "tool executed",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	provider := &scriptedStreamProvider{
		steps: []streamStep{
			{toolCalls: []models.ToolCall{{ID: "guardrail-call-1", Name: "bash", Arguments: map[string]any{"command": "pwd"}}}},
			{check: func(t *testing.T, req llm.ChatRequest) {
				last := req.Messages[len(req.Messages)-1]
				if last.Role != models.RoleTool || last.ToolResult == nil {
					t.Fatalf("last message = %#v want tool result", last)
				}
				if last.ToolResult.Status != models.CallStatusCompleted {
					t.Fatalf("tool result status = %q want completed", last.ToolResult.Status)
				}
			}, content: "Tool still ran."},
		},
		t: t,
	}

	failClosed := false
	agent := New(AgentConfig{
		LLMProvider:         provider,
		Tools:               registry,
		MaxTurns:            4,
		GuardrailProvider:   explodingGuardrailProvider{},
		GuardrailFailClosed: &failClosed,
	})

	result, err := agent.Run(context.Background(), "session-guardrail-fail-open", []models.Message{
		{ID: "m1", SessionID: "session-guardrail-fail-open", Role: models.RoleHuman, Content: "Run pwd"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "Tool still ran." {
		t.Fatalf("FinalOutput = %q", result.FinalOutput)
	}
	if got := toolExecutions.Load(); got != 1 {
		t.Fatalf("tool executions = %d want 1", got)
	}
}

type explodingGuardrailProvider struct{}

func (explodingGuardrailProvider) Name() string {
	return "exploding"
}

func (explodingGuardrailProvider) Evaluate(guardrails.Request) (guardrails.Decision, error) {
	return guardrails.Decision{}, errors.New("provider crashed")
}

func TestPatchDanglingToolCallsInsertsMissingToolResults(t *testing.T) {
	sessionID := "session-dangling"
	messages := []models.Message{
		{ID: "m1", SessionID: sessionID, Role: models.RoleHuman, Content: "Do the thing"},
		{
			ID:        "m2",
			SessionID: sessionID,
			Role:      models.RoleAI,
			Content:   "",
			ToolCalls: []models.ToolCall{
				{ID: "call-1", Name: "bash", Status: models.CallStatusPending},
			},
		},
		{
			ID:        "m3",
			SessionID: sessionID,
			Role:      models.RoleAI,
			Content:   "next step",
		},
	}

	patched := patchDanglingToolCalls(messages)
	if len(patched) != 4 {
		t.Fatalf("messages len=%d want 4", len(patched))
	}
	if patched[2].Role != models.RoleTool {
		t.Fatalf("patched message role=%q want tool", patched[2].Role)
	}
	if patched[2].ToolResult == nil {
		t.Fatal("expected synthetic tool result")
	}
	if patched[2].ToolResult.CallID != "call-1" {
		t.Fatalf("call id=%q want call-1", patched[2].ToolResult.CallID)
	}
	if patched[2].ToolResult.Status != models.CallStatusFailed {
		t.Fatalf("status=%q want failed", patched[2].ToolResult.Status)
	}
	if !strings.Contains(patched[2].Content, "[Tool call was interrupted and did not return a result.]") {
		t.Fatalf("content=%q", patched[2].Content)
	}
}

func TestPatchDanglingToolCallsSkipsCompletedToolResults(t *testing.T) {
	sessionID := "session-complete"
	messages := []models.Message{
		{
			ID:        "m1",
			SessionID: sessionID,
			Role:      models.RoleAI,
			ToolCalls: []models.ToolCall{
				{ID: "call-1", Name: "bash", Status: models.CallStatusCompleted},
			},
		},
		{
			ID:        "m2",
			SessionID: sessionID,
			Role:      models.RoleTool,
			Content:   "ok",
			ToolResult: &models.ToolResult{
				CallID:   "call-1",
				ToolName: "bash",
				Status:   models.CallStatusCompleted,
				Content:  "ok",
			},
		},
	}

	patched := patchDanglingToolCalls(messages)
	if len(patched) != len(messages) {
		t.Fatalf("messages len=%d want %d", len(patched), len(messages))
	}
}

func TestAgentRunPatchesDanglingToolCallsBeforeModelInvocation(t *testing.T) {
	provider := &scriptedStreamProvider{
		steps: []streamStep{
			{
				check: func(t *testing.T, req llm.ChatRequest) {
					if len(req.Messages) != 3 {
						t.Fatalf("messages len=%d want 3", len(req.Messages))
					}
					toolMsg := req.Messages[2]
					if toolMsg.Role != models.RoleTool {
						t.Fatalf("role=%q want tool", toolMsg.Role)
					}
					if toolMsg.ToolResult == nil {
						t.Fatal("expected synthetic tool result")
					}
					if toolMsg.ToolResult.CallID != "call-1" {
						t.Fatalf("call id=%q want call-1", toolMsg.ToolResult.CallID)
					}
				},
				content: "Recovered after patch.",
			},
		},
		t: t,
	}

	agent := New(AgentConfig{
		LLMProvider: provider,
		MaxTurns:    2,
	})

	result, err := agent.Run(context.Background(), "session-dangling-run", []models.Message{
		{ID: "m1", SessionID: "session-dangling-run", Role: models.RoleHuman, Content: "Continue"},
		{
			ID:        "m2",
			SessionID: "session-dangling-run",
			Role:      models.RoleAI,
			ToolCalls: []models.ToolCall{
				{ID: "call-1", Name: "bash", Status: models.CallStatusPending},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "Recovered after patch." {
		t.Fatalf("FinalOutput=%q", result.FinalOutput)
	}
}

func TestAgentRunNormalizesMalformedToolHistory(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name: "write_file",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"path"},
		},
		Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) {
			return models.ToolResult{}, nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	provider := &toolHistoryProvider{t: t}
	runAgent := New(AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    2,
	})

	result, err := runAgent.Run(context.Background(), "session_1", []models.Message{
		{ID: "m1", SessionID: "session_1", Role: models.RoleHuman, Content: "帮我生成一个小鱼游泳的页面"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := result.FinalOutput; got != "done" {
		t.Fatalf("FinalOutput = %q, want done", got)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
}

func TestToolMessageContentFormatsError(t *testing.T) {
	content := toolMessageContent(models.ToolResult{
		ToolName: "write_file",
		Error:    `missing required argument "path"`,
	})
	if !strings.Contains(content, "write_file") {
		t.Fatalf("formatted tool error = %q, want tool name", content)
	}
	if !strings.Contains(content, "Continue with available context") {
		t.Fatalf("formatted tool error = %q, want continuation hint", content)
	}
}

func TestRecoverableToolRetryPromptHandlesClarification(t *testing.T) {
	prompt := recoverableToolRetryPrompt([]models.Message{{
		ID:        "tool-1",
		SessionID: "session-1",
		Role:      models.RoleTool,
		Content:   "Error",
		ToolResult: &models.ToolResult{
			CallID:   "call-1",
			ToolName: "ask_clarification",
			Status:   models.CallStatusFailed,
			Error:    `missing required argument "question"`,
		},
	}})
	if !strings.Contains(prompt, "ask_clarification") {
		t.Fatalf("prompt=%q want ask_clarification guidance", prompt)
	}
	if !strings.Contains(prompt, "`question`") {
		t.Fatalf("prompt=%q want required question guidance", prompt)
	}
}

func TestAgentRunActivatesDeferredToolsViaToolSearch(t *testing.T) {
	var deferredExecutions atomic.Int32
	provider := &scriptedStreamProvider{
		steps: []streamStep{
			{
				check: func(t *testing.T, req llm.ChatRequest) {
					names := toolNames(req.Tools)
					if !slices.Contains(names, "tool_search") {
						t.Fatalf("tools=%v want tool_search", names)
					}
					if slices.Contains(names, "github.search_repos") {
						t.Fatalf("tools=%v should not include deferred tool before search", names)
					}
					if !strings.Contains(req.SystemPrompt, "github.search_repos") {
						t.Fatalf("system prompt missing deferred tools: %q", req.SystemPrompt)
					}
				},
				toolCalls: []models.ToolCall{{
					ID:        "call-search",
					Name:      "tool_search",
					Arguments: map[string]any{"query": "select:github.search_repos"},
				}},
			},
			{
				check: func(t *testing.T, req llm.ChatRequest) {
					names := toolNames(req.Tools)
					if !slices.Contains(names, "github.search_repos") {
						t.Fatalf("tools=%v want activated deferred tool", names)
					}
					last := req.Messages[len(req.Messages)-1]
					if last.Role != models.RoleTool || last.ToolResult == nil || !strings.Contains(last.Content, "github.search_repos") {
						t.Fatalf("last message=%#v", last)
					}
				},
				toolCalls: []models.ToolCall{{
					ID:        "call-github",
					Name:      "github.search_repos",
					Arguments: map[string]any{"query": "deerflow"},
				}},
			},
			{
				check: func(t *testing.T, req llm.ChatRequest) {
					if deferredExecutions.Load() != 1 {
						t.Fatalf("deferred executions=%d want 1", deferredExecutions.Load())
					}
				},
				content: "Deferred tool completed.",
			},
		},
		t: t,
	}

	runAgent := New(AgentConfig{
		LLMProvider: provider,
		DeferredTools: []models.Tool{{
			Name:        "github.search_repos",
			Description: "Search repositories",
			InputSchema: map[string]any{"type": "object"},
			Handler: func(_ context.Context, call models.ToolCall) (models.ToolResult, error) {
				deferredExecutions.Add(1)
				return models.ToolResult{
					CallID:   call.ID,
					ToolName: call.Name,
					Status:   models.CallStatusCompleted,
					Content:  "found deerflow",
				}, nil
			},
		}},
		MaxTurns: 4,
	})

	result, err := runAgent.Run(context.Background(), "session-deferred-tool", []models.Message{
		{ID: "m1", SessionID: "session-deferred-tool", Role: models.RoleHuman, Content: "Find repos"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "Deferred tool completed." {
		t.Fatalf("FinalOutput=%q", result.FinalOutput)
	}
}

func TestAgentRunContinuesAfterToolPanic(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name: "panic_tool",
		Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) {
			panic("exploded")
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	provider := &scriptedStreamProvider{
		steps: []streamStep{
			{toolCalls: []models.ToolCall{{ID: "panic-call-1", Name: "panic_tool"}}},
			{check: func(t *testing.T, req llm.ChatRequest) {
				last := req.Messages[len(req.Messages)-1]
				if last.Role != models.RoleTool || last.ToolResult == nil {
					t.Fatalf("last message=%#v", last)
				}
				if last.ToolResult.Status != models.CallStatusFailed {
					t.Fatalf("tool result status=%q want failed", last.ToolResult.Status)
				}
				if !strings.Contains(last.Content, `Error: Tool "panic_tool" panicked: exploded.`) {
					t.Fatalf("tool message content=%q", last.Content)
				}
			}, content: "Recovered after tool panic."},
		},
		t: t,
	}

	agent := New(AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    3,
	})

	result, err := agent.Run(context.Background(), "session-panic-tool", []models.Message{
		{ID: "m1", SessionID: "session-panic-tool", Role: models.RoleHuman, Content: "Use the tool"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "Recovered after tool panic." {
		t.Fatalf("FinalOutput=%q", result.FinalOutput)
	}
}

func TestAgentRunContinuesAfterMissingTool(t *testing.T) {
	provider := &scriptedStreamProvider{
		steps: []streamStep{
			{toolCalls: []models.ToolCall{{ID: "missing-call-1", Name: "missing_tool"}}},
			{check: func(t *testing.T, req llm.ChatRequest) {
				last := req.Messages[len(req.Messages)-1]
				if last.Role != models.RoleTool || last.ToolResult == nil {
					t.Fatalf("last message=%#v", last)
				}
				if last.ToolResult.Status != models.CallStatusFailed {
					t.Fatalf("tool result status=%q want failed", last.ToolResult.Status)
				}
				if !strings.Contains(last.Content, `Error: Tool "missing_tool" failed with errorString: tool "missing_tool" not found.`) {
					t.Fatalf("tool message content=%q", last.Content)
				}
			}, content: "Recovered after missing tool."},
		},
		t: t,
	}

	agent := New(AgentConfig{
		LLMProvider: provider,
		Tools:       tools.NewRegistry(),
		MaxTurns:    3,
	})

	result, err := agent.Run(context.Background(), "session-missing-tool", []models.Message{
		{ID: "m1", SessionID: "session-missing-tool", Role: models.RoleHuman, Content: "Use the missing tool"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "Recovered after missing tool." {
		t.Fatalf("FinalOutput=%q", result.FinalOutput)
	}
}

func TestAgentRunStopsAfterClarificationRequest(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name: "ask_clarification",
		Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) {
			return models.ToolResult{
				CallID:   "call-clarify",
				ToolName: "ask_clarification",
				Status:   models.CallStatusCompleted,
				Content:  "Which mode?\n1. Fast\n2. Safe",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	provider := &scriptedStreamProvider{
		steps: []streamStep{
			{
				toolCalls: []models.ToolCall{{
					ID:        "call-clarify",
					Name:      "ask_clarification",
					Arguments: map[string]any{"question": "Which mode?"},
				}},
			},
		},
		t: t,
	}

	runAgent := New(AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    3,
	})

	result, err := runAgent.Run(context.Background(), "session-clarify", []models.Message{
		{ID: "m1", SessionID: "session-clarify", Role: models.RoleHuman, Content: "Help me choose a mode"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "" {
		t.Fatalf("FinalOutput=%q want empty", result.FinalOutput)
	}
	if len(result.Messages) != 3 {
		t.Fatalf("messages len=%d want 3", len(result.Messages))
	}
	last := result.Messages[len(result.Messages)-1]
	if last.Role != models.RoleTool || last.ToolResult == nil {
		t.Fatalf("last message=%#v", last)
	}
	if last.Content != "❓ Which mode?" {
		t.Fatalf("tool message content=%q", last.Content)
	}
}

func TestClampMaxConcurrentSubagents(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{name: "default", input: 0, want: defaultMaxConcurrentSubagents},
		{name: "min", input: 1, want: minMaxConcurrentSubagents},
		{name: "within", input: 4, want: 4},
		{name: "max", input: 8, want: maxMaxConcurrentSubagents},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampMaxConcurrentSubagents(tt.input); got != tt.want {
				t.Fatalf("clampMaxConcurrentSubagents(%d)=%d want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestTruncateTaskToolCalls(t *testing.T) {
	calls := []models.ToolCall{
		{ID: "task-1", Name: "task"},
		{ID: "bash-1", Name: "bash"},
		{ID: "task-2", Name: "task"},
		{ID: "task-3", Name: "task"},
		{ID: "task-4", Name: "task"},
	}

	got := truncateTaskToolCalls(calls, 2)
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	if got[0].ID != "task-1" || got[1].ID != "bash-1" || got[2].ID != "task-2" {
		t.Fatalf("got ids=%v want [task-1 bash-1 task-2]", []string{got[0].ID, got[1].ID, got[2].ID})
	}
}

func TestAgentRunTruncatesExcessTaskToolCalls(t *testing.T) {
	var taskExecutions atomic.Int32
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name: "task",
		Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) {
			taskExecutions.Add(1)
			return models.ToolResult{
				CallID:   "task-call",
				ToolName: "task",
				Status:   models.CallStatusCompleted,
				Content:  "task ok",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register task tool: %v", err)
	}

	provider := &scriptedStreamProvider{
		steps: []streamStep{
			{toolCalls: []models.ToolCall{
				{ID: "task-1", Name: "task", Arguments: map[string]any{"prompt": "one"}},
				{ID: "task-2", Name: "task", Arguments: map[string]any{"prompt": "two"}},
				{ID: "task-3", Name: "task", Arguments: map[string]any{"prompt": "three"}},
				{ID: "task-4", Name: "task", Arguments: map[string]any{"prompt": "four"}},
			}},
			{check: func(t *testing.T, req llm.ChatRequest) {
				lastAssistant := req.Messages[1]
				if len(lastAssistant.ToolCalls) != 2 {
					t.Fatalf("assistant tool calls=%d want 2", len(lastAssistant.ToolCalls))
				}
				if lastAssistant.ToolCalls[0].ID != "task-1" || lastAssistant.ToolCalls[1].ID != "task-2" {
					t.Fatalf("assistant tool calls=%v", lastAssistant.ToolCalls)
				}
			}, content: "Done."},
		},
		t: t,
	}

	agent := New(AgentConfig{
		LLMProvider:            provider,
		Tools:                  registry,
		MaxTurns:               3,
		MaxConcurrentSubagents: 2,
	})

	result, err := agent.Run(context.Background(), "session-task-limit", []models.Message{
		{ID: "m1", SessionID: "session-task-limit", Role: models.RoleHuman, Content: "Parallelize this"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "Done." {
		t.Fatalf("FinalOutput=%q", result.FinalOutput)
	}
	if got := taskExecutions.Load(); got != 2 {
		t.Fatalf("task executions=%d want 2", got)
	}
}

func TestAgentRunExecutesTaskToolCallsInParallel(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name: "task",
		Handler: func(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
			time.Sleep(80 * time.Millisecond)
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusCompleted,
				Content:  call.ID + " ok",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register task tool: %v", err)
	}

	provider := &scriptedStreamProvider{
		steps: []streamStep{
			{toolCalls: []models.ToolCall{
				{ID: "task-1", Name: "task", Arguments: map[string]any{"prompt": "one"}},
				{ID: "task-2", Name: "task", Arguments: map[string]any{"prompt": "two"}},
				{ID: "task-3", Name: "task", Arguments: map[string]any{"prompt": "three"}},
			}},
			{check: func(t *testing.T, req llm.ChatRequest) {
				if got := len(req.Messages); got != 5 {
					t.Fatalf("messages=%d want 5", got)
				}
				for i, want := range []string{"task-1", "task-2", "task-3"} {
					msg := req.Messages[i+2]
					if msg.ToolResult == nil || msg.ToolResult.CallID != want {
						t.Fatalf("tool result %d call_id=%v want %s", i, msg.ToolResult, want)
					}
				}
			}, content: "Done."},
		},
		t: t,
	}

	agent := New(AgentConfig{
		LLMProvider:            provider,
		Tools:                  registry,
		MaxTurns:               3,
		MaxConcurrentSubagents: 3,
	})

	started := time.Now()
	result, err := agent.Run(context.Background(), "session-task-parallel", []models.Message{
		{ID: "m1", SessionID: "session-task-parallel", Role: models.RoleHuman, Content: "Parallelize this"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "Done." {
		t.Fatalf("FinalOutput=%q", result.FinalOutput)
	}

	if elapsed := time.Since(started); elapsed >= 220*time.Millisecond {
		t.Fatalf("elapsed=%s want parallel execution under 220ms", elapsed)
	}
}

func TestSelectSubagentToolsWithDenylist(t *testing.T) {
	all := []models.Tool{
		{Name: "bash", Groups: []string{"bash"}},
		{Name: "read_file", Groups: []string{"file_ops"}},
		{Name: "web_search", Groups: []string{"web"}},
		{Name: "task", Groups: []string{"agent"}},
		{Name: "ask_clarification", Groups: []string{"agent"}},
		{Name: "present_files", Groups: []string{"artifact"}},
	}

	got := selectSubagentToolsWithDenylist(all, nil, []string{"ask_clarification", "present_files"})
	if names := toolNames(got); !slices.Equal(names, []string{"bash", "read_file", "web_search"}) {
		t.Fatalf("names=%v want [bash read_file web_search]", names)
	}

	got = selectSubagentToolsWithDenylist(all, []string{"bash", "file_ops", "task"}, []string{"bash"})
	if names := toolNames(got); !slices.Equal(names, []string{"read_file"}) {
		t.Fatalf("filtered names=%v want [read_file]", names)
	}
}

func TestRunEmitsFinalAssistantMetadataAfterNormalization(t *testing.T) {
	provider := &scriptedStreamProvider{
		t: t,
		steps: []streamStep{
			{content: "<think>internal reasoning</think>\n\nVisible answer"},
		},
	}
	agent := New(AgentConfig{
		LLMProvider: provider,
		Model:       "test-model",
		MaxTurns:    1,
	})

	done := make(chan []AgentEvent, 1)
	go func() {
		var events []AgentEvent
		for evt := range agent.Events() {
			events = append(events, evt)
			if evt.Type == AgentEventEnd || evt.Type == AgentEventError {
				done <- events
				return
			}
		}
		done <- events
	}()

	result, err := agent.Run(context.Background(), "session-1", []models.Message{
		{ID: "m1", SessionID: "session-1", Role: models.RoleHuman, Content: "hello"},
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.FinalOutput != "Visible answer" {
		t.Fatalf("final output=%q want Visible answer", result.FinalOutput)
	}

	events := <-done
	for _, evt := range events {
		if evt.Type != AgentEventEnd {
			continue
		}
		if evt.Text != "Visible answer" {
			t.Fatalf("end text=%q want Visible answer", evt.Text)
		}
		if got := evt.Metadata["additional_kwargs"]; !strings.Contains(got, `"reasoning_content":"internal reasoning"`) {
			t.Fatalf("metadata=%q want reasoning_content", got)
		}
		return
	}

	t.Fatal("missing AgentEventEnd")
}

func TestRunPreservesReasoningOnlyAssistantMessages(t *testing.T) {
	provider := &scriptedStreamProvider{
		t: t,
		steps: []streamStep{
			{content: "<think>internal reasoning</think>"},
		},
	}
	agent := New(AgentConfig{
		LLMProvider: provider,
		Model:       "test-model",
		MaxTurns:    1,
	})

	done := make(chan []AgentEvent, 1)
	go func() {
		var events []AgentEvent
		for evt := range agent.Events() {
			events = append(events, evt)
			if evt.Type == AgentEventEnd || evt.Type == AgentEventError {
				done <- events
				return
			}
		}
		done <- events
	}()

	result, err := agent.Run(context.Background(), "session-1", []models.Message{
		{ID: "m1", SessionID: "session-1", Role: models.RoleHuman, Content: "hello"},
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.FinalOutput != "" {
		t.Fatalf("final output=%q want empty", result.FinalOutput)
	}
	if len(result.Messages) == 0 {
		t.Fatal("expected reasoning-only assistant message to be preserved")
	}
	last := result.Messages[len(result.Messages)-1]
	if last.Content != "" {
		t.Fatalf("content=%q want empty", last.Content)
	}
	if got := last.Metadata["additional_kwargs"]; !strings.Contains(got, `"reasoning_content":"internal reasoning"`) {
		t.Fatalf("metadata=%q want reasoning_content", got)
	}

	events := <-done
	for _, evt := range events {
		if evt.Type != AgentEventEnd {
			continue
		}
		if evt.Text != "" {
			t.Fatalf("end text=%q want empty", evt.Text)
		}
		if got := evt.Metadata["additional_kwargs"]; !strings.Contains(got, `"reasoning_content":"internal reasoning"`) {
			t.Fatalf("metadata=%q want reasoning_content", got)
		}
		return
	}

	t.Fatal("missing AgentEventEnd")
}

type timeoutProvider struct{}

type chatOnlyProvider struct {
	t    *testing.T
	chat func(llm.ChatRequest) llm.ChatResponse
}

func (p chatOnlyProvider) PrefersStructuredToolCalls() bool { return true }

func (p chatOnlyProvider) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	return p.chat(req), nil
}

func (p chatOnlyProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	if p.t != nil {
		p.t.Fatal("Stream() should not be used for tool turns")
	}
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

type streamOnlyToolProvider struct {
	t           *testing.T
	streamCalls int
}

func (p *streamOnlyToolProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	if p.t != nil {
		p.t.Fatal("Chat() should not be used for non-structured providers")
	}
	return llm.ChatResponse{}, nil
}

func (p *streamOnlyToolProvider) Stream(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	p.streamCalls++
	ch := make(chan llm.StreamChunk, 1)
	go func() {
		defer close(ch)
		switch p.streamCalls {
		case 1:
			ch <- llm.StreamChunk{
				Message: &models.Message{
					Role:    models.RoleAI,
					Content: "先调用工具。",
					ToolCalls: []models.ToolCall{{
						ID:        "call-1",
						Name:      "echo_tool",
						Arguments: map[string]any{"value": "ok"},
						Status:    models.CallStatusPending,
					}},
				},
				ToolCalls: []models.ToolCall{{
					ID:        "call-1",
					Name:      "echo_tool",
					Arguments: map[string]any{"value": "ok"},
					Status:    models.CallStatusPending,
				}},
				Stop: "tool_calls",
				Done: true,
			}
		case 2:
			if len(req.Messages) < 3 {
				p.t.Fatalf("second stream request messages=%d want>=3", len(req.Messages))
			}
			if req.Messages[2].ToolResult == nil || req.Messages[2].ToolResult.Content != "ok" {
				p.t.Fatalf("tool result history=%#v", req.Messages[2].ToolResult)
			}
			ch <- llm.StreamChunk{
				Message: &models.Message{
					Role:    models.RoleAI,
					Content: "done",
				},
				Stop: "stop",
				Done: true,
			}
		default:
			p.t.Fatalf("unexpected Stream call %d", p.streamCalls)
		}
	}()
	return ch, nil
}

type streamThenChatFallbackProvider struct {
	t           *testing.T
	streamCalls atomic.Int32
	chatCalls   atomic.Int32
}

func (p *streamThenChatFallbackProvider) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	callIndex := int(p.chatCalls.Add(1))
	if callIndex != 1 {
		p.t.Fatalf("unexpected Chat call %d", callIndex)
	}
	if len(req.Tools) == 0 {
		p.t.Fatal("Chat() fallback should only be used for tool turns")
	}
	return llm.ChatResponse{
		Message: models.Message{
			Role:    models.RoleAI,
			Content: "先调用工具。",
			ToolCalls: []models.ToolCall{{
				ID:        "call-1",
				Name:      "echo_tool",
				Arguments: map[string]any{"value": "ok"},
				Status:    models.CallStatusPending,
			}},
		},
		Stop: "tool_calls",
	}, nil
}

func (p *streamThenChatFallbackProvider) Stream(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	callIndex := int(p.streamCalls.Add(1))
	ch := make(chan llm.StreamChunk, 2)
	go func() {
		defer close(ch)
		switch callIndex {
		case 1:
			ch <- llm.StreamChunk{Delta: "先调用工具。"}
			ch <- llm.StreamChunk{Err: errors.New("stream truncated"), Done: true}
		case 2:
			if len(req.Messages) < 3 {
				p.t.Fatalf("second stream request messages=%d want>=3", len(req.Messages))
			}
			if req.Messages[2].ToolResult == nil || req.Messages[2].ToolResult.Content != "ok" {
				p.t.Fatalf("tool result history=%#v", req.Messages[2].ToolResult)
			}
			ch <- llm.StreamChunk{
				Message: &models.Message{
					Role:    models.RoleAI,
					Content: "done",
				},
				Stop: "stop",
				Done: true,
			}
		default:
			p.t.Fatalf("unexpected Stream call %d", callIndex)
		}
	}()
	return ch, nil
}

type structuredNoToolProvider struct {
	t *testing.T
}

func (p structuredNoToolProvider) PrefersStructuredToolCalls() bool { return true }

func (p structuredNoToolProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	if p.t != nil {
		p.t.Fatal("Chat() should not be used when no tools are available")
	}
	return llm.ChatResponse{}, nil
}

func TestAgentRunRewritesSkillAliasToolCallsToReadFile(t *testing.T) {
	registry := tools.NewRegistry()
	var gotCall models.ToolCall
	if err := registry.Register(models.Tool{
		Name: "read_file",
		Handler: func(_ context.Context, call models.ToolCall) (models.ToolResult, error) {
			gotCall = call
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Content:  "# Frontend Skill",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register read_file: %v", err)
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
						Name: "frontend-design",
						Arguments: map[string]any{
							"description": "Load the frontend skill",
						},
					}},
				},
				Stop: "tool_calls",
			}
		case 2:
			if len(req.Messages) < 3 {
				t.Fatalf("second chat request messages=%d want>=3", len(req.Messages))
			}
			if req.Messages[2].ToolResult == nil || req.Messages[2].ToolResult.ToolName != "read_file" {
				t.Fatalf("tool result history=%#v", req.Messages[2].ToolResult)
			}
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

	agent := New(AgentConfig{
		LLMProvider:  provider,
		Tools:        registry,
		SystemPrompt: "test",
		MaxTurns:     4,
	})
	ctx := tools.WithRuntimeContext(context.Background(), map[string]any{
		"skill_paths": map[string]any{
			"frontend-design": "/mnt/skills/public/frontend-design/SKILL.md",
		},
	})
	result, err := agent.Run(ctx, "session-1", []models.Message{{
		ID:        "msg-1",
		SessionID: "session-1",
		Role:      models.RoleHuman,
		Content:   "build a page",
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gotCall.Name != "read_file" {
		t.Fatalf("tool name=%q want read_file", gotCall.Name)
	}
	if got, _ := gotCall.Arguments["path"].(string); got != "/mnt/skills/public/frontend-design/SKILL.md" {
		t.Fatalf("tool path=%q", got)
	}
	if got, _ := gotCall.Arguments["description"].(string); got != "Load the frontend skill" {
		t.Fatalf("tool description=%q", got)
	}
	if len(result.Messages) == 0 {
		t.Fatal("result messages should not be empty")
	}
}

func TestAgentRunStopsAfterSuccessfulPresentFiles(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name: "present_files",
		Handler: func(_ context.Context, call models.ToolCall) (models.ToolResult, error) {
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusCompleted,
				Content:  "Registered file /mnt/user-data/outputs/result.html",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register present_files: %v", err)
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
						Name: "present_files",
						Arguments: map[string]any{
							"filepaths": []string{"/mnt/user-data/outputs/result.html"},
						},
					}},
				},
				Stop: "tool_calls",
			}
		case 2:
			if len(req.Messages) < 3 {
				t.Fatalf("messages=%d want at least 3", len(req.Messages))
			}
			last := req.Messages[len(req.Messages)-1]
			if last.Role != models.RoleTool || last.ToolResult == nil || last.ToolResult.ToolName != "present_files" {
				t.Fatalf("last message=%#v want present_files tool result", last)
			}
			return llm.ChatResponse{
				Model: "test-model",
				Message: models.Message{
					Role:    models.RoleAI,
					Content: "页面已经准备好了。",
				},
				Stop: "stop",
			}
		}
		t.Fatalf("unexpected Chat call %d", chatCalls)
		return llm.ChatResponse{}
	}}

	agent := New(AgentConfig{
		LLMProvider:  provider,
		Tools:        registry,
		SystemPrompt: "test",
		MaxTurns:     4,
	})
	result, err := agent.Run(context.Background(), "session-present-stop", []models.Message{{
		ID:        "msg-1",
		SessionID: "session-present-stop",
		Role:      models.RoleHuman,
		Content:   "present it",
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if chatCalls != 2 {
		t.Fatalf("chat calls=%d want 2", chatCalls)
	}
	if len(result.Messages) < 2 {
		t.Fatalf("messages=%d want at least 2", len(result.Messages))
	}
	last := result.Messages[len(result.Messages)-1]
	if last.Role != models.RoleAI || last.Content != "页面已经准备好了。" {
		t.Fatalf("last message=%#v", last)
	}
}

func TestAgentRunCompletesArtifactWorkflowWithFinalAssistantReply(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", dataRoot)

	threadID := "thread-artifact-workflow"
	uploadsDir := filepath.Join(dataRoot, "threads", threadID, "user-data", "uploads")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	briefPath := filepath.Join(uploadsDir, "brief.md")
	if err := os.WriteFile(briefPath, []byte("# Brief\n\nBuild a fish page.\n"), 0o644); err != nil {
		t.Fatalf("write brief: %v", err)
	}

	presentRegistry := tools.NewPresentFileRegistry()
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.ReadFileTool()); err != nil {
		t.Fatalf("register read_file: %v", err)
	}
	if err := registry.Register(builtin.WriteFileTool()); err != nil {
		t.Fatalf("register write_file: %v", err)
	}
	if err := registry.Register(tools.PresentFilesTool(presentRegistry)); err != nil {
		t.Fatalf("register present_files: %v", err)
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
						ID:   "call-read-1",
						Name: "read_file",
						Arguments: map[string]any{
							"description": "Load the uploaded brief",
							"path":        "/mnt/user-data/uploads/brief.md",
						},
					}},
				},
				Stop: "tool_calls",
			}
		case 2:
			last := req.Messages[len(req.Messages)-1]
			if last.ToolResult == nil || last.ToolResult.ToolName != "read_file" {
				t.Fatalf("last message=%#v want read_file tool result", last)
			}
			if !strings.Contains(last.ToolResult.Content, "Build a fish page") {
				t.Fatalf("read_file result=%q", last.ToolResult.Content)
			}
			return llm.ChatResponse{
				Model: "test-model",
				Message: models.Message{
					Role: models.RoleAI,
					ToolCalls: []models.ToolCall{{
						ID:   "call-write-1",
						Name: "write_file",
						Arguments: map[string]any{
							"description": "Write the final fish page",
							"path":        "/mnt/user-data/outputs/index.html",
							"content":     "<!doctype html><title>Fish</title><p>fish page</p>",
						},
					}},
				},
				Stop: "tool_calls",
			}
		case 3:
			last := req.Messages[len(req.Messages)-1]
			if last.ToolResult == nil || last.ToolResult.ToolName != "write_file" {
				t.Fatalf("last message=%#v want write_file tool result", last)
			}
			target := filepath.Join(dataRoot, "threads", threadID, "user-data", "outputs", "index.html")
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("read output file: %v", err)
			}
			if string(data) != "<!doctype html><title>Fish</title><p>fish page</p>" {
				t.Fatalf("output file=%q", string(data))
			}
			return llm.ChatResponse{
				Model: "test-model",
				Message: models.Message{
					Role: models.RoleAI,
					ToolCalls: []models.ToolCall{{
						ID:   "call-present-1",
						Name: "present_files",
						Arguments: map[string]any{
							"description": "Fish page artifact",
							"filepaths":   []any{"/mnt/user-data/outputs/index.html"},
						},
					}},
				},
				Stop: "tool_calls",
			}
		case 4:
			last := req.Messages[len(req.Messages)-1]
			if last.ToolResult == nil || last.ToolResult.ToolName != "present_files" {
				t.Fatalf("last message=%#v want present_files tool result", last)
			}
			return llm.ChatResponse{
				Model: "test-model",
				Message: models.Message{
					Role:    models.RoleAI,
					Content: "已为你创建页面，并完成交付。",
				},
				Stop: "stop",
			}
		default:
			t.Fatalf("unexpected Chat call %d", chatCalls)
			return llm.ChatResponse{}
		}
	}}

	agent := New(AgentConfig{
		LLMProvider:  provider,
		Tools:        registry,
		SystemPrompt: "test",
		MaxTurns:     6,
	})
	result, err := agent.Run(tools.WithThreadID(context.Background(), threadID), threadID, []models.Message{{
		ID:        "msg-1",
		SessionID: threadID,
		Role:      models.RoleHuman,
		Content:   "帮我生成一个小鱼游泳的页面",
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if chatCalls != 4 {
		t.Fatalf("chat calls=%d want 4", chatCalls)
	}
	if result.FinalOutput != "已为你创建页面，并完成交付。" {
		t.Fatalf("final output=%q", result.FinalOutput)
	}
	files := presentRegistry.List()
	if len(files) != 1 {
		t.Fatalf("presented files=%d want 1", len(files))
	}
	if files[0].Path != "/mnt/user-data/outputs/index.html" {
		t.Fatalf("presented path=%q", files[0].Path)
	}
}

func (p structuredNoToolProvider) Stream(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	if len(req.Tools) != 0 {
		p.t.Fatalf("tools len=%d want 0", len(req.Tools))
	}
	ch := make(chan llm.StreamChunk, 1)
	go func() {
		defer close(ch)
		ch <- llm.StreamChunk{
			Message: &models.Message{
				Role:    models.RoleAI,
				Content: "done",
			},
			Stop: "stop",
			Done: true,
		}
	}()
	return ch, nil
}

func (timeoutProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}

func (timeoutProvider) Stream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk, 1)
	go func() {
		defer close(ch)
		<-ctx.Done()
		ch <- llm.StreamChunk{Err: ctx.Err(), Done: true}
	}()
	return ch, nil
}

type streamStep struct {
	content        string
	toolCalls      []models.ToolCall
	finalToolCalls []models.ToolCall
	check          func(*testing.T, llm.ChatRequest)
}

type scriptedStreamProvider struct {
	t     *testing.T
	steps []streamStep
	index atomic.Int32
}

func (p *scriptedStreamProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}

func (p *scriptedStreamProvider) Stream(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	stepIndex := int(p.index.Add(1)) - 1
	if stepIndex >= len(p.steps) {
		p.t.Fatalf("unexpected Stream() call %d", stepIndex+1)
	}
	step := p.steps[stepIndex]
	if step.check != nil {
		step.check(p.t, req)
	}
	ch := make(chan llm.StreamChunk, 1)
	go func() {
		defer close(ch)
		ch <- llm.StreamChunk{
			Message: &models.Message{
				Role:      models.RoleAI,
				Content:   step.content,
				ToolCalls: firstNonEmptyToolCalls(step.finalToolCalls, step.toolCalls),
			},
			ToolCalls: step.toolCalls,
			Stop:      "stop",
			Done:      true,
		}
	}()
	return ch, nil
}

func firstNonEmptyToolCalls(primary, fallback []models.ToolCall) []models.ToolCall {
	if len(primary) > 0 {
		return primary
	}
	return fallback
}

func toolNames(items []models.Tool) []string {
	names := make([]string, 0, len(items))
	for _, tool := range items {
		names = append(names, tool.Name)
	}
	return names
}

type toolHistoryProvider struct {
	t     *testing.T
	calls int
}

func (p *toolHistoryProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}

func (p *toolHistoryProvider) Stream(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	p.calls++
	ch := make(chan llm.StreamChunk, 1)

	go func() {
		defer close(ch)
		switch p.calls {
		case 1:
			ch <- llm.StreamChunk{
				Done: true,
				Message: &models.Message{
					ToolCalls: []models.ToolCall{
						{
							ID:        "call_valid",
							Name:      "write_file",
							Arguments: map[string]any{"content": "<html></html>"},
							Status:    models.CallStatusPending,
						},
						{
							ID:     "",
							Name:   "",
							Status: models.CallStatusPending,
						},
					},
				},
			}
		case 2:
			if len(req.Messages) != 3 {
				p.t.Errorf("second request messages = %d, want 3", len(req.Messages))
			}
			if len(req.Messages) >= 2 {
				assistant := req.Messages[1]
				if len(assistant.ToolCalls) != 1 {
					p.t.Errorf("assistant tool calls = %d, want 1", len(assistant.ToolCalls))
				}
				if len(assistant.ToolCalls) == 1 {
					call := assistant.ToolCalls[0]
					if call.ID != "call_valid" {
						p.t.Errorf("assistant tool call id = %q, want call_valid", call.ID)
					}
					if call.Name != "write_file" {
						p.t.Errorf("assistant tool call name = %q, want write_file", call.Name)
					}
				}
			}
			if len(req.Messages) >= 3 {
				toolMsg := req.Messages[2]
				if toolMsg.ToolResult == nil {
					p.t.Fatalf("tool result missing from history")
				}
				if toolMsg.ToolResult.CallID != "call_valid" {
					p.t.Errorf("tool result call id = %q, want call_valid", toolMsg.ToolResult.CallID)
				}
				if toolMsg.ToolResult.ToolName != "write_file" {
					p.t.Errorf("tool result tool name = %q, want write_file", toolMsg.ToolResult.ToolName)
				}
				if !strings.Contains(toolMsg.ToolResult.Error, `missing required argument "path"`) {
					p.t.Errorf("tool result error = %q, want missing path", toolMsg.ToolResult.Error)
				}
			}
			ch <- llm.StreamChunk{
				Done: true,
				Message: &models.Message{
					Content: "done",
				},
			}
		default:
			p.t.Fatalf("unexpected stream call %d", p.calls)
		}
	}()

	return ch, nil
}
