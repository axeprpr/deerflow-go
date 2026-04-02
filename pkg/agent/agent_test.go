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
	if !strings.Contains(content, "/mnt/user-data/outputs/report.md") {
		t.Fatalf("content=%q missing normalized output path", content)
	}

	files := presentRegistry.List()
	if len(files) != 1 {
		t.Fatalf("registered files=%d want=1", len(files))
	}
	if files[0].Path != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("registered path=%q want=%q", files[0].Path, "/mnt/user-data/outputs/report.md")
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
	if last.Content != "Which mode?\n1. Fast\n2. Safe" {
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
