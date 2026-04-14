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

func TestDeriveTaskStateFromResultTracksMultipleSubagentTasks(t *testing.T) {
	t.Parallel()

	state, ok := DeriveTaskStateFromResult(&agent.RunResult{
		Messages: []models.Message{
			{
				Role: models.RoleAI,
				ToolCalls: []models.ToolCall{
					{ID: "call-task-1", Name: "task", Arguments: map[string]any{"description": "research api"}},
					{ID: "call-task-2", Name: "task", Arguments: map[string]any{"description": "draft page"}},
				},
			},
			{
				Role: models.RoleTool,
				ToolResult: &models.ToolResult{
					CallID:   "call-task-1",
					ToolName: "task",
					Status:   models.CallStatusCompleted,
					Data: map[string]any{
						"description": "research api",
						"status":      "completed",
					},
				},
			},
			{
				Role: models.RoleTool,
				ToolResult: &models.ToolResult{
					CallID:   "call-task-2",
					ToolName: "task",
					Status:   models.CallStatusFailed,
					Data: map[string]any{
						"description": "draft page",
						"status":      "failed",
					},
				},
			},
		},
	})
	if !ok {
		t.Fatal("DeriveTaskStateFromResult() ok = false, want true")
	}
	if len(state.Items) != 2 {
		t.Fatalf("Items=%+v want len=2", state.Items)
	}
	if state.Items[0].Text != "research api" || state.Items[0].Status != harness.TaskStatusCompleted {
		t.Fatalf("Items[0]=%+v", state.Items[0])
	}
	if state.Items[1].Text != "draft page" || state.Items[1].Status != harness.TaskStatusPending {
		t.Fatalf("Items[1]=%+v", state.Items[1])
	}
}

func TestDeriveTaskStateFromResultKeepsDistinctSubagentTaskIDs(t *testing.T) {
	t.Parallel()

	state, ok := DeriveTaskStateFromResult(&agent.RunResult{
		Messages: []models.Message{
			{
				Role: models.RoleAI,
				ToolCalls: []models.ToolCall{
					{ID: "call-task-1", Name: "task", Arguments: map[string]any{"description": "delegate research"}},
					{ID: "call-task-2", Name: "task", Arguments: map[string]any{"description": "delegate research"}},
				},
			},
			{
				Role: models.RoleTool,
				ToolResult: &models.ToolResult{
					CallID:   "call-task-1",
					ToolName: "task",
					Status:   models.CallStatusCompleted,
					Data: map[string]any{
						"task_id":     "task-1",
						"description": "delegate research",
						"status":      "completed",
					},
				},
			},
			{
				Role: models.RoleTool,
				ToolResult: &models.ToolResult{
					CallID:   "call-task-2",
					ToolName: "task",
					Status:   models.CallStatusFailed,
					Data: map[string]any{
						"task_id":     "task-2",
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
	if len(state.Items) != 2 {
		t.Fatalf("Items=%+v want len=2", state.Items)
	}
	if state.Items[0].ID != "task-1" || state.Items[1].ID != "task-2" {
		t.Fatalf("Items=%+v", state.Items)
	}
}

func TestTaskStateProviderLoadTaskStateMergesLiveAndRunState(t *testing.T) {
	t.Parallel()

	provider := TaskStateProvider{
		Load: func(threadID string) harness.TaskState {
			if threadID != "thread-1" {
				t.Fatalf("threadID=%q", threadID)
			}
			return harness.TaskState{
				Items: []harness.TaskItem{
					{Text: "delegate review", Status: harness.TaskStatusInProgress},
				},
				VerifiedOutputs: []string{"/mnt/user-data/outputs/report.md"},
			}
		},
	}

	state := provider.LoadTaskState(&harness.RunState{
		ThreadID: "thread-1",
		TaskState: harness.TaskState{
			Items: []harness.TaskItem{
				{Text: "draft report", Status: harness.TaskStatusCompleted},
			},
			ExpectedOutputs: []string{"/mnt/user-data/outputs/report.md"},
		},
	})

	if len(state.Items) != 2 {
		t.Fatalf("Items=%+v", state.Items)
	}
	if len(state.ExpectedOutputs) != 1 || state.ExpectedOutputs[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("ExpectedOutputs=%v", state.ExpectedOutputs)
	}
	if len(state.VerifiedOutputs) != 1 || state.VerifiedOutputs[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("VerifiedOutputs=%v", state.VerifiedOutputs)
	}
}
