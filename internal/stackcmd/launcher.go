package stackcmd

import (
	"context"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
	"github.com/axeprpr/deerflow-go/internal/runtimecmd"
)

type Launcher struct {
	group   *commandrun.LifecycleGroup
	gateway *langgraphcmd.Launcher
	worker  *runtimecmd.NodeConfig
}

func NewLauncher(gateway *langgraphcmd.Launcher, worker commandrun.Lifecycle, workerConfig *runtimecmd.NodeConfig) *Launcher {
	return &Launcher{
		group:   commandrun.NewLifecycleGroup(worker, gateway),
		gateway: gateway,
		worker:  workerConfig,
	}
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
	gatewayLauncher, err := cfg.Gateway.BuildLauncher()
	if err != nil {
		return nil, err
	}
	workerConfig := cfg.Worker
	return NewLauncher(gatewayLauncher, workerLauncher, &workerConfig), nil
}
