package commandrun

import (
	"context"
	"log"
	"time"
)

type PreparedCommand struct {
	Logger          *log.Logger
	Lifecycle       Lifecycle
	StartupLines    []string
	ReadyLines      []string
	Ready           ReadyFunc
	ReadyTimeout    time.Duration
	ShutdownTimeout time.Duration
	IgnoredErrors   []error
}

func (p PreparedCommand) Run() error {
	for _, line := range p.StartupLines {
		if p.Logger != nil && line != "" {
			p.Logger.Print(line)
		}
	}
	var ready ReadyFunc
	if len(p.ReadyLines) > 0 {
		ready = func(ctx context.Context) error {
			if p.Ready != nil {
				if err := p.Ready(ctx); err != nil {
					return err
				}
			}
			for _, line := range p.ReadyLines {
				if p.Logger != nil && line != "" {
					p.Logger.Print(line)
				}
			}
			return nil
		}
	} else {
		ready = p.Ready
	}
	return RunWithReady(p.Logger, p.Lifecycle, ready, p.ReadyTimeout, p.ShutdownTimeout, p.IgnoredErrors...)
}
