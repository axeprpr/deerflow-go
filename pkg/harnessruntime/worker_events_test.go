package harnessruntime

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestWorkerRunEventRecorderRecordsChunkAndToolEvents(t *testing.T) {
	store := NewInMemoryRunEventStore()
	recorder := NewWorkerRunEventRecorder(store)
	plan := WorkerExecutionPlan{
		RunID:    "run-worker-events",
		ThreadID: "thread-worker-events",
		Attempt:  2,
	}

	recorder.RecordAgentEvent(plan, agent.AgentEvent{
		Type: agent.AgentEventChunk,
		Text: "hello",
	})
	recorder.RecordAgentEvent(plan, agent.AgentEvent{
		Type: agent.AgentEventToolCallEnd,
		ToolEvent: &agent.ToolCallEvent{
			ID:            "call-1",
			Name:          "read_file",
			Status:        models.CallStatusCompleted,
			ResultPreview: "done",
		},
	})

	events := store.LoadRunEvents(plan.RunID)
	if len(events) != 5 {
		t.Fatalf("event count = %d, want 5", len(events))
	}
	if events[0].Event != "chunk" {
		t.Fatalf("first event = %q", events[0].Event)
	}
	if events[0].Attempt != 2 || events[0].Outcome.RunStatus != "running" {
		t.Fatalf("chunk event context = %#v", events[0])
	}
	if events[1].Event != "tool_call_end" {
		t.Fatalf("second event = %q", events[1].Event)
	}
	if events[4].Event != "messages-tuple" {
		t.Fatalf("last event = %q", events[4].Event)
	}
}
