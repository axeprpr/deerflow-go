package agent

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
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
	if prompt == "custom system prompt" {
		t.Error("BuildSystemPrompt should include runtime instructions in addition to the base prompt")
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
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
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
	if patched[2].Content != "[Tool call was interrupted and did not return a result.]" {
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

type timeoutProvider struct{}

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
	content   string
	toolCalls []models.ToolCall
	check     func(*testing.T, llm.ChatRequest)
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
				ToolCalls: step.toolCalls,
			},
			ToolCalls: step.toolCalls,
			Stop:      "stop",
			Done:      true,
		}
	}()
	return ch, nil
}

func toolNames(items []models.Tool) []string {
	names := make([]string, 0, len(items))
	for _, tool := range items {
		names = append(names, tool.Name)
	}
	return names
}
