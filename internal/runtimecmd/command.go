package runtimecmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/llm"
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
	if err := cfg.ValidateForRuntimeNode(); err != nil {
		return err
	}

	provider := llm.NewProvider(cfg.Provider)
	clarify := clarification.NewManager(32)
	_, launcher, err := harnessruntime.BuildDefaultRuntimeSystemLauncherForRoleWithMemory(
		context.Background(),
		cfg.Role,
		cfg.Name,
		cfg.Root,
		cfg.Endpoint,
		cfg.DataRoot,
		provider,
		clarify,
		cfg.MaxTurns,
		nil,
		nil,
		nil,
	)
	if err != nil {
		return err
	}
	if launcher != nil && launcher.Node() != nil && launcher.Node().RemoteWorker != nil && launcher.Node().RemoteWorker.Server() != nil {
		launcher.Node().RemoteWorker.Server().Addr = cfg.Addr
	}

	spec := launcher.Spec()
	if !spec.ServesRemoteWorker {
		return fmt.Errorf("runtime node role %q does not expose a remote worker server", spec.Role)
	}
	logger.Printf("runtime node ready role=%s addr=%s", spec.Role, spec.RemoteWorkerAddr)
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
