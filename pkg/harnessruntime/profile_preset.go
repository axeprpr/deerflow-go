package harnessruntime

type ExecutionMode string

const (
	ExecutionModeDefault     ExecutionMode = ""
	ExecutionModeInteractive ExecutionMode = "interactive"
	ExecutionModeBackground  ExecutionMode = "background"
	ExecutionModeStrict      ExecutionMode = "strict"
)

type ProfilePresetOptions struct {
	ClarificationEnabled bool
	MemoryEnabled        bool
	TitleEnabled         bool
}

func DefaultLifecycleConfig() LifecycleConfig {
	return LifecycleConfig{
		SummaryMetadataKey:   "history_summary",
		MemorySessionKey:     "memory_session_id",
		InterruptMetadataKey: "clarification_interrupt",
		TitleMetadataKey:     "generated_title",
		TaskStateMetadataKey: DefaultTaskStateMetadataKey,
	}
}

func ProfilePreset(mode ExecutionMode, opts ProfilePresetOptions) ProfileConfig {
	if mode == ExecutionModeDefault {
		mode = ExecutionModeInteractive
	}
	config := ProfileConfig{
		Mode: mode,
		Features: FeatureConfig{
			ClarificationEnabled: opts.ClarificationEnabled,
			MemoryEnabled:        opts.MemoryEnabled,
			SummarizationEnabled: true,
			TitleEnabled:         opts.TitleEnabled,
		},
		Lifecycle: DefaultLifecycleConfig(),
		RunPolicy: NewDefaultRunPolicy(),
	}
	switch mode {
	case ExecutionModeBackground:
		config.Features.ClarificationEnabled = false
	case ExecutionModeStrict:
		config.Features.SummarizationEnabled = false
		config.Features.TitleEnabled = false
	}
	return config
}
