package clarification

import (
	"context"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestAskClarificationTool(t *testing.T) {
	manager := NewManager(1)
	tool := AskClarificationTool(manager)

	result, err := tool.Handler(WithThreadID(context.Background(), "thread-3"), models.ToolCall{
		ID:   "call-1",
		Name: tool.Name,
		Arguments: map[string]any{
			"question":           "Which approach should I use?",
			"clarification_type": "approach_choice",
			"options":            []any{"Fast", "Thorough"},
			"required":           true,
		},
	})
	if err != nil {
		t.Fatalf("tool handler error = %v", err)
	}
	if result.Status != models.CallStatusCompleted {
		t.Fatalf("result status = %q", result.Status)
	}
	if result.Content != "🔀 Which approach should I use?\n\n  1. Fast\n  2. Thorough" {
		t.Fatalf("content = %q", result.Content)
	}
	if got, _ := result.Data["clarification_type"].(string); got != "approach_choice" {
		t.Fatalf("clarification_type = %q, want approach_choice", got)
	}

	id, _ := result.Data["id"].(string)
	if id == "" {
		t.Fatal("tool result missing clarification id")
	}

	item, ok := manager.Get(id)
	if !ok {
		t.Fatal("clarification not stored in manager")
	}
	if item.ThreadID != "thread-3" {
		t.Fatalf("thread_id = %q, want thread-3", item.ThreadID)
	}
}

func TestAskClarificationToolAcceptsLegacyTypeAlias(t *testing.T) {
	manager := NewManager(1)
	tool := AskClarificationTool(manager)

	result, err := tool.Handler(context.Background(), models.ToolCall{
		ID:   "call-2",
		Name: tool.Name,
		Arguments: map[string]any{
			"type":     "confirm",
			"question": "Continue?",
		},
	})
	if err != nil {
		t.Fatalf("tool handler error = %v", err)
	}
	if result.Status != models.CallStatusCompleted {
		t.Fatalf("result status = %q", result.Status)
	}
	if got, _ := result.Data["type"].(string); got != "confirm" {
		t.Fatalf("type = %q, want confirm", got)
	}
	if got, _ := result.Data["clarification_type"].(string); got != "risk_confirmation" {
		t.Fatalf("clarification_type = %q, want risk_confirmation", got)
	}
	if result.Content != "⚠️ Continue?" {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestInterceptToolCallUsesManagerFromContext(t *testing.T) {
	manager := NewManager(1)
	ctx := WithManager(WithThreadID(context.Background(), "thread-9"), manager)

	result, err := InterceptToolCall(ctx, models.ToolCall{
		ID:   "call-3",
		Name: "ask_clarification",
		Arguments: map[string]any{
			"question":           "Which mode should I use?",
			"clarification_type": "approach_choice",
			"context":            "There are multiple valid approaches.",
			"options":            []any{"Fast", "Safe"},
			"required":           true,
		},
	})
	if err != nil {
		t.Fatalf("InterceptToolCall() error = %v", err)
	}
	if result.Status != models.CallStatusCompleted {
		t.Fatalf("result status = %q", result.Status)
	}
	if !strings.Contains(result.Content, "There are multiple valid approaches.") {
		t.Fatalf("content = %q", result.Content)
	}

	id, _ := result.Data["id"].(string)
	if id == "" {
		t.Fatal("result missing clarification id")
	}
	item, ok := manager.Get(id)
	if !ok {
		t.Fatal("clarification not stored in manager")
	}
	if item.ThreadID != "thread-9" {
		t.Fatalf("thread_id = %q, want thread-9", item.ThreadID)
	}
}

func TestInterceptToolCallAllowsMissingQuestion(t *testing.T) {
	result, err := InterceptToolCall(context.Background(), models.ToolCall{
		ID:   "call-4",
		Name: "ask_clarification",
		Arguments: map[string]any{
			"clarification_type": "missing_info",
		},
	})
	if err != nil {
		t.Fatalf("InterceptToolCall() error = %v", err)
	}
	if result.Status != models.CallStatusCompleted {
		t.Fatalf("result status = %q", result.Status)
	}
	if result.Content != "❓ Please provide the missing information needed to continue." {
		t.Fatalf("content = %q", result.Content)
	}
	if got, _ := result.Data["question"].(string); got != "Please provide the missing information needed to continue." {
		t.Fatalf("question = %q", got)
	}
}
