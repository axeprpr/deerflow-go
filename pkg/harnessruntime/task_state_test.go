package harnessruntime

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestDeriveTaskStateFromResultTracksVerifiedOutputs(t *testing.T) {
	t.Parallel()

	state, ok := DeriveTaskStateFromResult(&agent.RunResult{
		Messages: []models.Message{
			{
				Role: models.RoleAI,
				ToolCalls: []models.ToolCall{{
					ID:        "call-write",
					Name:      "write_file",
					Arguments: map[string]any{"path": "/mnt/user-data/outputs/report.md"},
					Status:    models.CallStatusCompleted,
				}},
			},
			{
				Role: models.RoleTool,
				ToolResult: &models.ToolResult{
					CallID:   "call-write",
					ToolName: "write_file",
					Status:   models.CallStatusCompleted,
					Content:  "OK",
				},
			},
		},
	})
	if !ok {
		t.Fatal("DeriveTaskStateFromResult() ok = false, want true")
	}
	if got := len(state.VerifiedOutputs); got != 1 {
		t.Fatalf("VerifiedOutputs=%v want len=1", state.VerifiedOutputs)
	}
	if got := state.VerifiedOutputs[0]; got != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("VerifiedOutputs[0]=%q", got)
	}
}

func TestResolveTaskStateFromRunStateMergesDerivedOutputs(t *testing.T) {
	t.Parallel()

	state, ok := resolveTaskStateFromRunState(&harness.RunState{
		ThreadID: "thread-1",
		TaskState: harness.TaskState{
			Items: []harness.TaskItem{
				{Text: "write report", Status: harness.TaskStatusCompleted},
				{Text: "present report", Status: harness.TaskStatusCompleted},
			},
			ExpectedOutputs: []string{"/mnt/user-data/outputs/report.md"},
		},
	}, &agent.RunResult{
		Messages: []models.Message{{
			Role: models.RoleTool,
			ToolResult: &models.ToolResult{
				CallID:   "call-present",
				ToolName: "present_files",
				Status:   models.CallStatusCompleted,
				Data: map[string]any{
					"filepaths": []any{"/mnt/user-data/outputs/report.md"},
				},
			},
		}},
	}, DefaultTaskStateMetadataKey)
	if !ok {
		t.Fatal("resolveTaskStateFromRunState() ok = false, want true")
	}
	if missing := state.MissingExpectedOutputs(); len(missing) != 0 {
		t.Fatalf("MissingExpectedOutputs()=%v want=[]", missing)
	}
}

func TestDeriveTaskStateFromResultTracksCompletedSubagentTasks(t *testing.T) {
	t.Parallel()

	state, ok := DeriveTaskStateFromResult(&agent.RunResult{
		Messages: []models.Message{
			{
				Role: models.RoleAI,
				ToolCalls: []models.ToolCall{{
					ID:   "call-task",
					Name: "task",
					Arguments: map[string]any{
						"description": "delegate research",
					},
				}},
			},
			{
				Role: models.RoleTool,
				ToolResult: &models.ToolResult{
					CallID:   "call-task",
					ToolName: "task",
					Status:   models.CallStatusCompleted,
					Data: map[string]any{
						"description": "delegate research",
						"status":      "completed",
					},
				},
			},
		},
	})
	if !ok {
		t.Fatal("DeriveTaskStateFromResult() ok = false, want true")
	}
	if len(state.Items) != 1 || state.Items[0].Text != "delegate research" || state.Items[0].Status != harness.TaskStatusCompleted {
		t.Fatalf("Items=%+v", state.Items)
	}
}

func TestDeriveTaskStateFromResultTracksFailedSubagentTasksAsPending(t *testing.T) {
	t.Parallel()

	state, ok := DeriveTaskStateFromResult(&agent.RunResult{
		Messages: []models.Message{
			{
				Role: models.RoleAI,
				ToolCalls: []models.ToolCall{{
					ID:   "call-task",
					Name: "task",
					Arguments: map[string]any{
						"description": "delegate research",
					},
				}},
			},
			{
				Role: models.RoleTool,
				ToolResult: &models.ToolResult{
					CallID:   "call-task",
					ToolName: "task",
					Status:   models.CallStatusFailed,
					Error:    "boom",
					Data: map[string]any{
						"description": "delegate research",
						"status":      "failed",
					},
				},
			},
		},
	})
	if !ok {
		t.Fatal("DeriveTaskStateFromResult() ok = false, want true")
	}
	if len(state.Items) != 1 || state.Items[0].Status != harness.TaskStatusPending {
		t.Fatalf("Items=%+v", state.Items)
	}
}
