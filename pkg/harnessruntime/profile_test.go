package harnessruntime

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

func TestProfileConfigBuildProfile(t *testing.T) {
	memoryRuntime := &harness.MemoryRuntime{}
	profile := ProfileConfig{
		Features: FeatureConfig{
			ClarificationEnabled: true,
			MemoryEnabled:        true,
			SummarizationEnabled: true,
		},
		Lifecycle: LifecycleConfig{
			SummaryMetadataKey:   "summary",
			MemorySessionKey:     "memory_session_id",
			InterruptMetadataKey: "interrupt",
		},
	}.BuildProfile(ProfileProviders{
		ClarificationManager: clarification.NewManager(8),
		Lifecycle: LifecycleProviders{
			MemoryRuntime: memoryRuntime,
		},
	})

	if profile.RunPolicy == nil {
		t.Fatal("expected default run policy")
	}
	if !profile.Features.Clarification.Enabled || !profile.Features.Memory.Enabled || !profile.Features.Summarization.Enabled {
		t.Fatalf("features = %#v", profile.Features)
	}
	if profile.Lifecycle == nil {
		t.Fatal("expected lifecycle hooks")
	}
}
