package harness

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
)

func TestWithFeatureBuilderAppliesBundle(t *testing.T) {
	var afterCalled bool
	runtime := NewRuntime(RuntimeDeps{}, nil,
		WithFeatureBuilder(NewStaticFeatureBuilder(
			FeatureAssembly{
				Memory:        MemoryFeature{Enabled: true},
				Summarization: SummarizationFeature{Enabled: true},
			},
			&LifecycleHooks{
				AfterRun: []AfterRunHook{
					func(_ context.Context, state *RunState, result *agent.RunResult) error {
						afterCalled = true
						state.Metadata["after"] = "ok"
						return nil
					},
				},
			},
		)),
	)

	features := runtime.Features()
	if !features.Memory.Enabled || !features.Summarization.Enabled {
		t.Fatalf("features = %#v", features)
	}

	state := &RunState{Metadata: map[string]any{}}
	if err := runtime.AfterRun(context.Background(), state, &agent.RunResult{}); err != nil {
		t.Fatalf("AfterRun() error = %v", err)
	}
	if !afterCalled || state.Metadata["after"] != "ok" {
		t.Fatalf("lifecycle metadata = %#v", state.Metadata)
	}
}

func TestWithProfileBuilderAppliesProfile(t *testing.T) {
	policy := agent.DefaultRunPolicy()
	var afterCalled bool
	runtime := NewRuntime(RuntimeDeps{}, nil,
		WithProfileBuilder(NewStaticProfileBuilder(RuntimeProfile{
			RunPolicy: policy,
			Features: FeatureAssembly{
				Memory: MemoryFeature{Enabled: true},
				Title:  TitleFeature{Enabled: true},
			},
			Lifecycle: &LifecycleHooks{
				AfterRun: []AfterRunHook{
					func(_ context.Context, state *RunState, _ *agent.RunResult) error {
						afterCalled = true
						state.Metadata["profile"] = "ok"
						return nil
					},
				},
			},
		})),
	)

	if runtime.Profile().RunPolicy != policy {
		t.Fatal("runtime profile run policy not retained")
	}
	features := runtime.Features()
	if !features.Memory.Enabled || !features.Title.Enabled {
		t.Fatalf("features = %#v", features)
	}
	state := &RunState{Metadata: map[string]any{}}
	if err := runtime.AfterRun(context.Background(), state, &agent.RunResult{}); err != nil {
		t.Fatalf("AfterRun() error = %v", err)
	}
	if !afterCalled || state.Metadata["profile"] != "ok" {
		t.Fatalf("metadata = %#v", state.Metadata)
	}
}
