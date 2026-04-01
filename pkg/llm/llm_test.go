package llm

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
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

	if msg.Content != "answer hidden in reasoning" {
		t.Fatalf("content=%q want extracted reasoning", msg.Content)
	}
	if got := msg.Metadata["additional_kwargs"]; got != "" {
		t.Fatalf("additional_kwargs=%q want removed reasoning_content", got)
	}
}
