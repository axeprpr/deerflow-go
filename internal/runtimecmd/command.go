package runtimecmd

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
)

type CommandOptions struct {
	LogPrefix string
	Stderr    io.Writer
	Stdout    io.Writer
	Args      []string
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
	if options.LogPrefix == "" {
		options.LogPrefix = "[runtime-node] "
	}
	logger := log.New(commandrun.OutputWriter(options.Stderr), options.LogPrefix, log.LstdFlags)
	printManifest := fs.Bool("print-manifest", false, "print resolved runtime node manifest and exit")

	defaults := DefaultRuntimeWorkerNodeConfig()
	binding := BindFlags(fs, defaults, "", "")
	if err := fs.Parse(commandrun.CommandArgs(fs, options.Args)); err != nil {
		return nil, err
	}
	cfg := binding.Config()
	if *printManifest {
		return &commandrun.PreparedCommand{
			RunFunc: func() error {
				return commandrun.PrintJSON(options.Stdout, cfg.Manifest())
			},
		}, nil
	}
	launcher, err := cfg.BuildLauncher(context.Background())
	if err != nil {
		return nil, err
	}
	spec := launcher.Spec()
	readyLine, err := cfg.ReadyLine(spec)
	if err != nil {
		return nil, err
	}
	return &commandrun.PreparedCommand{
		Logger:          logger,
		Lifecycle:       launcher,
		StartupLines:    cfg.StartupLines(),
		ReadyLines:      []string{readyLine},
		Ready:           cfg.ReadyProbe(spec),
		ReadyTimeout:    15 * time.Second,
		ShutdownTimeout: 15 * time.Second,
		IgnoredErrors:   []error{http.ErrServerClosed, context.Canceled},
	}, nil
}

func IsExpectedCommandExit(err error) bool {
	return err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, context.Canceled)
}
