package harnessruntime

import (
	"context"
	"testing"
	"time"

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
	d.plan = req.Plan
	return &DispatchResult{
		Lifecycle: &harness.RunState{
			ThreadID:         req.Plan.ThreadID,
			AssistantID:      req.Plan.AssistantID,
			Model:            req.Plan.Model,
			AgentName:        req.Plan.AgentName,
			Spec:             req.Plan.Spec,
			ExistingMessages: append([]models.Message(nil), req.Plan.ExistingMessages...),
			Messages:         append([]models.Message(nil), req.Plan.Messages...),
			Metadata:         map[string]any{},
		},
		Execution: &harness.Execution{},
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
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if !dispatcher.called {
		t.Fatal("dispatcher was not called")
	}
	if dispatcher.req.Runtime != nil {
		t.Fatalf("dispatcher runtime = %#v, want nil", dispatcher.req.Runtime)
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
	if dispatcher.req.Runtime != nil {
		t.Fatalf("dispatcher runtime = %#v, want nil", dispatcher.req.Runtime)
	}
	if prepared == nil || prepared.Execution == nil || prepared.Lifecycle == nil {
		t.Fatalf("prepared = %#v", prepared)
	}
	if dispatcher.plan.ThreadID != "thread-1" || dispatcher.plan.AssistantID != "lead_agent" {
		t.Fatalf("dispatcher plan = %#v", dispatcher.plan)
	}
}
