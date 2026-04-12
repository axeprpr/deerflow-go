package harnessruntime

import "testing"

func TestProfilePresetInteractive(t *testing.T) {
	config := ProfilePreset(ExecutionModeInteractive, ProfilePresetOptions{
		ClarificationEnabled: true,
		MemoryEnabled:        true,
	})
	if !config.Features.ClarificationEnabled || !config.Features.MemoryEnabled || !config.Features.SummarizationEnabled {
		t.Fatalf("interactive features = %#v", config.Features)
	}
	if config.RunPolicy == nil {
		t.Fatal("interactive preset should configure run policy")
	}
	if config.Lifecycle.InterruptMetadataKey != "clarification_interrupt" {
		t.Fatalf("interrupt key = %q", config.Lifecycle.InterruptMetadataKey)
	}
}

func TestProfilePresetBackgroundDisablesClarification(t *testing.T) {
	config := ProfilePreset(ExecutionModeBackground, ProfilePresetOptions{
		ClarificationEnabled: true,
		MemoryEnabled:        true,
	})
	if config.Features.ClarificationEnabled {
		t.Fatal("background preset should disable clarification")
	}
	if !config.Features.SummarizationEnabled {
		t.Fatal("background preset should keep summarization enabled")
	}
}

func TestProfilePresetStrictDisablesDerivedFeatures(t *testing.T) {
	config := ProfilePreset(ExecutionModeStrict, ProfilePresetOptions{
		ClarificationEnabled: true,
		MemoryEnabled:        true,
		TitleEnabled:         true,
	})
	if !config.Features.ClarificationEnabled {
		t.Fatal("strict preset should preserve clarification")
	}
	if config.Features.SummarizationEnabled {
		t.Fatal("strict preset should disable summarization")
	}
	if config.Features.TitleEnabled {
		t.Fatal("strict preset should disable title generation")
	}
}
