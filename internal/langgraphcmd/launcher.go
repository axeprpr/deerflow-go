package langgraphcmd

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/langgraphcompat"
)

type Launcher struct {
	server *langgraphcompat.Server
}

func NewLauncher(server *langgraphcompat.Server) *Launcher {
	return &Launcher{server: server}
}

func (l *Launcher) Server() *langgraphcompat.Server {
	if l == nil {
		return nil
	}
	return l.server
}

func (l *Launcher) Start() error {
	if l == nil || l.server == nil {
		return nil
	}
	return l.server.Start()
}

func (l *Launcher) Close(ctx context.Context) error {
	if l == nil || l.server == nil {
		return nil
	}
	return l.server.Shutdown(ctx)
}

func (c Config) BuildLauncher() (*Launcher, error) {
	server, err := c.BuildServer()
	if err != nil {
		return nil, err
	}
	return NewLauncher(server), nil
}
