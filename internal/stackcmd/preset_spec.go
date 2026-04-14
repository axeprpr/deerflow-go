package stackcmd

import "github.com/axeprpr/deerflow-go/internal/runtimecmd"

type PresetSpec struct {
	Preset                  StackPreset                  `json:"preset"`
	GatewayRuntimePreset    runtimecmd.RuntimeNodePreset `json:"gateway_runtime_preset"`
	WorkerRuntimePreset     runtimecmd.RuntimeNodePreset `json:"worker_runtime_preset"`
	StateRuntimePreset      runtimecmd.RuntimeNodePreset `json:"state_runtime_preset"`
	SandboxRuntimePreset    runtimecmd.RuntimeNodePreset `json:"sandbox_runtime_preset"`
	DedicatedStateService   bool                         `json:"dedicated_state_service"`
	DedicatedSandboxService bool                         `json:"dedicated_sandbox_service"`
}

func DefaultPresetSpec(preset StackPreset) PresetSpec {
	switch preset {
	case StackPresetSharedRemote:
		return PresetSpec{
			Preset:                  StackPresetSharedRemote,
			GatewayRuntimePreset:    runtimecmd.RuntimeNodePresetSharedRemote,
			WorkerRuntimePreset:     runtimecmd.RuntimeNodePresetSharedSQLite,
			StateRuntimePreset:      runtimecmd.RuntimeNodePresetSharedSQLite,
			SandboxRuntimePreset:    runtimecmd.RuntimeNodePresetFastLocal,
			DedicatedStateService:   true,
			DedicatedSandboxService: true,
		}
	case StackPresetSharedSQLite:
		return PresetSpec{
			Preset:               StackPresetSharedSQLite,
			GatewayRuntimePreset: runtimecmd.RuntimeNodePresetSharedSQLite,
			WorkerRuntimePreset:  runtimecmd.RuntimeNodePresetSharedSQLite,
			StateRuntimePreset:   runtimecmd.RuntimeNodePresetSharedSQLite,
			SandboxRuntimePreset: runtimecmd.RuntimeNodePresetFastLocal,
		}
	default:
		return PresetSpec{
			Preset:               StackPresetAuto,
			GatewayRuntimePreset: runtimecmd.RuntimeNodePresetSharedSQLite,
			WorkerRuntimePreset:  runtimecmd.RuntimeNodePresetSharedSQLite,
			StateRuntimePreset:   runtimecmd.RuntimeNodePresetSharedSQLite,
			SandboxRuntimePreset: runtimecmd.RuntimeNodePresetFastLocal,
		}
	}
}

func BuildPresetConfig(preset StackPreset) Config {
	return BuildProfileConfig(StackProfile{
		Name:   string(preset),
		Preset: DefaultPresetSpec(preset),
	})
}

func (c Config) PresetSpec() PresetSpec {
	return DefaultPresetSpec(c.effectivePreset())
}

func (s PresetSpec) Apply(config Config) Config {
	switch s.Preset {
	case StackPresetSharedRemote, StackPresetSharedSQLite:
		config.Gateway.Runtime.Preset = s.GatewayRuntimePreset
		config.Worker.Preset = s.WorkerRuntimePreset
		config.State.Runtime.Preset = s.StateRuntimePreset
		config.Sandbox.Runtime.Preset = s.SandboxRuntimePreset
	default:
		if config.Gateway.Runtime.Preset == "" {
			config.Gateway.Runtime.Preset = s.GatewayRuntimePreset
		}
		if config.Worker.Preset == "" {
			config.Worker.Preset = s.WorkerRuntimePreset
		}
		if config.State.Runtime.Preset == "" {
			config.State.Runtime.Preset = s.StateRuntimePreset
		}
		if config.Sandbox.Runtime.Preset == "" {
			config.Sandbox.Runtime.Preset = s.SandboxRuntimePreset
		}
	}
	config.Gateway.Runtime = runtimecmd.ApplyNodePresetDefaults(config.Gateway.Runtime)
	config.Worker = runtimecmd.ApplyNodePresetDefaults(config.Worker)
	config.State.Runtime = runtimecmd.ApplyNodePresetDefaults(config.State.Runtime)
	config.Sandbox.Runtime = runtimecmd.ApplyNodePresetDefaults(config.Sandbox.Runtime)
	return config
}
