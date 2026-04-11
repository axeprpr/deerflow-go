package harness

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestMergeLifecycleHooksCombinesPhases(t *testing.T) {
	var before []string
	var after []string

	hooks := MergeLifecycleHooks(
		&LifecycleHooks{
			BeforeRun: []BeforeRunHook{
				func(_ context.Context, _ *RunState) error {
					before = append(before, "one")
					return nil
				},
			},
		},
		&LifecycleHooks{
			AfterRun: []AfterRunHook{
				func(_ context.Context, _ *RunState, _ *agent.RunResult) error {
					after = append(after, "two")
					return nil
				},
			},
		},
	)

	if hooks == nil {
		t.Fatal("MergeLifecycleHooks() returned nil")
	}
	if err := hooks.Before(context.Background(), &RunState{}); err != nil {
		t.Fatalf("Before() error = %v", err)
	}
	if err := hooks.After(context.Background(), &RunState{}, &agent.RunResult{}); err != nil {
		t.Fatalf("After() error = %v", err)
	}
	if len(before) != 1 || before[0] != "one" {
		t.Fatalf("before hooks = %#v", before)
	}
	if len(after) != 1 || after[0] != "two" {
		t.Fatalf("after hooks = %#v", after)
	}
}

func TestClarificationLifecycleHooksStoresInterrupt(t *testing.T) {
	hooks := ClarificationLifecycleHooks(ClarificationLifecycleConfig{})
	state := &RunState{Metadata: map[string]any{}}
	result := &agent.RunResult{
		Messages: []models.Message{{
			Role: models.RoleTool,
			ToolResult: &models.ToolResult{
				ToolName: "ask_clarification",
				Status:   models.CallStatusCompleted,
				Content:  "Need more detail",
				Data: map[string]any{
					"id":       "clarify-1",
					"question": "Need more detail?",
				},
			},
		}},
	}

	if err := hooks.After(context.Background(), state, result); err != nil {
		t.Fatalf("After() error = %v", err)
	}
	raw, _ := state.Metadata["clarification_interrupt"].(map[string]any)
	if stringValue(raw["id"]) != "clarify-1" {
		t.Fatalf("clarification id = %q", stringValue(raw["id"]))
	}
}
