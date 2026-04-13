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
