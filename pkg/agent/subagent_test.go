package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestSubagentTaskEventFromAgentEvent_EmitsStructuredMessageSnapshots(t *testing.T) {
	task := &subagent.Task{
		ID:          "task-1",
		Description: "inspect repo",
	}
	state := &subagentStreamState{}

	toolEvent, ok := subagentTaskEventFromAgentEvent(task, AgentEvent{
		Type:      AgentEventToolCall,
		MessageID: "ai-1",
		ToolCall: &models.ToolCall{
			ID:        "call-1",
			Name:      "bash",
			Arguments: map[string]any{"command": "pwd"},
		},
	}, state)
	if !ok {
		t.Fatal("tool call event was ignored")
	}
	if toolEvent.Type != "task_running" {
		t.Fatalf("type=%q want task_running", toolEvent.Type)
	}
	if toolEvent.MessageIndex != 1 || toolEvent.TotalMessages != 1 {
		t.Fatalf("message counters=(%d,%d) want (1,1)", toolEvent.MessageIndex, toolEvent.TotalMessages)
	}

	message, ok := toolEvent.Message.(map[string]any)
	if !ok {
		t.Fatalf("message type=%T want map[string]any", toolEvent.Message)
	}
	if got := trimStringValue(message["type"]); got != "ai" {
		t.Fatalf("message.type=%q want ai", got)
	}
	toolCalls, ok := message["tool_calls"].([]map[string]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("tool_calls=%#v want single call", message["tool_calls"])
	}
	if got := trimStringValue(toolCalls[0]["name"]); got != "bash" {
		t.Fatalf("tool name=%q want bash", got)
	}
	args, ok := toolCalls[0]["args"].(map[string]any)
	if !ok || trimStringValue(args["command"]) != "pwd" {
		t.Fatalf("tool args=%#v want command=pwd", toolCalls[0]["args"])
	}

	chunkEvent, ok := subagentTaskEventFromAgentEvent(task, AgentEvent{
		Type:      AgentEventChunk,
		MessageID: "ai-1",
		Text:      "running command",
	}, state)
	if !ok {
		t.Fatal("chunk event was ignored")
	}
	chunkMessage, ok := chunkEvent.Message.(map[string]any)
	if !ok {
		t.Fatalf("chunk message type=%T want map[string]any", chunkEvent.Message)
	}
	if got := trimStringValue(chunkMessage["content"]); got != "running command" {
		t.Fatalf("chunk content=%q want running command", got)
	}
	toolCalls, ok = chunkMessage["tool_calls"].([]map[string]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("chunk tool_calls=%#v want single call", chunkMessage["tool_calls"])
	}

	finalEvent, ok := subagentTaskEventFromAgentEvent(task, AgentEvent{
		Type:      AgentEventEnd,
		MessageID: "ai-2",
		Text:      "done",
	}, state)
	if !ok {
		t.Fatal("end event was ignored")
	}
	if finalEvent.MessageIndex != 2 || finalEvent.TotalMessages != 2 {
		t.Fatalf("final counters=(%d,%d) want (2,2)", finalEvent.MessageIndex, finalEvent.TotalMessages)
	}
	finalMessage, ok := finalEvent.Message.(map[string]any)
	if !ok {
		t.Fatalf("final message type=%T want map[string]any", finalEvent.Message)
	}
	if got := trimStringValue(finalMessage["id"]); got != "ai-2" {
		t.Fatalf("final id=%q want ai-2", got)
	}
	if got := trimStringValue(finalMessage["content"]); got != "done" {
		t.Fatalf("final content=%q want done", got)
	}
	if _, exists := finalMessage["tool_calls"]; exists {
		t.Fatalf("final tool_calls=%#v want none", finalMessage["tool_calls"])
	}
}

func TestSubagentTaskEventFromAgentEvent_IgnoresTextChunkDuplicates(t *testing.T) {
	task := &subagent.Task{ID: "task-1", Description: "inspect repo"}
	state := &subagentStreamState{}

	if _, ok := subagentTaskEventFromAgentEvent(task, AgentEvent{
		Type:      AgentEventTextChunk,
		MessageID: "ai-1",
		Text:      "duplicate delta",
	}, state); ok {
		t.Fatal("text chunk should be ignored to avoid duplicate task_running updates")
	}
}

func TestSubagentTaskEventFromAgentEvent_PreservesChunkSpacing(t *testing.T) {
	task := &subagent.Task{ID: "task-1", Description: "inspect repo"}
	state := &subagentStreamState{}

	first, ok := subagentTaskEventFromAgentEvent(task, AgentEvent{
		Type:      AgentEventChunk,
		MessageID: "ai-1",
		Text:      "hello ",
	}, state)
	if !ok {
		t.Fatal("first chunk event was ignored")
	}
	second, ok := subagentTaskEventFromAgentEvent(task, AgentEvent{
		Type:      AgentEventChunk,
		MessageID: "ai-1",
		Text:      "world",
	}, state)
	if !ok {
		t.Fatal("second chunk event was ignored")
	}

	firstMessage, ok := first.Message.(map[string]any)
	if !ok {
		t.Fatalf("first message type=%T want map[string]any", first.Message)
	}
	if got := trimStringValue(firstMessage["content"]); got != "hello" {
		t.Fatalf("first content=%q want hello", got)
	}

	secondMessage, ok := second.Message.(map[string]any)
	if !ok {
		t.Fatalf("second message type=%T want map[string]any", second.Message)
	}
	if got := trimStringValue(secondMessage["content"]); got != "hello world" {
		t.Fatalf("second content=%q want hello world", got)
	}
}

func TestSubagentExecutorUsesLazySandboxProvider(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(models.Tool{
		Name:        "inspect_sandbox",
		Description: "Report whether a sandbox is available.",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
			if tools.SandboxFromContext(ctx) == nil {
				return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: "sandbox:missing"}, nil
			}
			return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: "sandbox:available"}, nil
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	provider := &scriptedStreamProvider{
		t: t,
		steps: []streamStep{
			{
				toolCalls: []models.ToolCall{{
					ID:   "call-sandbox-1",
					Name: "inspect_sandbox",
				}},
			},
			{
				check: func(t *testing.T, req llm.ChatRequest) {
					last := req.Messages[len(req.Messages)-1]
					if last.Role != models.RoleTool || last.ToolResult == nil {
						t.Fatalf("last message=%#v", last)
					}
					if !strings.Contains(last.Content, "sandbox:available") {
						t.Fatalf("tool result content=%q want sandbox:available", last.Content)
					}
				},
				content: "Sandbox check complete.",
			},
		},
	}

	executor := NewSubagentExecutor(provider, registry, nil)
	var providerCalls int
	executor.SetSandboxProvider(func() (sandbox.Session, error) {
		providerCalls++
		return &sandbox.Sandbox{}, nil
	})

	result, err := executor.Execute(context.Background(), &subagent.Task{
		ID:          "task-sandbox-provider",
		Description: "inspect sandbox availability",
		Prompt:      "Check whether tools receive a sandbox.",
		Config: subagent.SubagentConfig{
			Type:     subagent.SubagentGeneralPurpose,
			MaxTurns: 3,
			Timeout:  time.Second,
		},
	}, func(subagent.TaskEvent) {})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if providerCalls != 1 {
		t.Fatalf("sandbox provider calls=%d want 1", providerCalls)
	}
	if result.Result != "Sandbox check complete." {
		t.Fatalf("result=%q want Sandbox check complete.", result.Result)
	}
}

func TestSubagentExecutorInheritsRuntimeContext(t *testing.T) {
	provider := &scriptedStreamProvider{
		t: t,
		steps: []streamStep{
			{
				check: func(t *testing.T, req llm.ChatRequest) {
					if req.Model != "gpt-5" {
						t.Fatalf("model=%q want gpt-5", req.Model)
					}
					if req.ReasoningEffort != "high" {
						t.Fatalf("reasoning_effort=%q want high", req.ReasoningEffort)
					}
					if !strings.Contains(req.SystemPrompt, "general-purpose subagent") {
						t.Fatalf("system prompt missing base prompt: %q", req.SystemPrompt)
					}
					if !strings.Contains(req.SystemPrompt, "<skill_system>") {
						t.Fatalf("system prompt missing skills prompt: %q", req.SystemPrompt)
					}
				},
				content: "Inherited runtime context.",
			},
		},
	}

	executor := NewSubagentExecutor(provider, tools.NewRegistry(), nil)
	ctx := tools.WithRuntimeContext(context.Background(), map[string]any{
		"model_name":       "gpt-5",
		"reasoning_effort": "high",
		"skills_prompt":    "<skill_system>\nUse the bootstrap skill.\n</skill_system>",
	})

	result, err := executor.Execute(ctx, &subagent.Task{
		ID:          "task-runtime-context",
		Description: "inherit runtime context",
		Prompt:      "Do the thing.",
		Config: subagent.SubagentConfig{
			Type:     subagent.SubagentGeneralPurpose,
			MaxTurns: 2,
			Timeout:  time.Second,
		},
	}, func(subagent.TaskEvent) {})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Result != "Inherited runtime context." {
		t.Fatalf("result=%q want inherited result", result.Result)
	}
}
