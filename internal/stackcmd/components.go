package stackcmd

import (
	"context"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
)

type LaunchComponent struct {
	Kind      ComponentKind
	Lifecycle commandrun.Lifecycle
}

func (c Config) BuildComponents(ctx context.Context) ([]LaunchComponent, error) {
	cfg := c.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	components := make([]LaunchComponent, 0, 4)

	if cfg.usesDedicatedStateService() {
		stateLauncher, err := cfg.State.BuildLauncher()
		if err != nil {
			return nil, err
		}
		components = append(components, LaunchComponent{Kind: ComponentState, Lifecycle: stateLauncher})

		sandboxLauncher, err := cfg.Sandbox.BuildLauncher()
		if err != nil {
			return nil, err
		}
		components = append(components, LaunchComponent{Kind: ComponentSandbox, Lifecycle: sandboxLauncher})
	}

	workerLauncher, err := cfg.Worker.BuildLauncher(ctx)
	if err != nil {
		return nil, err
	}
	components = append(components, LaunchComponent{Kind: ComponentWorker, Lifecycle: workerLauncher})

	gatewayLauncher, err := cfg.Gateway.BuildLauncher()
	if err != nil {
		return nil, err
	}
	components = append(components, LaunchComponent{Kind: ComponentGateway, Lifecycle: gatewayLauncher})
	return components, nil
}
