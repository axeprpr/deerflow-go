package harnessruntime

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
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
	if events[0].Outcome.TaskLifecycle.Status != "running" {
		t.Fatalf("chunk task lifecycle = %+v", events[0].Outcome.TaskLifecycle)
	}
	if events[1].Event != "tool_call_end" {
		t.Fatalf("second event = %q", events[1].Event)
	}
	if events[4].Event != "messages-tuple" {
		t.Fatalf("last event = %q", events[4].Event)
	}
}

func TestWorkerRunEventRecorderRecordsTaskClarificationAndCompletion(t *testing.T) {
	store := NewInMemoryRunEventStore()
	recorder := NewWorkerRunEventRecorder(store)
	plan := WorkerExecutionPlan{
		RunID:    "run-worker-sinks",
		ThreadID: "thread-worker-sinks",
		Attempt:  1,
	}

	recorder.RecordTaskEvent(plan, subagent.TaskEvent{
		Type:   "task_running",
		TaskID: "task-1",
	})
	recorder.RecordClarification(plan, &clarification.Clarification{
		ID:       "clarify-1",
		ThreadID: plan.ThreadID,
		Question: "Need more detail?",
	})
	recorder.RecordCompletion(plan, &agent.RunResult{Usage: &agent.Usage{TotalTokens: 7}}, RunOutcomeDescriptor{
		RunStatus:     "incomplete",
		TaskLifecycle: TaskLifecycleDescriptor{Status: "incomplete", PendingTasks: []string{"verify artifact"}},
	})

	events := store.LoadRunEvents(plan.RunID)
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	if events[0].Event != "task_running" {
		t.Fatalf("first event = %q", events[0].Event)
	}
	if events[1].Event != "clarification_request" {
		t.Fatalf("second event = %q", events[1].Event)
	}
	if events[2].Event != "end" {
		t.Fatalf("third event = %q", events[2].Event)
	}
	if events[2].Outcome.RunStatus != "incomplete" || events[2].Outcome.TaskLifecycle.Status != "incomplete" {
		t.Fatalf("completion event outcome = %+v", events[2].Outcome)
	}
}

func TestWorkerRunEventRecorderCarriesLiveTaskLifecycleIntoTaskEvents(t *testing.T) {
	store := NewInMemoryRunEventStore()
	threads := NewInMemoryThreadStateStore()
	threads.SetThreadMetadata("thread-worker-live", DefaultTaskStateMetadataKey, harness.TaskState{
		Items: []harness.TaskItem{
			{Text: "delegate research", Status: harness.TaskStatusInProgress},
		},
		ExpectedOutputs: []string{"/mnt/user-data/outputs/report.md"},
	}.Value())

	recorder := NewWorkerRunEventRecorder(store, threads)
	plan := WorkerExecutionPlan{
		RunID:    "run-worker-live",
		ThreadID: "thread-worker-live",
		Attempt:  1,
	}

	recorder.RecordTaskEvent(plan, subagent.TaskEvent{
		Type:        "task_running",
		TaskID:      "task-1",
		Description: "delegate research",
	})

	events := store.LoadRunEvents(plan.RunID)
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if events[0].Outcome.TaskLifecycle.Status != "running" {
		t.Fatalf("task lifecycle = %+v", events[0].Outcome.TaskLifecycle)
	}
	if len(events[0].Outcome.TaskLifecycle.PendingTasks) != 1 || events[0].Outcome.TaskLifecycle.PendingTasks[0] != "delegate research" {
		t.Fatalf("pending tasks = %#v", events[0].Outcome.TaskLifecycle.PendingTasks)
	}
	if len(events[0].Outcome.TaskLifecycle.ExpectedArtifacts) != 1 || events[0].Outcome.TaskLifecycle.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("expected artifacts = %#v", events[0].Outcome.TaskLifecycle.ExpectedArtifacts)
	}
	if len(events[0].Outcome.TaskState.Items) != 1 || events[0].Outcome.TaskState.Items[0].Text != "delegate research" {
		t.Fatalf("task state = %+v", events[0].Outcome.TaskState)
	}
	if len(events[0].Outcome.TaskState.ExpectedOutputs) != 1 || events[0].Outcome.TaskState.ExpectedOutputs[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("task state expected outputs = %#v", events[0].Outcome.TaskState.ExpectedOutputs)
	}
}

func TestWorkerRunEventRecorderSyncsRunningRecordFromLiveTaskState(t *testing.T) {
	store := NewInMemoryRunEventStore()
	threads := NewInMemoryThreadStateStore()
	snapshots := NewInMemoryRunStore()
	threads.SetThreadMetadata("thread-worker-live", DefaultTaskStateMetadataKey, harness.TaskState{
		Items: []harness.TaskItem{
			{ID: "task-1", Text: "delegate research", Status: harness.TaskStatusInProgress},
		},
		ExpectedOutputs: []string{"/mnt/user-data/outputs/report.md"},
	}.Value())

	recorder := NewWorkerRunEventRecorderWithRuntime(store, threads, snapshots)
	plan := WorkerExecutionPlan{
		RunID:    "run-worker-live",
		ThreadID: "thread-worker-live",
		Attempt:  1,
	}

	recorder.RecordTaskEvent(plan, subagent.TaskEvent{
		Type:        "task_running",
		TaskID:      "task-1",
		Description: "delegate research",
	})

	record, ok := NewSnapshotStoreService(snapshots).LoadRecord(plan.RunID)
	if !ok {
		t.Fatal("LoadRecord() ok = false, want true")
	}
	if record.Status != "running" || record.Outcome.RunStatus != "running" {
		t.Fatalf("record = %+v", record)
	}
	if len(record.Outcome.TaskState.Items) != 1 || record.Outcome.TaskState.Items[0].ID != "task-1" {
		t.Fatalf("record outcome task state = %+v", record.Outcome.TaskState)
	}
	if len(record.Outcome.TaskLifecycle.ExpectedArtifacts) != 1 || record.Outcome.TaskLifecycle.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("record outcome lifecycle = %+v", record.Outcome.TaskLifecycle)
	}
}

type workerContextRuntimeStub struct {
	spec harness.ContextSpec
}

func (s workerContextRuntimeStub) ResolveWorkerAgentSpec(_ string, spec PortableAgentSpec) harness.AgentSpec {
	return spec.AgentSpec()
}

func (s workerContextRuntimeStub) ResolveWorkerContextSpec(_ string) harness.ContextSpec {
	return s.spec
}

func TestBindWorkerExecutionContextMergesResolvedHooks(t *testing.T) {
	var taskCount int
	var clarifyCount int
	specs := workerContextRuntimeStub{
		spec: harness.ContextSpec{
			ThreadID: "thread-worker-bind",
			Hooks: harness.RunHooks{
				TaskSink: func(evt subagent.TaskEvent) {
					if evt.Type == "task_running" {
						taskCount++
					}
				},
				ClarificationSink: func(item *clarification.Clarification) {
					if item != nil && item.ID == "clarify-1" {
						clarifyCount++
					}
				},
			},
		},
	}
	store := NewInMemoryRunEventStore()
	ctx := bindWorkerExecutionContext(context.Background(), nil, specs, WorkerExecutionPlan{
		RunID:    "run-worker-bind",
		ThreadID: "thread-worker-bind",
	}, NewWorkerRunEventRecorder(store))

	subagent.EmitEvent(ctx, subagent.TaskEvent{Type: "task_running", TaskID: "task-1"})
	clarification.EmitEvent(ctx, &clarification.Clarification{ID: "clarify-1", ThreadID: "thread-worker-bind"})

	if taskCount != 1 {
		t.Fatalf("taskCount = %d, want 1", taskCount)
	}
	if clarifyCount != 1 {
		t.Fatalf("clarifyCount = %d, want 1", clarifyCount)
	}
	events := store.LoadRunEvents("run-worker-bind")
	if len(events) != 2 || events[0].Event != "task_running" || events[1].Event != "clarification_request" {
		t.Fatalf("events = %#v", events)
	}
}
