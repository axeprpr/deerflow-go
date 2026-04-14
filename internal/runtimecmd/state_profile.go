package runtimecmd

import "github.com/axeprpr/deerflow-go/pkg/harnessruntime"

type StateProfile struct {
	Name     string                                  `json:"name"`
	Provider harnessruntime.RuntimeStateProviderMode `json:"provider"`
}

func DefaultStateProfile(preset RuntimeNodePreset, role harnessruntime.RuntimeNodeRole) StateProfile {
	switch preset {
	case RuntimeNodePresetSharedSQLite:
		return StateProfile{
			Name:     "shared-sqlite",
			Provider: harnessruntime.RuntimeStateProviderModeSharedSQLite,
		}
	case RuntimeNodePresetSharedRemote:
		if role == harnessruntime.RuntimeNodeRoleGateway {
			return StateProfile{
				Name:     "shared-remote",
				Provider: harnessruntime.RuntimeStateProviderModeAuto,
			}
		}
		return StateProfile{
			Name:     "shared-sqlite",
			Provider: harnessruntime.RuntimeStateProviderModeSharedSQLite,
		}
	case RuntimeNodePresetFastLocal:
		return StateProfile{
			Name:     "fast-local",
			Provider: harnessruntime.RuntimeStateProviderModeIsolated,
		}
	default:
		if role == harnessruntime.RuntimeNodeRoleGateway || role == harnessruntime.RuntimeNodeRoleWorker {
			return StateProfile{
				Name:     "shared-sqlite",
				Provider: harnessruntime.RuntimeStateProviderModeSharedSQLite,
			}
		}
		return StateProfile{
			Name:     "auto",
			Provider: harnessruntime.RuntimeStateProviderModeAuto,
		}
	}
}

func (p StateProfile) Apply(config NodeConfig) NodeConfig {
	switch p.Name {
	case "shared-sqlite":
		config = config.applySharedSQLiteDefaults()
	case "shared-remote":
		config = config.applySharedRemoteDefaults()
	case "fast-local":
		config = config.applyFastLocalDefaults()
	default:
		if config.Role == harnessruntime.RuntimeNodeRoleGateway || config.Role == harnessruntime.RuntimeNodeRoleWorker {
			config = config.applySharedSQLiteDefaults()
		}
	}
	if config.StateProvider == "" || config.StateProvider == harnessruntime.RuntimeStateProviderModeAuto {
		config.StateProvider = p.Provider
	}
	return config
}
