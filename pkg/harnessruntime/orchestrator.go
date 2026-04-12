package harnessruntime

import (
	"context"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type RunPlan struct {
	ThreadID         string
	AssistantID      string
	RunID            string
	SubmittedAt      time.Time
	Attempt          int
	ResumeFromEvent  int
	ResumeReason     string
	Model            string
	AgentName        string
	Spec             harness.AgentSpec
	Features         harness.FeatureSet
	ExistingMessages []models.Message
	Messages         []models.Message
}

type WorkerExecutionPlan struct {
	ThreadID         string
	AssistantID      string
	RunID            string
	SubmittedAt      time.Time
	Attempt          int
	ResumeFromEvent  int
	ResumeReason     string
	Model            string
	AgentName        string
	Spec             PortableAgentSpec
	Features         harness.FeatureSet
	ExistingMessages []models.Message
	Messages         []models.Message
}

type PreparedExecution struct {
	Lifecycle *harness.RunState
	Execution *harness.Execution
}

type Orchestrator struct {
	runtime *harness.Runtime
	specs   WorkerSpecRuntime
}

func NewOrchestrator(runtime *harness.Runtime, specs WorkerSpecRuntime) Orchestrator {
	return Orchestrator{runtime: runtime, specs: specs}
}

func NewWorkerExecutionPlan(plan RunPlan) WorkerExecutionPlan {
	return WorkerExecutionPlan{
		ThreadID:         plan.ThreadID,
		AssistantID:      plan.AssistantID,
		RunID:            plan.RunID,
		SubmittedAt:      plan.SubmittedAt,
		Attempt:          plan.Attempt,
		ResumeFromEvent:  plan.ResumeFromEvent,
		ResumeReason:     plan.ResumeReason,
		Model:            plan.Model,
		AgentName:        plan.AgentName,
		Spec:             PortableAgentSpecFromAgentSpec(plan.Spec),
		Features:         plan.Features,
		ExistingMessages: append([]models.Message(nil), plan.ExistingMessages...),
		Messages:         append([]models.Message(nil), plan.Messages...),
	}
}

func (o Orchestrator) Prepare(ctx context.Context, plan RunPlan) (*PreparedExecution, error) {
	return o.PrepareExecution(ctx, NewWorkerExecutionPlan(plan))
}

func (o Orchestrator) PrepareExecution(ctx context.Context, plan WorkerExecutionPlan) (*PreparedExecution, error) {
	spec := plan.Spec.AgentSpec()
	if o.specs != nil {
		spec = o.specs.ResolveWorkerAgentSpec(plan.ThreadID, plan.Spec)
	}
	lifecycle := &harness.RunState{
		ThreadID:         plan.ThreadID,
		AssistantID:      plan.AssistantID,
		Model:            plan.Model,
		AgentName:        plan.AgentName,
		Spec:             spec,
		ExistingMessages: append([]models.Message(nil), plan.ExistingMessages...),
		Messages:         append([]models.Message(nil), plan.Messages...),
		Metadata:         map[string]any{},
	}
	if plan.RunID != "" {
		lifecycle.Metadata["run_id"] = plan.RunID
	}
	if !plan.SubmittedAt.IsZero() {
		lifecycle.Metadata["submitted_at"] = plan.SubmittedAt.UTC().Format(time.RFC3339Nano)
	}
	if plan.Attempt > 0 {
		lifecycle.Metadata["submission_attempt"] = plan.Attempt
	}
	if plan.ResumeFromEvent > 0 {
		lifecycle.Metadata["resume_from_event_index"] = plan.ResumeFromEvent
	}
	if plan.ResumeReason != "" {
		lifecycle.Metadata["resume_reason"] = plan.ResumeReason
	}
	if o.runtime != nil {
		if err := o.runtime.BeforeRun(ctx, lifecycle); err != nil {
			return nil, err
		}
	}

	var execution *harness.Execution
	var err error
	if o.runtime != nil {
		execution, err = o.runtime.PrepareRun(harness.RunRequest{
			Agent: harness.AgentRequest{
				Spec:     lifecycle.Spec,
				Features: plan.Features,
			},
			SessionID: plan.ThreadID,
			Messages:  lifecycle.Messages,
		})
	} else {
		execution, err = harness.NewRunner(nil).Prepare(harness.RunRequest{
			Agent: harness.AgentRequest{
				Spec:     lifecycle.Spec,
				Features: plan.Features,
			},
			SessionID: plan.ThreadID,
			Messages:  lifecycle.Messages,
		})
	}
	if err != nil {
		return nil, err
	}

	return &PreparedExecution{
		Lifecycle: lifecycle,
		Execution: execution,
	}, nil
}
