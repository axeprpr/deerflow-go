package harnessruntime

import (
	"context"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type fakeDispatcher struct {
	called bool
	req    DispatchRequest
	plan   RunPlan
}

func (d *fakeDispatcher) Dispatch(_ context.Context, req DispatchRequest) (*DispatchResult, error) {
	d.called = true
	d.req = req
	d.plan = RunPlan{
		ThreadID:         req.Plan.ThreadID,
		AssistantID:      req.Plan.AssistantID,
		RunID:            req.Plan.RunID,
		SubmittedAt:      req.Plan.SubmittedAt,
		Attempt:          req.Plan.Attempt,
		ResumeFromEvent:  req.Plan.ResumeFromEvent,
		ResumeReason:     req.Plan.ResumeReason,
		Model:            req.Plan.Model,
		AgentName:        req.Plan.AgentName,
		Spec:             req.Plan.Spec.AgentSpec(),
		Features:         req.Plan.Features,
		ExistingMessages: append([]models.Message(nil), req.Plan.ExistingMessages...),
		Messages:         append([]models.Message(nil), req.Plan.Messages...),
	}
	return &DispatchResult{
		Lifecycle: &harness.RunState{
			ThreadID:         req.Plan.ThreadID,
			AssistantID:      req.Plan.AssistantID,
			Model:            req.Plan.Model,
			AgentName:        req.Plan.AgentName,
			Spec:             req.Plan.Spec.AgentSpec(),
			ExistingMessages: append([]models.Message(nil), req.Plan.ExistingMessages...),
			Messages:         append([]models.Message(nil), req.Plan.Messages...),
			Metadata:         map[string]any{},
		},
		Handle:    NewStaticExecutionHandle(&harness.Execution{}, req.Plan.ThreadID),
		Execution: ExecutionDescriptor{Kind: ExecutionKindLocalPrepared, SessionID: req.Plan.ThreadID},
	}, nil
}

type coordinatorPreflightRuntime struct{}

func (coordinatorPreflightRuntime) PrepareSession(threadID string) SessionSnapshot {
	return SessionSnapshot{
		ExistingMessages: []models.Message{{
			Role:      models.RoleHuman,
			Content:   "existing",
			SessionID: threadID,
		}},
	}
}
func (coordinatorPreflightRuntime) MarkThreadStatus(string, string)       {}
func (coordinatorPreflightRuntime) SetThreadMetadata(string, string, any) {}
func (coordinatorPreflightRuntime) ClearThreadMetadata(string, string)    {}
func (coordinatorPreflightRuntime) SaveRunRecord(RunRecord)               {}

func TestCoordinatorUsesInjectedDispatcher(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	coordinator := NewCoordinator(CoordinatorDeps{
		Dispatcher: dispatcher,
		Preflight:  coordinatorPreflightRuntime{},
	})
	preflight := NewPreflightService(coordinatorPreflightRuntime{})
	preflight.now = func() time.Time { return time.Unix(1, 0).UTC() }
	preflight.newID = func() string { return "run-1" }
	coordinator.preflight = preflight

	prepared, err := coordinator.Prepare(context.Background(), PreflightInput{
		RouteThreadID: "thread-1",
		AssistantID:   "lead_agent",
		NewMessages: []models.Message{{
			Role:      models.RoleHuman,
			Content:   "new",
			SessionID: "thread-1",
		}},
	}, RunPlan{
		Model:     "model-1",
		AgentName: "lead_agent",
		Spec:      harness.AgentSpec{},
		Features:  harness.FeatureSet{Sandbox: true},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if !dispatcher.called {
		t.Fatal("dispatcher was not called")
	}
	if dispatcher.plan.ThreadID != "thread-1" || dispatcher.plan.AssistantID != "lead_agent" {
		t.Fatalf("dispatcher plan = %#v", dispatcher.plan)
	}
	if prepared.Run.RunID != "run-1" {
		t.Fatalf("prepared run id = %q, want run-1", prepared.Run.RunID)
	}
}

func TestCoordinatorSubmitUsesInjectedDispatcher(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	coordinator := NewCoordinator(CoordinatorDeps{
		Dispatcher: dispatcher,
	})

	prepared, err := coordinator.Submit(context.Background(), RunPlan{
		ThreadID:    "thread-1",
		AssistantID: "lead_agent",
		Model:       "model-1",
		AgentName:   "lead_agent",
		Spec:        harness.AgentSpec{},
		Features:    harness.FeatureSet{Sandbox: true},
		Messages: []models.Message{{
			Role:      models.RoleHuman,
			Content:   "new",
			SessionID: "thread-1",
		}},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if !dispatcher.called {
		t.Fatal("dispatcher was not called")
	}
	if prepared == nil || prepared.Handle == nil || prepared.Lifecycle == nil {
		t.Fatalf("prepared = %#v", prepared)
	}
	if _, err := prepared.Handle.Resolve(); err != nil {
		t.Fatalf("prepared.Handle.Resolve() error = %v", err)
	}
	if prepared.Execution.Kind != ExecutionKindLocalPrepared {
		t.Fatalf("prepared.Execution = %#v", prepared.Execution)
	}
	if dispatcher.plan.ThreadID != "thread-1" || dispatcher.plan.AssistantID != "lead_agent" {
		t.Fatalf("dispatcher plan = %#v", dispatcher.plan)
	}
}

func TestCoordinatorResumeSubmitsRecoveredPlan(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	runState := &fakeRunStateRuntime{}
	coordinator := NewCoordinator(CoordinatorDeps{
		Dispatcher: dispatcher,
		RunState:   runState,
	})
	coordinator.runState.now = func() time.Time { return time.Unix(20, 0).UTC() }

	record, result, err := coordinator.Resume(context.Background(), RunPlan{
		Model:     "model-1",
		AgentName: "lead_agent",
		Spec:      harness.AgentSpec{},
		Features:  harness.FeatureSet{Sandbox: true},
		Messages: []models.Message{{
			Role:      models.RoleHuman,
			Content:   "resume",
			SessionID: "thread-1",
		}},
	}, RunRecord{
		RunID:       "run-1",
		ThreadID:    "thread-1",
		AssistantID: "lead_agent",
		Attempt:     1,
		Status:      "interrupted",
		Outcome:     RunOutcomeDescriptor{RunStatus: "interrupted", Attempt: 1},
	}, 7, "worker-retry")
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if result == nil || result.Handle == nil || result.Lifecycle == nil {
		t.Fatalf("result = %#v", result)
	}
	if !dispatcher.called {
		t.Fatal("dispatcher was not called")
	}
	if record.Attempt != 2 || record.ResumeFromEvent != 7 || record.ResumeReason != "worker-retry" {
		t.Fatalf("record = %+v", record)
	}
	if record.Status != "running" || record.Outcome.RunStatus != "running" {
		t.Fatalf("record outcome = %+v", record)
	}
	if dispatcher.plan.RunID != "run-1" || dispatcher.plan.Attempt != 2 || dispatcher.plan.ResumeFromEvent != 7 || dispatcher.plan.ResumeReason != "worker-retry" {
		t.Fatalf("dispatcher plan = %#v", dispatcher.plan)
	}
	if runState.threadStatus != "thread-1:busy" {
		t.Fatalf("runState threadStatus = %q", runState.threadStatus)
	}
	if runState.saved.Attempt != 2 || runState.saved.Status != "running" {
		t.Fatalf("runState saved = %+v", runState.saved)
	}
}

func TestCoordinatorResumeSkipsDispatchForIdempotentRecoveryContext(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	runState := &fakeRunStateRuntime{}
	coordinator := NewCoordinator(CoordinatorDeps{
		Dispatcher: dispatcher,
		RunState:   runState,
	})
	coordinator.runState.now = func() time.Time { return time.Unix(25, 0).UTC() }

	record, result, err := coordinator.Resume(context.Background(), RunPlan{
		Model:     "model-1",
		AgentName: "lead_agent",
		Spec:      harness.AgentSpec{},
		Features:  harness.FeatureSet{Sandbox: true},
		Messages: []models.Message{{
			Role:      models.RoleHuman,
			Content:   "resume",
			SessionID: "thread-1",
		}},
	}, RunRecord{
		RunID:           "run-1",
		ThreadID:        "thread-1",
		AssistantID:     "lead_agent",
		Attempt:         2,
		Status:          "running",
		ResumeFromEvent: 7,
		ResumeReason:    "worker-retry",
		Outcome:         RunOutcomeDescriptor{RunStatus: "running", Attempt: 2},
	}, 7, "worker-retry")
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil for idempotent recovery", result)
	}
	if dispatcher.called {
		t.Fatal("dispatcher was called for idempotent recovery")
	}
	if record.Attempt != 2 || record.ResumeFromEvent != 7 || record.ResumeReason != "worker-retry" {
		t.Fatalf("record = %+v", record)
	}
	if runState.saved.Attempt != 2 || runState.saved.Status != "running" {
		t.Fatalf("runState saved = %+v", runState.saved)
	}
	if runState.threadStatus != "thread-1:busy" {
		t.Fatalf("runState threadStatus = %q", runState.threadStatus)
	}
}

func TestCoordinatorFinalizePreservesTaskLifecycleDescriptor(t *testing.T) {
	t.Parallel()

	runState := &fakeRunStateRuntime{}
	completion := &fakeCompletionRuntime{}
	coordinator := NewCoordinator(CoordinatorDeps{
		RunState:   runState,
		Completion: completion,
	})
	coordinator.runState.now = func() time.Time { return time.Unix(30, 0).UTC() }

	result := coordinator.Finalize(context.Background(), "thread-1", &harness.RunState{
		ThreadID: "thread-1",
		TaskState: harness.TaskState{
			Items: []harness.TaskItem{{Text: "verify artifact", Status: harness.TaskStatusPending}},
		},
	}, &agent.RunResult{}, RunRecord{
		RunID:     "run-1",
		ThreadID:  "thread-1",
		Attempt:   2,
		Status:    "running",
		Outcome:   RunOutcomeDescriptor{RunStatus: "running", Attempt: 2},
		CreatedAt: time.Unix(1, 0).UTC(),
		UpdatedAt: time.Unix(1, 0).UTC(),
	})

	if result.Outcome.RunStatus != "incomplete" {
		t.Fatalf("Outcome.RunStatus=%q want incomplete", result.Outcome.RunStatus)
	}
	if result.Outcome.TaskLifecycle.Status != "incomplete" {
		t.Fatalf("TaskLifecycle=%+v", result.Outcome.TaskLifecycle)
	}
	if len(result.Outcome.TaskLifecycle.PendingTasks) != 1 {
		t.Fatalf("TaskLifecycle=%+v", result.Outcome.TaskLifecycle)
	}
	if result.Outcome.Attempt != 2 {
		t.Fatalf("Outcome recovery fields=%+v", result.Outcome)
	}
	if runState.saved.Outcome.TaskLifecycle.Status != "incomplete" {
		t.Fatalf("saved outcome=%+v", runState.saved.Outcome)
	}
}

func TestCoordinatorFinalizeSkipsThreadMutationsForStaleRun(t *testing.T) {
	t.Parallel()

	threads := NewInMemoryThreadStateStore()
	threads.MarkThreadStatus("thread-1", "busy")
	threads.SetThreadMetadata("thread-1", DefaultRunIDMetadataKey, "run-b")
	threads.SetThreadMetadata("thread-1", DefaultActiveRunMetadataKey, "run-b")
	threads.SetThreadMetadata("thread-1", DefaultTaskLifecycleMetadataKey, TaskLifecycleDescriptor{
		Status: "running",
	}.Value())

	snapshots := NewInMemoryRunStore()
	coordinator := NewCoordinator(CoordinatorDeps{
		RunState: workerRunStateRuntime{
			snapshots: snapshots,
			threads:   threads,
		},
		Completion: workerCompletionRuntime{
			threads: threads,
		},
	})
	coordinator.runState.now = func() time.Time { return time.Unix(40, 0).UTC() }

	result := coordinator.Finalize(context.Background(), "thread-1", &harness.RunState{
		ThreadID: "thread-1",
	}, &agent.RunResult{}, RunRecord{
		RunID:     "run-a",
		ThreadID:  "thread-1",
		Status:    "running",
		Attempt:   1,
		CreatedAt: time.Unix(1, 0).UTC(),
		UpdatedAt: time.Unix(1, 0).UTC(),
		Outcome:   RunOutcomeDescriptor{RunStatus: "running", Attempt: 1},
	})

	if result.Run.RunID != "run-a" {
		t.Fatalf("result run = %+v", result.Run)
	}
	threadState, ok := threads.LoadThreadRuntimeState("thread-1")
	if !ok {
		t.Fatal("thread state missing after stale finalize")
	}
	if got := threadState.Status; got != "busy" {
		t.Fatalf("thread status after stale finalize = %q, want busy", got)
	}
	if got := metadataRunID(threadState.Metadata[DefaultActiveRunMetadataKey]); got != "run-b" {
		t.Fatalf("active run after stale finalize = %q, want run-b", got)
	}
	lifecycle, ok := ParseTaskLifecycle(threadState.Metadata[DefaultTaskLifecycleMetadataKey])
	if !ok || lifecycle.Status != "running" {
		t.Fatalf("task lifecycle after stale finalize = %#v", threadState.Metadata[DefaultTaskLifecycleMetadataKey])
	}
	if _, saved := NewSnapshotStoreService(snapshots).LoadRecord("run-a"); !saved {
		t.Fatal("stale run record was not persisted")
	}
}
