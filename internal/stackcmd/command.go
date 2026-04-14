package stackcmd

import (
	"context"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
)

type CommandOptions struct {
	Stderr io.Writer
	Args   []string
}

func RunCommand(fs *flag.FlagSet, build langgraphcmd.BuildInfo, options CommandOptions) error {
	prepared, err := PrepareCommand(fs, build, options)
	if err != nil {
		return err
	}
	return prepared.Run()
}

func PrepareCommand(fs *flag.FlagSet, build langgraphcmd.BuildInfo, options CommandOptions) (*commandrun.PreparedCommand, error) {
	if fs == nil {
		fs = flag.CommandLine
	}
	logger := log.New(commandrun.OutputWriter(options.Stderr), "[runtime-stack] ", log.LstdFlags)

	yolo := fs.Bool("yolo", false, "YOLO mode: no auth, defaults for all settings")
	cfg := DefaultConfig()
	binding := BindFlags(fs, cfg)
	if err := fs.Parse(commandrun.CommandArgs(fs, options.Args)); err != nil {
		return nil, err
	}
	cfg = binding.Config()
	cfg.Gateway.ApplyYoloEnvironment(*yolo)
	cfg.Gateway.ApplyProcessEnvironment()
	builder := NewBuilder(cfg)

	launcher, err := builder.BuildLauncher(context.Background())
	if err != nil {
		return nil, err
	}
	return &commandrun.PreparedCommand{
		Logger:          logger,
		Lifecycle:       launcher,
		StartupLines:    builder.StartupLines(build, *yolo, strings.TrimSpace(os.Getenv("LOG_LEVEL"))),
		ReadyLines:      builder.ReadyLines(),
		Ready:           builder.ReadyProbe(),
		ReadyTimeout:    15 * time.Second,
		ShutdownTimeout: 15 * time.Second,
		IgnoredErrors:   []error{http.ErrServerClosed, context.Canceled},
	}, nil
}
