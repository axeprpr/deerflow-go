package harnessruntime

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

type CoordinatorDeps struct {
	Runtime      *harness.Runtime
	Dispatcher   RunDispatcher
	Preflight    PreflightRuntime
	Context      ContextRuntime
	RunState     RunStateRuntime
	Completion   CompletionRuntime
	Coordination CoordinationRuntime
	Query        QueryRuntime
	TitleKey     string
	InterruptKey string
}

type PreparedRun struct {
	ThreadID         string
	AssistantID      string
	PresentFiles     *tools.PresentFileRegistry
	ExistingMessages []models.Message
	Messages         []models.Message
	Lifecycle        *harness.RunState
	Run              RunRecord
	Execution        ExecutionHandle
	ExecutionDesc    ExecutionDescriptor
}

type CompletionResult struct {
	Run         RunRecord
	Interrupted bool
	Outcome     RunOutcomeDescriptor
}

type Coordinator struct {
	runtime      *harness.Runtime
	dispatcher   RunDispatcher
	recovery     RecoveryPlanner
	preflight    PreflightService
	context      ContextService
	runState     RunStateService
	completion   CompletionService
	coordination CoordinationService
	query        QueryService
}

func NewCoordinator(deps CoordinatorDeps) Coordinator {
	return Coordinator{
		runtime:      deps.Runtime,
		dispatcher:   deps.Dispatcher,
		recovery:     NewRecoveryPlanner(),
		preflight:    NewPreflightService(deps.Preflight),
		context:      NewContextService(deps.Context, deps.Runtime),
		runState:     NewRunStateService(deps.RunState),
		completion:   NewCompletionService(deps.Completion, deps.TitleKey, deps.InterruptKey),
		coordination: NewCoordinationService(deps.Coordination),
		query:        NewQueryService(deps.Query),
	}
}

func (c Coordinator) Prepare(ctx context.Context, input PreflightInput, plan RunPlan) (*PreparedRun, error) {
	preflight := c.preflight.Prepare(input)
	orchestrated, err := c.Submit(ctx, plan.WithMessages(preflight.ThreadID, preflight.AssistantID, preflight.ExistingMessages, preflight.Messages))
	if err != nil {
		return nil, err
	}
	return &PreparedRun{
		ThreadID:         preflight.ThreadID,
		AssistantID:      preflight.AssistantID,
		PresentFiles:     preflight.PresentFiles,
		ExistingMessages: append([]models.Message(nil), preflight.ExistingMessages...),
		Messages:         append([]models.Message(nil), orchestrated.Lifecycle.Messages...),
		Lifecycle:        orchestrated.Lifecycle,
		Run:              preflight.Run,
		Execution:        orchestrated.Handle,
		ExecutionDesc:    orchestrated.Execution,
	}, nil
}

func (c Coordinator) Submit(ctx context.Context, plan RunPlan) (*DispatchResult, error) {
	dispatcher := c.dispatcher
	if dispatcher == nil {
		dispatcher = NewInProcessRunDispatcher()
	}
	return dispatcher.Dispatch(ctx, DispatchRequest{
		Plan: NewWorkerExecutionPlan(plan),
	})
}

func (c Coordinator) ResumePlan(plan RunPlan, record RunRecord, afterEvent int, reason string) RunPlan {
	return c.recovery.ResumePlan(plan, record, afterEvent, reason)
}

func (c Coordinator) BindContext(ctx context.Context, threadID string, taskSink func(subagent.TaskEvent), clarificationSink func(*clarification.Clarification)) context.Context {
	return c.context.Bind(ctx, threadID, harness.RunHooks{
		TaskSink:          taskSink,
		ClarificationSink: clarificationSink,
	})
}

func (c Coordinator) MarkError(record RunRecord, err error) RunRecord {
	return c.runState.MarkError(record, err)
}

func (c Coordinator) MarkCanceled(record RunRecord) RunRecord {
	return c.runState.MarkCanceled(record)
}

func (c Coordinator) Finalize(ctx context.Context, threadID string, state *harness.RunState, result *agent.RunResult, record RunRecord) CompletionResult {
	if c.runtime != nil && state != nil {
		state.Messages = append([]models.Message(nil), result.Messages...)
		_ = c.runtime.AfterRun(ctx, state, result)
	}
	outcome := c.completion.Apply(threadID, state, result)
	outcome.Descriptor = NewOutcomeService().Describe(record, outcome.RunOutcome, "")
	return CompletionResult{
		Run:         c.runState.Finalize(record, outcome),
		Interrupted: outcome.Interrupted,
		Outcome:     outcome.Descriptor,
	}
}

func (c Coordinator) Run(threadID, runID string) (RunRecord, bool) {
	return c.query.Run(threadID, runID)
}

func (c Coordinator) ListThreadRuns(threadID string) []RunRecord {
	return c.query.ListThreadRuns(threadID)
}

func (c Coordinator) Wait(ctx context.Context, threadID, runID string, cancelOnDisconnect bool) (RunRecord, bool, bool) {
	return c.coordination.Wait(ctx, threadID, runID, cancelOnDisconnect)
}

func (c Coordinator) Cancel(threadID, runID string) (map[string]any, bool, bool) {
	return c.coordination.Cancel(threadID, runID)
}

func (p RunPlan) WithMessages(threadID, assistantID string, existingMessages, messages []models.Message) RunPlan {
	p.ThreadID = threadID
	p.AssistantID = assistantID
	p.ExistingMessages = append([]models.Message(nil), existingMessages...)
	p.Messages = append([]models.Message(nil), messages...)
	return p
}
