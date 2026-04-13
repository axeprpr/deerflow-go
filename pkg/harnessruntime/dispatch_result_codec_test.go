package harnessruntime

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestDispatchResultCodecRoundTripsExecutionHandle(t *testing.T) {
	registry := NewInMemoryExecutionHandleRegistry()
	codec := DispatchResultCodec{Handles: registry}
	handle := NewStaticExecutionHandle(&harness.Execution{}, "thread-1")

	payload, err := codec.Encode(&DispatchResult{
		Lifecycle: &harness.RunState{ThreadID: "thread-1"},
		Handle:    handle,
		Execution: handle.Describe(),
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	result, err := codec.Decode(payload)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if result == nil || result.Handle == nil {
		t.Fatalf("result = %#v", result)
	}
	if result.Execution.Kind != ExecutionKindLocalPrepared || result.Execution.SessionID != "thread-1" {
		t.Fatalf("result.Execution = %#v", result.Execution)
	}
	if _, err := result.Handle.Resolve(); err != nil {
		t.Fatalf("result.Handle.Resolve() error = %v", err)
	}
}

func TestDispatchResultCodecRoundTripsCompletedRunResult(t *testing.T) {
	codec := DispatchResultCodec{}
	payload, err := codec.Encode(&DispatchResult{
		Lifecycle: &harness.RunState{ThreadID: "thread-completed"},
		Execution: ExecutionDescriptor{Kind: ExecutionKindRemoteCompleted, SessionID: "thread-completed"},
		Completed: &agent.RunResult{
			Messages: []models.Message{{Role: models.RoleAI, Content: "done"}},
		},
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	result, err := codec.Decode(payload)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if result == nil || result.Completed == nil {
		t.Fatalf("result = %#v", result)
	}
	if result.Completed.Messages[0].Content != "done" {
		t.Fatalf("completed = %#v", result.Completed)
	}
	if result.Execution.Kind != ExecutionKindRemoteCompleted {
		t.Fatalf("execution = %#v", result.Execution)
	}
}
