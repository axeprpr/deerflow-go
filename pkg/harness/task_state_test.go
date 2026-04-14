package harness

import "testing"

func TestNormalizeTaskStateValidatesSingleInProgress(t *testing.T) {
	t.Parallel()

	_, err := NormalizeTaskState(TaskState{
		Items: []TaskItem{
			{Text: "draft", Status: TaskStatusInProgress},
			{Text: "verify", Status: TaskStatusInProgress},
		},
	})
	if err == nil {
		t.Fatal("NormalizeTaskState() error = nil, want error")
	}
}

func TestParseTaskStateNormalizesStructuredValues(t *testing.T) {
	t.Parallel()

	state, ok := ParseTaskState(map[string]any{
		"items": []any{
			map[string]any{"text": " draft plan ", "status": "pending"},
			map[string]any{"text": "verify artifact", "status": "completed"},
		},
		"expected_outputs": []any{" /tmp/report.md ", "/tmp/report.md", ""},
		"verified_outputs": []any{" /tmp/report.md "},
	})
	if !ok {
		t.Fatal("ParseTaskState() ok = false, want true")
	}
	if got := len(state.Items); got != 2 {
		t.Fatalf("items=%d want=2", got)
	}
	if state.Items[0].Text != "draft plan" {
		t.Fatalf("item text=%q want=%q", state.Items[0].Text, "draft plan")
	}
	if missing := state.MissingExpectedOutputs(); len(missing) != 0 {
		t.Fatalf("MissingExpectedOutputs()=%v want=[]", missing)
	}
}

func TestMergeTaskStatesPreservesExpectedOutputsAndAddsVerifiedOutputs(t *testing.T) {
	t.Parallel()

	merged, err := MergeTaskStates(
		TaskState{
			Items: []TaskItem{
				{Text: "draft report", Status: TaskStatusCompleted},
				{Text: "present report", Status: TaskStatusPending},
			},
			ExpectedOutputs: []string{"/mnt/user-data/outputs/report.md"},
		},
		TaskState{
			Items: []TaskItem{
				{Text: "draft report", Status: TaskStatusCompleted},
				{Text: "present report", Status: TaskStatusCompleted},
			},
			VerifiedOutputs: []string{"/mnt/user-data/outputs/report.md"},
		},
	)
	if err != nil {
		t.Fatalf("MergeTaskStates() error = %v", err)
	}
	if missing := merged.MissingExpectedOutputs(); len(missing) != 0 {
		t.Fatalf("MissingExpectedOutputs()=%v want=[]", missing)
	}
	if got := len(merged.VerifiedOutputs); got != 1 {
		t.Fatalf("VerifiedOutputs=%v want len=1", merged.VerifiedOutputs)
	}
	if got := merged.Items[1].Status; got != TaskStatusCompleted {
		t.Fatalf("merged status=%q want=%q", got, TaskStatusCompleted)
	}
}
