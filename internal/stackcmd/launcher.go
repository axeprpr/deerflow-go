package stackcmd

import (
	"context"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
)

type Launcher struct {
	group          *commandrun.LifecycleGroup
	components     []LaunchComponent
	spec           LaunchSpec
	deploymentSpec DeploymentSpec
}

func NewLauncher(components []LaunchComponent) *Launcher {
	lifecycles := make([]commandrun.Lifecycle, 0, len(components))
	for _, component := range components {
		if component.Lifecycle != nil {
			lifecycles = append(lifecycles, component.Lifecycle)
		}
	}
	return &Launcher{
		group:      commandrun.NewLifecycleGroup(lifecycles...),
		components: append([]LaunchComponent(nil), components...),
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

func (l *Launcher) Components() []LaunchComponent {
	if l == nil {
		return nil
	}
	return append([]LaunchComponent(nil), l.components...)
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
	components, err := cfg.BuildComponents(ctx)
	if err != nil {
		return nil, err
	}
	launcher := NewLauncher(components)
	launcher.spec = cfg.LaunchSpec()
	launcher.deploymentSpec = cfg.DeploymentSpec()
	return launcher, nil
}
