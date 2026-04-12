package agent

import (
	"testing"

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
