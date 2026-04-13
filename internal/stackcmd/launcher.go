package stackcmd

import (
	"context"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
	"github.com/axeprpr/deerflow-go/internal/runtimecmd"
	"github.com/axeprpr/deerflow-go/internal/sandboxcmd"
	"github.com/axeprpr/deerflow-go/internal/statecmd"
)

type Launcher struct {
	group          *commandrun.LifecycleGroup
	gateway        *langgraphcmd.Launcher
	worker         *runtimecmd.NodeConfig
	state          *statecmd.Config
	sandbox        *sandboxcmd.Config
	spec           LaunchSpec
	deploymentSpec DeploymentSpec
}

func NewLauncher(gateway *langgraphcmd.Launcher, worker commandrun.Lifecycle, workerConfig *runtimecmd.NodeConfig, state commandrun.Lifecycle, stateConfig *statecmd.Config, sandbox commandrun.Lifecycle, sandboxConfig *sandboxcmd.Config) *Launcher {
	return &Launcher{
		group:   commandrun.NewLifecycleGroup(state, sandbox, worker, gateway),
		gateway: gateway,
		worker:  workerConfig,
		state:   stateConfig,
		sandbox: sandboxConfig,
	}
}

func (l *Launcher) Spec() LaunchSpec {
	if l == nil {
		return LaunchSpec{}
	}
	return l.spec
}

func (l *Launcher) DeploymentSpec() DeploymentSpec {
	if l == nil {
		return DeploymentSpec{}
	}
	return l.deploymentSpec
}

func (l *Launcher) Start() error {
	if l == nil || l.group == nil {
		return nil
	}
	return l.group.Start()
}

func (l *Launcher) Close(ctx context.Context) error {
	if l == nil || l.group == nil {
		return nil
	}
	return l.group.Close(ctx)
}

func (c Config) BuildLauncher(ctx context.Context) (*Launcher, error) {
	cfg := c.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	workerLauncher, err := cfg.Worker.BuildLauncher(ctx)
	if err != nil {
		return nil, err
	}
	var stateLauncher commandrun.Lifecycle
	var stateConfig *statecmd.Config
	var sandboxLauncher commandrun.Lifecycle
	var sandboxConfig *sandboxcmd.Config
	if cfg.usesDedicatedStateService() {
		launcher, err := cfg.State.BuildLauncher()
		if err != nil {
			return nil, err
		}
		stateLauncher = launcher
		stateCopy := cfg.State
		stateConfig = &stateCopy

		sbLauncher, err := cfg.Sandbox.BuildLauncher()
		if err != nil {
			return nil, err
		}
		sandboxLauncher = sbLauncher
		sbCopy := cfg.Sandbox
		sandboxConfig = &sbCopy
	}
	gatewayLauncher, err := cfg.Gateway.BuildLauncher()
	if err != nil {
		return nil, err
	}
	workerConfig := cfg.Worker
	launcher := NewLauncher(gatewayLauncher, workerLauncher, &workerConfig, stateLauncher, stateConfig, sandboxLauncher, sandboxConfig)
	launcher.spec = cfg.LaunchSpec()
	launcher.deploymentSpec = cfg.DeploymentSpec()
	return launcher, nil
}
