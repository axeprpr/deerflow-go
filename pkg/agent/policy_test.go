package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestDefaultLoopPolicyWarnsThenHardStops(t *testing.T) {
	policy := resolveRunPolicy(nil)
	state := newToolLoopState()
	calls := []models.ToolCall{{ID: "1", Name: "read_file", Arguments: map[string]any{"path": "/tmp/a.txt"}}}

	for i := 0; i < 2; i++ {
		decision := policy.Loop.Evaluate(state, calls)
		if decision.Warning != "" || decision.HardStop {
			t.Fatalf("iteration %d decision = %+v, want no action yet", i, decision)
		}
	}

	decision := policy.Loop.Evaluate(state, calls)
	if decision.Warning != loopWarningMessage || decision.HardStop {
		t.Fatalf("warning decision = %+v", decision)
	}

	decision = policy.Loop.Evaluate(state, calls)
	if decision.Warning != "" || decision.HardStop {
		t.Fatalf("post-warning decision = %+v, want no repeated warning yet", decision)
	}

	decision = policy.Loop.Evaluate(state, calls)
	if !decision.HardStop {
		t.Fatalf("hard-stop decision = %+v, want hard stop", decision)
	}
}

func TestDefaultRetryPolicyUsesRecoverableToolPrompt(t *testing.T) {
	policy := resolveRunPolicy(nil)
	prompt := policy.Retry.RecoverableToolRetryPrompt([]models.Message{{
		Role: models.RoleTool,
		ToolResult: &models.ToolResult{
			ToolName: "write_file",
			Status:   models.CallStatusFailed,
			Error:    "missing required argument: content",
		},
	}})
	if prompt == "" {
		t.Fatal("prompt is empty")
	}
}

func TestDefaultToolExecutionPolicyPausesAfterAskClarification(t *testing.T) {
	policy := resolveRunPolicy(nil)
	if !policy.ToolExec.ShouldPauseAfterToolCall(
		models.ToolCall{Name: "ask_clarification"},
		models.ToolResult{Status: models.CallStatusCompleted},
	) {
		t.Fatal("expected ask_clarification to pause")
	}
}

func TestDefaultToolTurnRecoveryPolicyUsesChatFallback(t *testing.T) {
	policy := resolveRunPolicy(nil)
	events := make([]AgentEvent, 0, 2)
	result, recovered, err := policy.Recovery.Recover(
		t.Context(),
		toolRecoveryLLMProvider{
			response: llm.ChatResponse{
				Message: models.Message{
					Content: "hello world",
					ToolCalls: []models.ToolCall{{
						ID:   "call-1",
						Name: "read_file",
					}},
				},
				Usage: llm.Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3},
				Stop:  "tool_calls",
			},
		},
		llm.ChatRequest{Tools: []models.Tool{{Name: "read_file"}}},
		ToolTurnRecoveryState{MessageID: "ai-1", PartialText: "", HasPartialToolCalls: true},
		assertiveError("stream failed"),
		func(evt AgentEvent) { events = append(events, evt) },
	)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if !recovered {
		t.Fatal("recovered = false")
	}
	if result.Text != "hello world" || len(result.ToolCalls) != 1 || result.StopReason != "tool_calls" {
		t.Fatalf("result = %+v", result)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
}

func TestDefaultTaskProgressPolicyInjectsReminderAfterToolRounds(t *testing.T) {
	policy := resolveRunPolicy(nil)
	state := newTaskProgressState()
	for i := 0; i < defaultTodoReminderThreshold; i++ {
		policy.Task.ObserveToolCalls(state, []models.ToolCall{{Name: "read_file"}})
	}
	reminder := policy.Task.Reminder(state, []models.Message{
		{Role: models.RoleAI, ToolCalls: []models.ToolCall{{Name: "read_file"}}},
		{Role: models.RoleTool, Content: "ok"},
		{Role: models.RoleAI, ToolCalls: []models.ToolCall{{Name: "write_file"}}},
		{Role: models.RoleTool, Content: "ok"},
		{Role: models.RoleAI, ToolCalls: []models.ToolCall{{Name: "ls"}}},
	})
	if reminder == "" {
		t.Fatal("Reminder() = empty, want reminder")
	}
	if !strings.Contains(reminder, "expected_outputs") {
		t.Fatalf("Reminder() = %q, want expected_outputs guidance", reminder)
	}
}

func TestDefaultTaskProgressPolicyResetsAfterWriteTodos(t *testing.T) {
	policy := resolveRunPolicy(nil)
	state := newTaskProgressState()
	policy.Task.ObserveToolCalls(state, []models.ToolCall{{Name: "read_file"}})
	policy.Task.ObserveToolCalls(state, []models.ToolCall{{
		Name:      "write_todos",
		Arguments: map[string]any{"todos": []any{map[string]any{"content": "draft", "status": "in_progress"}}},
	}})
	if state.RoundsSinceUpdate != 0 {
		t.Fatalf("RoundsSinceUpdate = %d, want 0", state.RoundsSinceUpdate)
	}
}

func TestDefaultTaskProgressPolicyRequiresMaterialTodoUpdate(t *testing.T) {
	policy := resolveRunPolicy(nil)
	state := newTaskProgressState()

	call := models.ToolCall{
		Name:      "write_todos",
		Arguments: map[string]any{"todos": []any{map[string]any{"content": "draft", "status": "in_progress"}}},
	}
	policy.Task.ObserveToolCalls(state, []models.ToolCall{call})
	if state.RoundsSinceUpdate != 0 {
		t.Fatalf("RoundsSinceUpdate after first update = %d, want 0", state.RoundsSinceUpdate)
	}
	firstHash := state.LastTodoHash
	if firstHash == "" {
		t.Fatal("LastTodoHash is empty after first update")
	}

	policy.Task.ObserveToolCalls(state, []models.ToolCall{call})
	if state.RoundsSinceUpdate != 1 {
		t.Fatalf("RoundsSinceUpdate after repeated update = %d, want 1", state.RoundsSinceUpdate)
	}
	if state.LastTodoHash != firstHash {
		t.Fatalf("LastTodoHash changed on repeated update: %q -> %q", firstHash, state.LastTodoHash)
	}

	policy.Task.ObserveToolCalls(state, []models.ToolCall{{
		Name: "write_todos",
		Arguments: map[string]any{
			"todos": []any{map[string]any{"content": "draft", "status": "completed"}},
		},
	}})
	if state.RoundsSinceUpdate != 0 {
		t.Fatalf("RoundsSinceUpdate after changed update = %d, want 0", state.RoundsSinceUpdate)
	}
	if state.LastTodoHash == firstHash {
		t.Fatalf("LastTodoHash did not change after material update: %q", state.LastTodoHash)
	}
}

type toolRecoveryLLMProvider struct {
	response llm.ChatResponse
}

func (p toolRecoveryLLMProvider) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	return p.response, nil
}

func (p toolRecoveryLLMProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

type assertiveError string

func (e assertiveError) Error() string { return string(e) }
