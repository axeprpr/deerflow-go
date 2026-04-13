package statecmd

import (
	"context"
	"flag"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
)

type CommandOptions struct {
	Stderr io.Writer
	Args   []string
}

func RunCommand(fs *flag.FlagSet, options CommandOptions) error {
	prepared, err := PrepareCommand(fs, options)
	if err != nil {
		return err
	}
	return prepared.Run()
}

func PrepareCommand(fs *flag.FlagSet, options CommandOptions) (*commandrun.PreparedCommand, error) {
	if fs == nil {
		fs = flag.CommandLine
	}
	logger := log.New(commandrun.OutputWriter(options.Stderr), "[runtime-state] ", log.LstdFlags)
	cfg := DefaultConfig()
	binding := BindFlags(fs, cfg)
	if err := fs.Parse(commandrun.CommandArgs(fs, options.Args)); err != nil {
		return nil, err
	}
	cfg = binding.Config()
	launcher, err := cfg.BuildLauncher()
	if err != nil {
		return nil, err
	}
	return &commandrun.PreparedCommand{
		Logger:          logger,
		Lifecycle:       launcher,
		StartupLines:    cfg.StartupLines(),
		ReadyLines:      cfg.ReadyLines(),
		Ready:           cfg.ReadyProbe(),
		ReadyTimeout:    15 * time.Second,
		ShutdownTimeout: 15 * time.Second,
		IgnoredErrors:   []error{http.ErrServerClosed, context.Canceled},
	}, nil
}
