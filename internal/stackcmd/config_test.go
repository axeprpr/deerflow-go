package stackcmd

import (
	"context"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/internal/runtimecmd"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestDefaultConfigUsesGatewayWorkerSplit(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Gateway.Runtime.Role != harnessruntime.RuntimeNodeRoleGateway {
		t.Fatalf("gateway role = %q", cfg.Gateway.Runtime.Role)
	}
	if cfg.Worker.Role != harnessruntime.RuntimeNodeRoleWorker {
		t.Fatalf("worker role = %q", cfg.Worker.Role)
	}
	if !strings.Contains(cfg.Gateway.Runtime.Endpoint, harnessruntime.DefaultRemoteWorkerDispatchPath) {
		t.Fatalf("gateway endpoint = %q", cfg.Gateway.Runtime.Endpoint)
	}
	if cfg.Gateway.Runtime.Preset != runtimecmd.RuntimeNodePresetSharedSQLite {
		t.Fatalf("gateway preset = %q", cfg.Gateway.Runtime.Preset)
	}
	if cfg.Worker.Preset != runtimecmd.RuntimeNodePresetSharedSQLite {
		t.Fatalf("worker preset = %q", cfg.Worker.Preset)
	}
	if cfg.Gateway.Runtime.StateProvider != harnessruntime.RuntimeStateProviderModeSharedSQLite {
		t.Fatalf("gateway state provider = %q", cfg.Gateway.Runtime.StateProvider)
	}
	if cfg.Worker.StateProvider != harnessruntime.RuntimeStateProviderModeSharedSQLite {
		t.Fatalf("worker state provider = %q", cfg.Worker.StateProvider)
	}
	if cfg.Worker.StateStoreURL != cfg.Gateway.Runtime.StateStoreURL {
		t.Fatalf("state store mismatch gateway=%q worker=%q", cfg.Gateway.Runtime.StateStoreURL, cfg.Worker.StateStoreURL)
	}
}

func TestConfigBuildLauncherUsesSplitRoles(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Gateway.Addr = ":0"
	cfg.Worker.Addr = ":19081"
	launcher, err := cfg.BuildLauncher(context.Background())
	if err != nil {
		t.Fatalf("BuildLauncher() error = %v", err)
	}
	if launcher == nil {
		t.Fatal("BuildLauncher() = nil")
	}
}

func TestConfigValidateRejectsRemoteWorkerTransport(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Worker.TransportBackend = harnessruntime.WorkerTransportBackendRemote
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "cannot be remote") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestConfigWithRemoteGatewayStateKeepsWorkerLocalState(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Worker.Addr = ":19081"
	cfg.Gateway.Runtime.StateBackend = harnessruntime.RuntimeStateStoreBackendRemote
	cfg.Gateway.Runtime.SnapshotBackend = harnessruntime.RuntimeStateStoreBackendRemote
	cfg.Gateway.Runtime.EventBackend = harnessruntime.RuntimeStateStoreBackendRemote
	cfg.Gateway.Runtime.ThreadBackend = harnessruntime.RuntimeStateStoreBackendRemote
	cfg.Gateway.Runtime.StateStoreURL = ""

	cfg = cfg.withDefaults()

	if cfg.Gateway.Runtime.StateStoreURL != "http://127.0.0.1:19081"+harnessruntime.DefaultRemoteStateBasePath {
		t.Fatalf("gateway state store = %q", cfg.Gateway.Runtime.StateStoreURL)
	}
	if cfg.Worker.StateBackend == harnessruntime.RuntimeStateStoreBackendRemote {
		t.Fatalf("worker state backend = %q", cfg.Worker.StateBackend)
	}
	if cfg.Worker.StateStoreURL == cfg.Gateway.Runtime.StateStoreURL {
		t.Fatalf("worker state store unexpectedly mirrored remote endpoint: %q", cfg.Worker.StateStoreURL)
	}
}
