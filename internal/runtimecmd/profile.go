package runtimecmd

import "github.com/axeprpr/deerflow-go/pkg/harnessruntime"

type RuntimeProfile struct {
	Name      string
	State     StateProfile
	Execution ExecutionProfile
}

func DefaultRuntimeProfile(preset RuntimeNodePreset, role harnessruntime.RuntimeNodeRole) RuntimeProfile {
	state := DefaultStateProfile(preset, role)
	execution := DefaultExecutionProfile(preset, role)
	name := state.Name
	if name == "" {
		name = execution.Name
	}
	if execution.Name != "" && execution.Name != name {
		name = name + "+" + execution.Name
	}
	return RuntimeProfile{
		Name:      name,
		State:     state,
		Execution: execution,
	}
}

func (p RuntimeProfile) Apply(config NodeConfig) NodeConfig {
	config = p.Execution.Apply(config)
	config = p.State.Apply(config)
	return config
}
