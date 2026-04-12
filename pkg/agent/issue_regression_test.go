package agent

import (
	"context"
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
