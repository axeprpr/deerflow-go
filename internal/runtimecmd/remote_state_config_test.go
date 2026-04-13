package runtimecmd

import (
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestDeriveStateBackendFromRemoteStoreURL(t *testing.T) {
	t.Parallel()

	if got := deriveStateBackendFromStoreURL("http://state.example", harnessruntime.RuntimeStateStoreBackendInMemory); got != harnessruntime.RuntimeStateStoreBackendRemote {
		t.Fatalf("deriveStateBackendFromStoreURL(http) = %q", got)
	}
	if got := deriveStateBackendFromStoreURL("https://state.example", harnessruntime.RuntimeStateStoreBackendInMemory); got != harnessruntime.RuntimeStateStoreBackendRemote {
		t.Fatalf("deriveStateBackendFromStoreURL(https) = %q", got)
	}
}

func TestNodeConfigValidatesSharedRemoteStateStore(t *testing.T) {
	t.Parallel()

	cfg := NodeConfig{
		Role:          harnessruntime.RuntimeNodeRoleAllInOne,
		StateBackend:  harnessruntime.RuntimeStateStoreBackendRemote,
		StateStoreURL: "http://state.example",
	}
	cfg = cfg.withRoleDefaults()
	if err := cfg.ValidateForRuntimeNode(); err != nil {
		t.Fatalf("ValidateForRuntimeNode() error = %v", err)
	}

	cfg.EventBackend = harnessruntime.RuntimeStateStoreBackendFile
	if err := cfg.ValidateForRuntimeNode(); err == nil || !strings.Contains(err.Error(), "state-store requires remote") {
		t.Fatalf("ValidateForRuntimeNode() error = %v", err)
	}
}
