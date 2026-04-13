package commandrun

import (
	"log"
	"time"
)

type PreparedCommand struct {
	Logger          *log.Logger
	Lifecycle       Lifecycle
	StartupLines    []string
	ReadyLines      []string
	ShutdownTimeout time.Duration
	IgnoredErrors   []error
}

func (p PreparedCommand) Run() error {
	for _, line := range p.StartupLines {
		if p.Logger != nil && line != "" {
			p.Logger.Print(line)
		}
	}
	for _, line := range p.ReadyLines {
		if p.Logger != nil && line != "" {
			p.Logger.Print(line)
		}
	}
	return Run(p.Logger, p.Lifecycle, p.ShutdownTimeout, p.IgnoredErrors...)
}
