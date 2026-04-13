package runtimecmd

import (
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestNodeConfigReadyLine(t *testing.T) {
	cfg := DefaultRuntimeWorkerNodeConfig()
	line, err := cfg.ReadyLine(harnessruntime.RuntimeNodeLaunchSpec{
		Role:                harnessruntime.RuntimeNodeRoleWorker,
		ServesRemoteWorker:  true,
		RemoteWorkerAddr:    ":9999",
		ServesRemoteSandbox: true,
		RemoteSandboxAddr:   ":9999",
	})
	if err != nil {
		t.Fatalf("ReadyLine() error = %v", err)
	}
	if !strings.Contains(line, "role=worker") || !strings.Contains(line, "addr=:9999") || !strings.Contains(line, "sandbox=true") {
		t.Fatalf("ReadyLine() = %q", line)
	}
}
