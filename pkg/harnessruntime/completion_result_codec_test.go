package harnessruntime

import "testing"

func TestCompletionResultCodecRoundTrip(t *testing.T) {
	codec := CompletionResultCodec{}
	payload, err := codec.Encode(CompletionResult{
		Run: RunRecord{
			RunID:           "run-1",
			ThreadID:        "thread-1",
			AssistantID:     "lead_agent",
			Attempt:         2,
			ResumeFromEvent: 9,
			ResumeReason:    "worker-retry",
			Status:          "interrupted",
			Outcome: RunOutcomeDescriptor{
				RunStatus:       "interrupted",
				Interrupted:     true,
				Attempt:         2,
				ResumeFromEvent: 9,
				ResumeReason:    "worker-retry",
			},
		},
		Interrupted: true,
		Outcome: RunOutcomeDescriptor{
			RunStatus:       "interrupted",
			Interrupted:     true,
			Attempt:         2,
			ResumeFromEvent: 9,
			ResumeReason:    "worker-retry",
		},
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	result, err := codec.Decode(payload)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if result.Run.RunID != "run-1" || result.Run.Attempt != 2 {
		t.Fatalf("result.Run = %+v", result.Run)
	}
	if !result.Interrupted || result.Outcome.ResumeFromEvent != 9 || result.Outcome.ResumeReason != "worker-retry" {
		t.Fatalf("result = %+v", result)
	}
}
