package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
	"github.com/axeprpr/deerflow-go/internal/runtimecmd"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/llm"
)

type config struct {
	Runtime   runtimecmd.NodeConfig
	LogPrefix string
}

func main() {
	cfg := parseConfig()
	logger := log.New(os.Stderr, cfg.LogPrefix, log.LstdFlags)

	launcher, err := buildLauncher(cfg)
	if err != nil {
		logger.Fatal(err)
	}

	spec := launcher.Spec()
	if !spec.ServesRemoteWorker {
		logger.Fatalf("runtime node role %q does not expose a remote worker server", spec.Role)
	}

	logger.Printf("runtime node ready role=%s addr=%s", spec.Role, spec.RemoteWorkerAddr)
	if err := commandrun.Run(logger, launcher, 15*time.Second, http.ErrServerClosed); err != nil {
		logger.Fatal(err)
	}
}

func parseConfig() config {
	defaults := runtimecmd.DefaultRuntimeWorkerNodeConfig()
	binding := runtimecmd.BindFlags(flag.CommandLine, defaults, "", "")
	flag.Parse()

	return config{
		Runtime:   binding.Config(),
		LogPrefix: "[runtime-node] ",
	}
}

func buildLauncher(cfg config) (*harnessruntime.RuntimeNodeLauncher, error) {
	if err := cfg.Runtime.ValidateForRuntimeNode(); err != nil {
		return nil, err
	}
	provider := llm.NewProvider(cfg.Runtime.Provider)
	clarify := clarification.NewManager(32)

	_, launcher, err := harnessruntime.BuildDefaultRuntimeSystemLauncherForRoleWithMemory(
		context.Background(),
		cfg.Runtime.Role,
		cfg.Runtime.Name,
		cfg.Runtime.Root,
		cfg.Runtime.Endpoint,
		cfg.Runtime.DataRoot,
		provider,
		clarify,
		cfg.Runtime.MaxTurns,
		nil,
		nil,
		nil,
	)
	if err != nil {
		return nil, err
	}
	if launcher != nil && launcher.Node() != nil && launcher.Node().RemoteWorker != nil && launcher.Node().RemoteWorker.Server() != nil {
		launcher.Node().RemoteWorker.Server().Addr = cfg.Runtime.Addr
	}
	return launcher, nil
}
