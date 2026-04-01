package agent

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
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
