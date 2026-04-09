package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools/builtin"
	einoSchema "github.com/cloudwego/eino/schema"
)

func TestChatRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     ChatRequest
		wantErr bool
	}{
		{
			name: "valid request",
			req: ChatRequest{
				Model:    "test-model",
				Messages: []models.Message{{ID: "m1", SessionID: "s1", Role: models.RoleHuman, Content: "hello"}},
			},
			wantErr: false,
		},
		{
			name: "empty model",
			req: ChatRequest{
				Model:    "",
				Messages: []models.Message{{ID: "m1", SessionID: "s1", Role: models.RoleHuman, Content: "hello"}},
			},
			wantErr: true,
		},
		{
			name: "empty messages",
			req: ChatRequest{
				Model:    "test-model",
				Messages: []models.Message{},
			},
			wantErr: true,
		},
		{
			name: "nil messages",
			req: ChatRequest{
				Model:    "test-model",
				Messages: nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUnavailableProvider(t *testing.T) {
	provider := &UnavailableProvider{err: errors.New("unavailable")}

	// Test Chat
	_, err := provider.Chat(context.Background(), ChatRequest{
		Model:    "test",
		Messages: []models.Message{{ID: "m1", SessionID: "s1", Role: models.RoleHuman, Content: "hi"}},
	})
	if err == nil {
		t.Error("Chat should return error for UnavailableProvider")
	}

	// Test Stream - the error is sent in the channel, not returned
	ch, err := provider.Stream(context.Background(), ChatRequest{
		Model:    "test",
		Messages: []models.Message{{ID: "m1", SessionID: "s1", Role: models.RoleHuman, Content: "hi"}},
	})
	if err != nil {
		t.Errorf("Stream should not return error directly: %v", err)
	}

	// Check the channel receives the error
	chunk := <-ch
	if chunk.Err == nil {
		t.Error("Stream chunk should contain error")
	}
}

func TestNewProvider(t *testing.T) {
	// Test with openai
	provider := NewProvider("openai")
	if provider == nil {
		t.Error("NewProvider should return a provider")
	}

	// Test with siliconflow
	provider = NewProvider("siliconflow")
	if provider == nil {
		t.Error("NewProvider should return a provider for siliconflow")
	}

	// Test with invalid provider name (should return unavailable)
	provider = NewProvider("nonexistent")
	if provider == nil {
		t.Error("NewProvider should return unavailable provider for invalid names")
	}
}

func TestEinoProviderPrefersStructuredToolCalls(t *testing.T) {
	provider := &EinoProvider{}
	if !provider.PrefersStructuredToolCalls() {
		t.Fatal("EinoProvider should prefer structured tool calls")
	}
}

func TestToEinoToolInfosPreservesClarificationRequiredFields(t *testing.T) {
	tool := clarification.AskClarificationTool(nil)
	infos := toEinoToolInfos([]models.Tool{tool})
	if len(infos) != 1 {
		t.Fatalf("tool infos len=%d want 1", len(infos))
	}
	if infos[0].Name != "ask_clarification" {
		t.Fatalf("tool name=%q want ask_clarification", infos[0].Name)
	}
	if infos[0].ParamsOneOf == nil {
		t.Fatal("params oneof should not be nil")
	}

	params := jsonSchemaToParams(tool.InputSchema)
	question, ok := params["question"]
	if !ok || !question.Required {
		t.Fatalf("question required=%v want true", ok && question.Required)
	}
	clarificationType, ok := params["clarification_type"]
	if !ok || !clarificationType.Required {
		t.Fatalf("clarification_type required=%v want true", ok && clarificationType.Required)
	}
	if clarificationType.Type != einoSchema.String {
		t.Fatalf("clarification_type type=%v want string", clarificationType.Type)
	}
	if _, ok := params["type"]; ok {
		t.Fatal("legacy type alias should not be exposed to the model schema")
	}
	if _, ok := params["default"]; ok {
		t.Fatal("default should not be exposed to the model schema")
	}
	if _, ok := params["required"]; ok {
		t.Fatal("required flag should not be exposed to the model schema")
	}
	if !strings.Contains(infos[0].Desc, "When to use ask_clarification:") {
		t.Fatal("clarification description should include upstream usage guidance")
	}
	if !strings.Contains(infos[0].Desc, "Best practices:") {
		t.Fatal("clarification description should include upstream best practices")
	}
}

func TestToEinoToolInfosPreservesBashDescriptionGuidance(t *testing.T) {
	tool := builtin.BashTool()
	infos := toEinoToolInfos([]models.Tool{tool})
	if len(infos) != 1 {
		t.Fatalf("tool infos len=%d want 1", len(infos))
	}
	if !strings.Contains(infos[0].Desc, "Use `python` to run Python code.") {
		t.Fatal("bash description should include python guidance")
	}
	if !strings.Contains(infos[0].Desc, "/mnt/user-data/workspace/.venv") {
		t.Fatal("bash description should include workspace venv guidance")
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

func TestStreamChunk(t *testing.T) {
	chunk := StreamChunk{
		Delta: "Hello",
		Done:  false,
	}

	if chunk.Delta != "Hello" {
		t.Errorf("Delta = %s, want Hello", chunk.Delta)
	}
	if chunk.Done {
		t.Error("Done should be false")
	}

	// Test done chunk
	doneChunk := StreamChunk{
		Done:  true,
		Usage: &Usage{TotalTokens: 100},
	}

	if !doneChunk.Done {
		t.Error("Done should be true")
	}
	if doneChunk.Usage.TotalTokens != 100 {
		t.Errorf("Usage.TotalTokens = %d, want 100", doneChunk.Usage.TotalTokens)
	}
}

func TestChatResponse(t *testing.T) {
	resp := ChatResponse{
		Model: "test-model",
		Message: models.Message{
			ID:      "m1",
			Role:    models.RoleAI,
			Content: "Hello, world!",
		},
		Usage: Usage{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
		Stop: "stop",
	}

	if resp.Model != "test-model" {
		t.Errorf("Model = %s, want 'test-model'", resp.Model)
	}
	if resp.Message.Content != "Hello, world!" {
		t.Errorf("Content = %s, want 'Hello, world!'", resp.Message.Content)
	}
	if resp.Stop != "stop" {
		t.Errorf("Stop = %s, want 'stop'", resp.Stop)
	}
}

func TestPtr(t *testing.T) {
	val := "test"
	ptr := ptr(val)

	if ptr == nil {
		t.Error("ptr should not return nil")
	}

	if *ptr != val {
		t.Errorf("*ptr = %s, want %s", *ptr, val)
	}
}

func TestNormalizeAssistantMessage_StripsThinkTagsIntoReasoningContent(t *testing.T) {
	msg := NormalizeAssistantMessage(models.Message{
		Role:    models.RoleAI,
		Content: "<think>\nfirst pass\n</think>\n\nFinal answer.",
	})

	if msg.Content != "Final answer." {
		t.Fatalf("content=%q want final answer", msg.Content)
	}

	var kwargs map[string]any
	if err := json.Unmarshal([]byte(msg.Metadata["additional_kwargs"]), &kwargs); err != nil {
		t.Fatalf("unmarshal additional_kwargs: %v", err)
	}
	if got, _ := kwargs["reasoning_content"].(string); got != "first pass" {
		t.Fatalf("reasoning_content=%q want first pass", got)
	}
}

func TestNormalizeAssistantMessage_UsesThinkContentWhenVisibleAnswerWouldBeEmpty(t *testing.T) {
	msg := NormalizeAssistantMessage(models.Message{
		Role:    models.RoleAI,
		Content: "<think>\nanswer hidden in reasoning\n</think>",
		Metadata: map[string]string{
			"additional_kwargs": `{"reasoning_content":"older"}`,
		},
	})

	if msg.Content != "" {
		t.Fatalf("content=%q want empty", msg.Content)
	}
	var kwargs map[string]any
	if err := json.Unmarshal([]byte(msg.Metadata["additional_kwargs"]), &kwargs); err != nil {
		t.Fatalf("unmarshal additional_kwargs: %v", err)
	}
	if got, _ := kwargs["reasoning_content"].(string); got != "older\n\nanswer hidden in reasoning" {
		t.Fatalf("reasoning_content=%q want merged reasoning", got)
	}
	if !HasReasoningContent(msg) {
		t.Fatal("expected HasReasoningContent to detect reasoning-only message")
	}
}

func TestToEinoToolCalls_NormalizesAndSkipsInvalidCalls(t *testing.T) {
	calls := []models.ToolCall{
		{
			Name:      "write_file",
			Arguments: map[string]any{"path": "index.html"},
			Status:    models.CallStatusPending,
		},
		{
			ID:     "",
			Name:   "",
			Status: models.CallStatusPending,
		},
	}

	got := toEinoToolCalls(calls)
	if len(got) != 1 {
		t.Fatalf("len(toEinoToolCalls()) = %d, want 1", len(got))
	}
	if got[0].ID == "" {
		t.Fatal("normalized tool call id should not be empty")
	}
	if got[0].Function.Name != "write_file" {
		t.Fatalf("tool name = %q, want write_file", got[0].Function.Name)
	}
}

func TestToEinoMessage_SkipsInvalidToolMessages(t *testing.T) {
	msg := models.Message{
		ID:        "tool_1",
		SessionID: "session_1",
		Role:      models.RoleTool,
		ToolResult: &models.ToolResult{
			Status: models.CallStatusFailed,
			Error:  "bad tool result",
		},
	}

	if got := toEinoMessage(msg); got != nil {
		t.Fatal("toEinoMessage() should skip invalid tool messages")
	}
}

func TestCollectStreamToolCalls_ReassemblesArgumentsAcrossChunks(t *testing.T) {
	merged := collectStreamToolCalls([]*einoSchema.Message{
		{
			ToolCalls: []einoSchema.ToolCall{{
				ID:   "call-1",
				Type: "function",
				Function: einoSchema.FunctionCall{
					Name:      "read_file",
					Arguments: `{"description":"Load skill",`,
				},
			}},
		},
		{
			ToolCalls: []einoSchema.ToolCall{{
				ID:   "call-1",
				Type: "function",
				Function: einoSchema.FunctionCall{
					Name:      "read_file",
					Arguments: `"path":"/mnt/skills/public/frontend-design/SKILL.md"}`,
				},
			}},
		},
	})

	if len(merged) != 1 {
		t.Fatalf("tool calls=%d want 1", len(merged))
	}
	if merged[0].Name != "read_file" {
		t.Fatalf("tool name=%q want read_file", merged[0].Name)
	}
	if got, _ := merged[0].Arguments["description"].(string); got != "Load skill" {
		t.Fatalf("description=%q want Load skill", got)
	}
	if got, _ := merged[0].Arguments["path"].(string); got != "/mnt/skills/public/frontend-design/SKILL.md" {
		t.Fatalf("path=%q", got)
	}
}

func TestFinalStreamMessage_PrefersReassembledToolArguments(t *testing.T) {
	msg := &einoSchema.Message{
		Role:    einoSchema.Assistant,
		Content: "",
		ToolCalls: []einoSchema.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: einoSchema.FunctionCall{
				Name:      "read_file",
				Arguments: "",
			},
		}},
	}

	out := finalStreamMessage(msg, []models.ToolCall{{
		ID:        "call-1",
		Name:      "read_file",
		Arguments: map[string]any{"path": "/mnt/skills/public/frontend-design/SKILL.md"},
		Status:    models.CallStatusPending,
	}})

	if len(out.ToolCalls) != 1 {
		t.Fatalf("tool calls=%d want 1", len(out.ToolCalls))
	}
	if got, _ := out.ToolCalls[0].Arguments["path"].(string); got != "/mnt/skills/public/frontend-design/SKILL.md" {
		t.Fatalf("path=%q", got)
	}
}
