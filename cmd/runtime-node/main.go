package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

type config struct {
	Role       string
	Addr       string
	Name       string
	Root       string
	DataRoot   string
	Provider   string
	Endpoint   string
	MaxTurns   int
	LogPrefix  string
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
	role := flag.String("role", firstNonEmpty(os.Getenv("RUNTIME_NODE_ROLE"), string(harnessruntime.RuntimeNodeRoleWorker)), "runtime node role: worker|all-in-one|gateway")
	addr := flag.String("addr", firstNonEmpty(os.Getenv("RUNTIME_NODE_ADDR"), ":8081"), "remote worker listen address")
	name := flag.String("name", firstNonEmpty(os.Getenv("RUNTIME_NODE_NAME"), "runtime-node"), "runtime node name")
	root := flag.String("root", firstNonEmpty(os.Getenv("RUNTIME_NODE_ROOT"), filepath.Join(os.TempDir(), "deerflow-runtime-node")), "runtime node root")
	dataRoot := flag.String("data-root", firstNonEmpty(os.Getenv("DEERFLOW_DATA_ROOT"), tools.DataRootFromEnv()), "runtime data root")
	provider := flag.String("provider", firstNonEmpty(os.Getenv("DEFAULT_LLM_PROVIDER"), "siliconflow"), "LLM provider")
	endpoint := flag.String("endpoint", strings.TrimSpace(os.Getenv("RUNTIME_NODE_ENDPOINT")), "remote worker endpoint for gateway role")
	maxTurns := flag.Int("max-turns", intFromEnv("RUNTIME_NODE_MAX_TURNS", 100), "default max turns")
	flag.Parse()

	return config{
		Role:      strings.TrimSpace(*role),
		Addr:      normalizeAddr(*addr),
		Name:      strings.TrimSpace(*name),
		Root:      strings.TrimSpace(*root),
		DataRoot:  strings.TrimSpace(*dataRoot),
		Provider:  strings.TrimSpace(*provider),
		Endpoint:  strings.TrimSpace(*endpoint),
		MaxTurns:  *maxTurns,
		LogPrefix: "[runtime-node] ",
	}
}

func buildLauncher(ctx context.Context, cfg config) (*harnessruntime.RuntimeNodeLauncher, error) {
	provider := llm.NewProvider(firstNonEmpty(cfg.Provider, "siliconflow"))
	role := normalizeRole(cfg.Role)
	clarify := clarification.NewManager(32)

	switch role {
	case harnessruntime.RuntimeNodeRoleAllInOne:
		_, launcher, err := harnessruntime.BuildDefaultAllInOneRuntimeSystemLauncherWithMemory(ctx, cfg.Name, cfg.Root, cfg.DataRoot, provider, clarify, cfg.MaxTurns, nil, nil, nil)
		if err != nil {
			return nil, err
		}
		if launcher != nil && launcher.Node() != nil && launcher.Node().RemoteWorker != nil && launcher.Node().RemoteWorker.Server() != nil {
			launcher.Node().RemoteWorker.Server().Addr = cfg.Addr
		}
		return launcher, nil
	case harnessruntime.RuntimeNodeRoleWorker:
		_, launcher, err := harnessruntime.BuildDefaultWorkerRuntimeSystemLauncherWithMemory(ctx, cfg.Name, cfg.Root, cfg.DataRoot, provider, clarify, cfg.MaxTurns, nil, nil, nil)
		if err != nil {
			return nil, err
		}
		if launcher != nil && launcher.Node() != nil && launcher.Node().RemoteWorker != nil && launcher.Node().RemoteWorker.Server() != nil {
			launcher.Node().RemoteWorker.Server().Addr = cfg.Addr
		}
		return launcher, nil
	case harnessruntime.RuntimeNodeRoleGateway:
		return nil, fmt.Errorf("gateway role does not start a runtime worker server; use cmd/langgraph for API serving")
	default:
		return nil, fmt.Errorf("unsupported runtime node role %q", cfg.Role)
	}
}

func normalizeRole(role string) harnessruntime.RuntimeNodeRole {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case string(harnessruntime.RuntimeNodeRoleAllInOne):
		return harnessruntime.RuntimeNodeRoleAllInOne
	case string(harnessruntime.RuntimeNodeRoleGateway):
		return harnessruntime.RuntimeNodeRoleGateway
	default:
		return harnessruntime.RuntimeNodeRoleWorker
	}
}

func normalizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ":8081"
	}
	if strings.HasPrefix(addr, ":") {
		return addr
	}
	return ":" + addr
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func intFromEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil {
		return fallback
	}
	return value
}
