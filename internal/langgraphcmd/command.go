package langgraphcmd

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
)

type BuildInfo struct {
	Version   string
	Commit    string
	BuildTime string
}

type CommandOptions struct {
	Stderr io.Writer
	Stdout io.Writer
	Args   []string
}

func RunCommand(fs *flag.FlagSet, build BuildInfo, options CommandOptions) error {
	prepared, err := PrepareCommand(fs, build, options)
	if err != nil {
		return err
	}
	return prepared.Run()
}

func PrepareCommand(fs *flag.FlagSet, build BuildInfo, options CommandOptions) (*commandrun.PreparedCommand, error) {
	if fs == nil {
		fs = flag.CommandLine
	}
	logger := log.New(commandrun.OutputWriter(options.Stderr), "[deerflow] ", log.LstdFlags)
	printManifest := fs.Bool("print-manifest", false, "print resolved langgraph server manifest and exit")

	yolo := fs.Bool("yolo", false, "YOLO mode: no auth, defaults for all settings")
	cfg := DefaultConfig()
	binding := BindFlags(fs, cfg)
	if err := fs.Parse(commandrun.CommandArgs(fs, options.Args)); err != nil {
		return nil, err
	}
	cfg = binding.Config()

	cfg.ApplyYoloEnvironment(*yolo)
	cfg.ApplyProcessEnvironment()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid runtime configuration: %w", err)
	}
	if *printManifest {
		return &commandrun.PreparedCommand{
			RunFunc: func() error {
				return commandrun.PrintJSON(options.Stdout, cfg.Manifest())
			},
		}, nil
	}

	launcher, err := cfg.BuildLauncher()
	if err != nil {
		return nil, fmt.Errorf("failed to create server: %w", err)
	}
	return &commandrun.PreparedCommand{
		Logger:          logger,
		Lifecycle:       launcher,
		StartupLines:    cfg.StartupLines(build, *yolo, strings.TrimSpace(os.Getenv("LOG_LEVEL"))),
		ReadyLines:      cfg.ReadyLines(),
		Ready:           cfg.ReadyProbe(),
		ReadyTimeout:    15 * time.Second,
		ShutdownTimeout: 15 * time.Second,
		IgnoredErrors:   []error{http.ErrServerClosed},
	}, nil
}
