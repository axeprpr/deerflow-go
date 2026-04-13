package harnessruntime

import "testing"

func TestRunSnapshotCodecRoundTrip(t *testing.T) {
	codec := RunSnapshotCodec{}
	payload, err := codec.Encode(RunSnapshot{
		Record: RunRecord{
			RunID:           "run-1",
			ThreadID:        "thread-1",
			AssistantID:     "lead_agent",
			Attempt:         2,
			ResumeFromEvent: 5,
			ResumeReason:    "resume",
			Status:          "running",
			Outcome: RunOutcomeDescriptor{
				RunStatus:       "running",
				Attempt:         2,
				ResumeFromEvent: 5,
				ResumeReason:    "resume",
			},
		},
		Events: []RunEvent{{
			ID:              "run-1:6",
			Event:           "updates",
			RunID:           "run-1",
			ThreadID:        "thread-1",
			Attempt:         2,
			ResumeFromEvent: 5,
			ResumeReason:    "resume",
			Outcome: RunOutcomeDescriptor{
				RunStatus:       "running",
				Attempt:         2,
				ResumeFromEvent: 5,
				ResumeReason:    "resume",
			},
		}},
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	snapshot, err := codec.Decode(payload)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if snapshot.Record.ResumeReason != "resume" || len(snapshot.Events) != 1 || snapshot.Events[0].Attempt != 2 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestThreadRuntimeStateCodecRoundTrip(t *testing.T) {
	codec := ThreadRuntimeStateCodec{}
	payload, err := codec.Encode(ThreadRuntimeState{
		ThreadID: "thread-1",
		Status:   "busy",
		Metadata: map[string]any{
			"assistant_id": "lead_agent",
			"run_id":       "run-1",
		},
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	state, err := codec.Decode(payload)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if state.ThreadID != "thread-1" || state.Metadata["run_id"] != "run-1" {
		t.Fatalf("state = %+v", state)
	}
}
