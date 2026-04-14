package runtimecmd

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/llm"
)

func (c NodeConfig) BuildLauncher(ctx context.Context) (*harnessruntime.RuntimeNodeLauncher, error) {
	if err := c.ValidateForRuntimeNode(); err != nil {
		return nil, err
	}
	provider := llm.NewProvider(c.Provider)
	clarify := clarification.NewManager(32)
	_, launcher, err := harnessruntime.BuildDefaultRuntimeSystemLauncherForRoleWithMemory(
		ctx,
		c.Role,
		c.Name,
		c.Root,
		c.Endpoint,
		c.DataRoot,
		provider,
		clarify,
		c.MaxTurns,
		nil,
		nil,
		nil,
	)
	if err != nil {
		return nil, err
	}
	if launcher != nil && launcher.Node() != nil && launcher.Node().RemoteWorker != nil && launcher.Node().RemoteWorker.Server() != nil {
		launcher.Node().RemoteWorker.Server().Addr = c.Addr
	}
	return launcher, nil
}

func (c NodeConfig) ReadyLine(spec harnessruntime.RuntimeNodeLaunchSpec) (string, error) {
	return c.Manifest().ReadyLine(spec)
}
