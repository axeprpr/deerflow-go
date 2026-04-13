package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	launcher, err := buildLauncher(context.Background(), cfg)
	if err != nil {
		logger.Fatal(err)
	}

	spec := launcher.Spec()
	if !spec.ServesRemoteWorker {
		logger.Fatalf("runtime node role %q does not expose a remote worker server", spec.Role)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- launcher.Start()
	}()

	logger.Printf("runtime node ready role=%s addr=%s", spec.Role, spec.RemoteWorkerAddr)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal(err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := launcher.Close(shutdownCtx); err != nil {
			logger.Fatal(err)
		}
	}
}

func parseConfig() config {
	defaults := runtimecmd.DefaultRuntimeWorkerNodeConfig()
	role := flag.String("role", string(defaults.Role), "runtime node role: worker|all-in-one|gateway")
	addr := flag.String("addr", defaults.Addr, "remote worker listen address")
	name := flag.String("name", defaults.Name, "runtime node name")
	root := flag.String("root", defaults.Root, "runtime node root")
	dataRoot := flag.String("data-root", defaults.DataRoot, "runtime data root")
	provider := flag.String("provider", defaults.Provider, "LLM provider")
	endpoint := flag.String("endpoint", defaults.Endpoint, "remote worker endpoint for gateway role")
	maxTurns := flag.Int("max-turns", defaults.MaxTurns, "default max turns")
	transportBackend := flag.String("transport-backend", string(defaults.TransportBackend), "worker transport backend: direct|queue|remote")
	sandboxBackend := flag.String("sandbox-backend", string(defaults.SandboxBackend), "sandbox backend: local-linux|container|remote|windows-restricted")
	sandboxEndpoint := flag.String("sandbox-endpoint", defaults.SandboxEndpoint, "sandbox backend endpoint for remote sandbox")
	sandboxImage := flag.String("sandbox-image", defaults.SandboxImage, "sandbox image for container backend")
	stateRoot := flag.String("state-root", defaults.StateRoot, "runtime state root")
	stateBackend := flag.String("state-backend", string(defaults.StateBackend), "runtime state backend: in-memory|file")
	snapshotBackend := flag.String("snapshot-backend", string(defaults.SnapshotBackend), "runtime snapshot backend override: in-memory|file")
	eventBackend := flag.String("event-backend", string(defaults.EventBackend), "runtime event backend override: in-memory|file")
	threadBackend := flag.String("thread-backend", string(defaults.ThreadBackend), "runtime thread backend override: in-memory|file")
	flag.Parse()

	return config{
		Runtime: runtimecmd.NodeConfig{
			Role:             runtimecmd.NormalizeRole(*role, defaults.Role),
			Addr:             runtimecmd.NormalizeAddr(*addr, defaults.Addr),
			Name:             *name,
			Root:             *root,
			DataRoot:         *dataRoot,
			Provider:         *provider,
			Endpoint:         *endpoint,
			MaxTurns:         *maxTurns,
			TransportBackend: runtimecmd.NormalizeTransportBackend(*transportBackend, defaults.TransportBackend),
			SandboxBackend:   runtimecmd.NormalizeSandboxBackend(*sandboxBackend, defaults.SandboxBackend),
			SandboxEndpoint:  *sandboxEndpoint,
			SandboxImage:     *sandboxImage,
			StateRoot:        *stateRoot,
			StateBackend:     runtimecmd.NormalizeStateBackend(*stateBackend, defaults.StateBackend),
			SnapshotBackend:  runtimecmd.NormalizeStateBackend(*snapshotBackend, defaults.SnapshotBackend),
			EventBackend:     runtimecmd.NormalizeStateBackend(*eventBackend, defaults.EventBackend),
			ThreadBackend:    runtimecmd.NormalizeStateBackend(*threadBackend, defaults.ThreadBackend),
		},
		LogPrefix: "[runtime-node] ",
	}
}

func buildLauncher(ctx context.Context, cfg config) (*harnessruntime.RuntimeNodeLauncher, error) {
	if err := cfg.Runtime.ValidateForRuntimeNode(); err != nil {
		return nil, err
	}
	provider := llm.NewProvider(cfg.Runtime.Provider)
	clarify := clarification.NewManager(32)

	_, launcher, err := harnessruntime.BuildDefaultRuntimeSystemLauncherForRoleWithMemory(
		ctx,
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
