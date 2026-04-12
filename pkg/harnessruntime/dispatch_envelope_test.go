package harnessruntime

import (
	"testing"
	"time"
)

func TestDispatchEnvelopeCodecRoundTrip(t *testing.T) {
	codec := DispatchEnvelopeCodec{Plans: WorkerPlanCodec{}}
	req := DispatchRequest{
		Plan: WorkerExecutionPlan{
			RunID:       "run-1",
			ThreadID:    "thread-1",
			SubmittedAt: time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC),
			Attempt:     2,
			Spec:        PortableAgentSpec{Model: "model-1"},
		},
	}

	env, err := codec.Encode(req)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if env.RunID != "run-1" || env.ThreadID != "thread-1" || env.Attempt != 2 {
		t.Fatalf("envelope = %#v", env)
	}
	if len(env.Payload) == 0 {
		t.Fatal("payload is empty")
	}

	decoded, err := codec.Decode(env)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.Plan.RunID != "run-1" || decoded.Plan.ThreadID != "thread-1" || decoded.Plan.Spec.Model != "model-1" {
		t.Fatalf("decoded = %#v", decoded)
	}
}
