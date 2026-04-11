package harness

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestBindContextInjectsRuntimeHooks(t *testing.T) {
	manager := clarification.NewManager(16)
	var taskEvents int
	var clarificationEvents int

	ctx := BindContext(context.Background(), ContextSpec{
		ThreadID:             "thread-ctx",
		ClarificationManager: manager,
		RuntimeContext: map[string]any{
			"skill_paths": []string{"/mnt/skills/public/frontend-design/SKILL.md"},
		},
		Hooks: RunHooks{
			TaskSink: func(evt subagent.TaskEvent) {
				taskEvents++
			},
			ClarificationSink: func(item *clarification.Clarification) {
				clarificationEvents++
			},
		},
	})

	if got := clarification.ThreadIDFromContext(ctx); got != "thread-ctx" {
		t.Fatalf("clarification thread id = %q, want %q", got, "thread-ctx")
	}
	if got := clarification.ManagerFromContext(ctx); got != manager {
		t.Fatal("clarification manager missing from context")
	}
	if got := tools.RuntimeContextFromContext(ctx); len(got) != 1 {
		t.Fatalf("runtime context = %#v, want 1 entry", got)
	}

	subagent.EmitEvent(ctx, subagent.TaskEvent{Type: "started"})
	clarification.EmitEvent(ctx, &clarification.Clarification{ID: "c1"})

	if taskEvents != 1 {
		t.Fatalf("task events = %d, want 1", taskEvents)
	}
	if clarificationEvents != 1 {
		t.Fatalf("clarification events = %d, want 1", clarificationEvents)
	}
}
