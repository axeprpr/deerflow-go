package stackcmd

import "fmt"

type ComponentKind string

const (
	ComponentGateway ComponentKind = "gateway"
	ComponentWorker  ComponentKind = "worker"
	ComponentState   ComponentKind = "state"
	ComponentSandbox ComponentKind = "sandbox"
)

type ComponentSpec struct {
	Kind      ComponentKind
	Addr      string
	ReadyURL  string
	ReadyLine string
}

type DeploymentSpec struct {
	Preset         StackPreset
	WorkerDispatch string
	Components     []ComponentSpec
}

func (c Config) DeploymentSpec() DeploymentSpec {
	cfg := c.withDefaults()
	spec := DeploymentSpec{
		Preset:         cfg.effectivePreset(),
		WorkerDispatch: workerDispatchEndpoint(cfg.Worker.Addr),
		Components: []ComponentSpec{
			{
				Kind:     ComponentGateway,
				Addr:     cfg.Gateway.Addr,
				ReadyURL: cfg.LaunchSpec().GatewayHealthURL(),
			},
			{
				Kind:      ComponentWorker,
				Addr:      cfg.Worker.Addr,
				ReadyURL:  cfg.LaunchSpec().WorkerHealthURL(),
				ReadyLine: fmt.Sprintf("  Worker server: %s", cfg.LaunchSpec().WorkerDispatchURL()),
			},
		},
	}
	if cfg.usesDedicatedStateService() {
		spec.Components = append(spec.Components, ComponentSpec{
			Kind:      ComponentState,
			Addr:      cfg.State.Runtime.Addr,
			ReadyURL:  cfg.LaunchSpec().WorkerStateHealthURL(),
			ReadyLine: fmt.Sprintf("  State server: %s", cfg.LaunchSpec().WorkerStateHealthURL()),
		})
		spec.Components = append(spec.Components, ComponentSpec{
			Kind:      ComponentSandbox,
			Addr:      cfg.Sandbox.Runtime.Addr,
			ReadyURL:  cfg.LaunchSpec().WorkerSandboxHealthURL(),
			ReadyLine: fmt.Sprintf("  Sandbox server: %s", cfg.LaunchSpec().WorkerSandboxHealthURL()),
		})
	} else {
		spec.Components = append(spec.Components, ComponentSpec{
			Kind:      ComponentState,
			Addr:      cfg.Worker.Addr,
			ReadyURL:  cfg.LaunchSpec().WorkerStateHealthURL(),
			ReadyLine: fmt.Sprintf("  Worker state: %s", cfg.LaunchSpec().WorkerStateHealthURL()),
		})
		spec.Components = append(spec.Components, ComponentSpec{
			Kind:      ComponentSandbox,
			Addr:      cfg.Worker.Addr,
			ReadyURL:  cfg.LaunchSpec().WorkerSandboxHealthURL(),
			ReadyLine: fmt.Sprintf("  Worker sandbox: %s", cfg.LaunchSpec().WorkerSandboxHealthURL()),
		})
	}
	return spec
}

func (s DeploymentSpec) ReadyLines() []string {
	lines := make([]string, 0, len(s.Components))
	for _, component := range s.Components {
		if component.ReadyLine != "" {
			lines = append(lines, component.ReadyLine)
		}
	}
	return lines
}

func (s DeploymentSpec) ReadyTargets() []string {
	targets := make([]string, 0, len(s.Components))
	for _, component := range s.Components {
		if component.ReadyURL != "" {
			targets = append(targets, component.ReadyURL)
		}
	}
	return targets
}
