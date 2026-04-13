package runtimecmd

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
)

type CommandOptions struct {
	LogPrefix string
	Stderr    io.Writer
	Args      []string
}

func RunCommand(fs *flag.FlagSet, options CommandOptions) error {
	if fs == nil {
		fs = flag.CommandLine
	}
	if options.LogPrefix == "" {
		options.LogPrefix = "[runtime-node] "
	}
	logger := log.New(orStderr(options.Stderr), options.LogPrefix, log.LstdFlags)

	defaults := DefaultRuntimeWorkerNodeConfig()
	binding := BindFlags(fs, defaults, "", "")
	if err := fs.Parse(commandArgs(fs, options.Args)); err != nil {
		return err
	}
	cfg := binding.Config()
	launcher, err := cfg.BuildLauncher(context.Background())
	if err != nil {
		return err
	}
	for _, line := range cfg.StartupLines() {
		logger.Print(line)
	}
	spec := launcher.Spec()
	readyLine, err := cfg.ReadyLine(spec)
	if err != nil {
		return err
	}
	logger.Print(readyLine)
	return commandrun.Run(logger, launcher, 15*time.Second, http.ErrServerClosed, context.Canceled)
}

func orStderr(w io.Writer) io.Writer {
	if w != nil {
		return w
	}
	return os.Stderr
}

func commandArgs(fs *flag.FlagSet, explicit []string) []string {
	if explicit != nil {
		return explicit
	}
	if fs == flag.CommandLine {
		return os.Args[1:]
	}
	return nil
}

func IsExpectedCommandExit(err error) bool {
	return err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, context.Canceled)
}
