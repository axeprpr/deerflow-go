package stackcmd

import "github.com/axeprpr/deerflow-go/pkg/harnessruntime"

type StackProfile struct {
	Name                 string                                  `json:"name"`
	Preset               PresetSpec                              `json:"preset"`
	GatewayStateProvider harnessruntime.RuntimeStateProviderMode `json:"gateway_state_provider"`
	WorkerStateProvider  harnessruntime.RuntimeStateProviderMode `json:"worker_state_provider"`
	WorkerTransport      harnessruntime.WorkerTransportBackend   `json:"worker_transport"`
}

func DefaultStackProfile(preset StackPreset) StackProfile {
	switch preset {
	case StackPresetSharedRemote:
		return StackProfile{
			Name:                 "shared-remote",
			Preset:               DefaultPresetSpec(StackPresetSharedRemote),
			GatewayStateProvider: harnessruntime.RuntimeStateProviderModeAuto,
			WorkerStateProvider:  harnessruntime.RuntimeStateProviderModeAuto,
			WorkerTransport:      harnessruntime.WorkerTransportBackendQueue,
		}
	case StackPresetSharedSQLite:
		return StackProfile{
			Name:                 "shared-sqlite",
			Preset:               DefaultPresetSpec(StackPresetSharedSQLite),
			GatewayStateProvider: harnessruntime.RuntimeStateProviderModeSharedSQLite,
			WorkerStateProvider:  harnessruntime.RuntimeStateProviderModeSharedSQLite,
			WorkerTransport:      harnessruntime.WorkerTransportBackendQueue,
		}
	default:
		return StackProfile{
			Name:                 "auto",
			Preset:               DefaultPresetSpec(StackPresetAuto),
			GatewayStateProvider: harnessruntime.RuntimeStateProviderModeAuto,
			WorkerStateProvider:  harnessruntime.RuntimeStateProviderModeAuto,
			WorkerTransport:      harnessruntime.WorkerTransportBackendQueue,
		}
	}
}

func BuildProfileConfig(profile StackProfile) Config {
	gateway, worker, state, sb := defaultSplitComponents()
	cfg := Config{
		Preset:  profile.Preset.Preset,
		Gateway: gateway,
		Worker:  worker,
		State:   state,
		Sandbox: sb,
	}
	cfg = profile.Preset.Apply(cfg)
	if cfg.Gateway.Runtime.StateProvider == "" {
		cfg.Gateway.Runtime.StateProvider = profile.GatewayStateProvider
	}
	if cfg.Worker.StateProvider == "" {
		cfg.Worker.StateProvider = profile.WorkerStateProvider
	}
	if cfg.Worker.TransportBackend == "" {
		cfg.Worker.TransportBackend = profile.WorkerTransport
	}
	return cfg
}

func (c Config) Profile() StackProfile {
	return DefaultStackProfile(c.effectivePreset())
}

func DefaultStackProfileConfig(preset StackPreset) Config {
	return BuildProfileConfig(DefaultStackProfile(preset))
}
