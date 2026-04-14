package harnessruntime

import (
	"context"
	"errors"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
)

type RunExecutor interface {
	Execute(context.Context, DispatchRequest) (*DispatchResult, error)
}

type RuntimeWorker struct {
	runtime   func() *harness.Runtime
	specs     WorkerSpecRuntime
	complete  bool
	events    RunEventRecorder
	snapshots RunSnapshotStore
	threads   ThreadStateStore
}

func NewRuntimeWorker() RunExecutor {
	return RuntimeWorker{}
}

func NewRuntimeWorkerSource(source func() *harness.Runtime, specs WorkerSpecRuntime) RunExecutor {
	return RuntimeWorker{runtime: source, specs: specs}
}

func NewCompletingRuntimeWorkerSource(source func() *harness.Runtime, specs WorkerSpecRuntime, events RunEventRecorder, snapshots RunSnapshotStore, threads ThreadStateStore) RunExecutor {
	return RuntimeWorker{runtime: source, specs: specs, complete: true, events: events, snapshots: snapshots, threads: threads}
}

func (w RuntimeWorker) Execute(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	return w.execute(ctx, req)
}

func (w RuntimeWorker) execute(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	var runtime *harness.Runtime
	if w.runtime != nil {
		runtime = w.runtime()
	}
	if runtime == nil {
		return nil, errors.New("runtime is required")
	}
	prepared, err := NewOrchestrator(runtime, w.specs).PrepareExecution(ctx, req.Plan)
	if err != nil {
		return nil, err
	}
	handle := NewStaticExecutionHandle(prepared.Execution, prepared.Lifecycle.ThreadID)
	if w.complete {
		recorder := NewWorkerRunEventRecorderWithRuntime(w.events, w.threads, w.snapshots)
		ctx = bindWorkerExecutionContextWithTracker(ctx, runtime, w.specs, req.Plan, recorder, NewThreadTaskLifecycleTracker(w.threads, req.Plan.ThreadID))
		runState := NewRunStateService(workerRunStateRuntime{
			snapshots: w.snapshots,
			threads:   w.threads,
		})
		completion := NewCompletionService(workerCompletionRuntime{threads: w.threads}, "generated_title", "clarification_interrupt")
		record := runState.Begin(loadWorkerRunRecord(req.Plan, w.snapshots))
		eventsDone := make(chan struct{})
		go func() {
			defer close(eventsDone)
			for evt := range prepared.Execution.Events() {
				recorder.RecordAgentEvent(req.Plan, evt)
			}
		}()
		result, err := prepared.Execution.Run(ctx)
		<-eventsDone
		if err != nil {
			runState.MarkError(record, err)
			return nil, err
		}
		if runtime != nil && prepared.Lifecycle != nil {
			prepared.Lifecycle.Messages = append([]models.Message(nil), result.Messages...)
			_ = runtime.AfterRun(ctx, prepared.Lifecycle, result)
		}
		outcome := completion.Apply(req.Plan.ThreadID, prepared.Lifecycle, result)
		recorder.RecordCompletion(req.Plan, result, outcome.Descriptor)
		record = runState.Finalize(record, outcome)
		return &DispatchResult{
			Lifecycle: prepared.Lifecycle,
			Execution: ExecutionDescriptor{
				Kind:      ExecutionKindRemoteCompleted,
				SessionID: prepared.Lifecycle.ThreadID,
			},
			Completed: result,
		}, nil
	}
	return &DispatchResult{
		Lifecycle: prepared.Lifecycle,
		Handle:    handle,
		Execution: handle.Describe(),
	}, nil
}

func bindWorkerExecutionContext(ctx context.Context, runtime *harness.Runtime, specs WorkerSpecRuntime, plan WorkerExecutionPlan, recorder WorkerRunEventRecorder) context.Context {
	base := harness.ContextSpec{ThreadID: plan.ThreadID}
	if resolver, ok := specs.(WorkerContextRuntime); ok {
		base = resolver.ResolveWorkerContextSpec(plan.ThreadID)
		if base.ThreadID == "" {
			base.ThreadID = plan.ThreadID
		}
	}
	base.Hooks = mergeWorkerRunHooks(base.Hooks, harness.RunHooks{
		TaskSink: func(evt subagent.TaskEvent) {
			recorder.RecordTaskEvent(plan, evt)
		},
		ClarificationSink: func(item *clarification.Clarification) {
			recorder.RecordClarification(plan, item)
		},
	})
	if runtime != nil {
		return runtime.BindContext(ctx, base)
	}
	return harness.BindContext(ctx, base)
}

func bindWorkerExecutionContextWithTracker(ctx context.Context, runtime *harness.Runtime, specs WorkerSpecRuntime, plan WorkerExecutionPlan, recorder WorkerRunEventRecorder, tracker ThreadTaskLifecycleTracker) context.Context {
	base := harness.ContextSpec{ThreadID: plan.ThreadID}
	if resolver, ok := specs.(WorkerContextRuntime); ok {
		base = resolver.ResolveWorkerContextSpec(plan.ThreadID)
		if base.ThreadID == "" {
			base.ThreadID = plan.ThreadID
		}
	}
	base.Hooks = mergeWorkerRunHooks(base.Hooks, harness.RunHooks{
		TaskSink: func(evt subagent.TaskEvent) {
			tracker.Observe(evt)
			recorder.RecordTaskEvent(plan, evt)
		},
		ClarificationSink: func(item *clarification.Clarification) {
			recorder.RecordClarification(plan, item)
		},
	})
	if runtime != nil {
		return runtime.BindContext(ctx, base)
	}
	return harness.BindContext(ctx, base)
}

func mergeWorkerRunHooks(existing harness.RunHooks, extra harness.RunHooks) harness.RunHooks {
	return harness.RunHooks{
		TaskSink: func(evt subagent.TaskEvent) {
			if existing.TaskSink != nil {
				existing.TaskSink(evt)
			}
			if extra.TaskSink != nil {
				extra.TaskSink(evt)
			}
		},
		ClarificationSink: func(item *clarification.Clarification) {
			if existing.ClarificationSink != nil {
				existing.ClarificationSink(item)
			}
			if extra.ClarificationSink != nil {
				extra.ClarificationSink(item)
			}
		},
	}
}
