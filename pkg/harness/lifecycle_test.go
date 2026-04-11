package harness

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
)

func TestRuntimeCarriesFeatureAssemblyAndLifecycleHooks(t *testing.T) {
	manager := clarification.NewManager(8)
	var beforeCalled bool
	var afterCalled bool

	runtime := NewRuntime(RuntimeDeps{}, nil,
		WithFeatureAssembly(FeatureAssembly{
			Clarification: ClarificationFeature{Enabled: true, Manager: manager},
			Memory:        MemoryFeature{Enabled: true},
			Summarization: SummarizationFeature{Enabled: true},
			Title:         TitleFeature{Enabled: true},
		}),
		WithLifecycle(&LifecycleHooks{
			BeforeRun: []BeforeRunHook{
				func(_ context.Context, state *RunState) error {
					beforeCalled = true
					state.Metadata["before"] = "ok"
					return nil
				},
			},
			AfterRun: []AfterRunHook{
				func(_ context.Context, state *RunState, result *agent.RunResult) error {
					afterCalled = true
					result.FinalOutput = "after"
					state.Metadata["after"] = "ok"
					return nil
				},
			},
		}),
	)

	features := runtime.Features()
	if !features.Clarification.Enabled || features.Clarification.Manager != manager {
		t.Fatal("clarification feature not retained on runtime")
	}
	if !features.Memory.Enabled || !features.Summarization.Enabled || !features.Title.Enabled {
		t.Fatal("feature assembly not retained on runtime")
	}

	state := &RunState{Metadata: map[string]any{}}
	result := &agent.RunResult{}
	if err := runtime.BeforeRun(context.Background(), state); err != nil {
		t.Fatalf("BeforeRun() error = %v", err)
	}
	if err := runtime.AfterRun(context.Background(), state, result); err != nil {
		t.Fatalf("AfterRun() error = %v", err)
	}
	if !beforeCalled || !afterCalled {
		t.Fatal("expected both lifecycle phases to run")
	}
	if result.FinalOutput != "after" {
		t.Fatalf("FinalOutput = %q, want %q", result.FinalOutput, "after")
	}
	if state.Metadata["before"] != "ok" || state.Metadata["after"] != "ok" {
		t.Fatalf("metadata = %#v", state.Metadata)
	}
}
