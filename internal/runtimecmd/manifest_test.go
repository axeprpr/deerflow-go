package runtimecmd

import (
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestNodeConfigManifestIncludesProfiles(t *testing.T) {
	cfg := DefaultSplitGatewayNodeConfig("http://worker:8081/dispatch")
	manifest := cfg.Manifest()
	if manifest.RuntimeProfile == "" {
		t.Fatal("RuntimeProfile = empty")
	}
	if manifest.StateProfile == "" {
		t.Fatal("StateProfile = empty")
	}
	if manifest.ExecutionProfile == "" {
		t.Fatal("ExecutionProfile = empty")
	}
}

func TestNodeManifestStartupLinesIncludeProfiles(t *testing.T) {
	cfg := DefaultSplitGatewayNodeConfig("http://worker:8081/dispatch")
	joined := strings.Join(cfg.StartupLines(), "\n")
	if !strings.Contains(joined, "profile=") {
		t.Fatalf("StartupLines = %q", joined)
	}
	if !strings.Contains(joined, "state_profile=") {
		t.Fatalf("StartupLines = %q", joined)
	}
}

func TestNodeManifestReadyLineIncludesProfile(t *testing.T) {
	cfg := DefaultSplitGatewayNodeConfig("http://worker:8081/dispatch")
	line, err := cfg.ReadyLine(harnessruntime.RuntimeNodeLaunchSpec{
		Role:               harnessruntime.RuntimeNodeRoleWorker,
		RemoteWorkerAddr:   "127.0.0.1:8081",
		ServesRemoteWorker: true,
	})
	if err != nil {
		t.Fatalf("ReadyLine() error = %v", err)
	}
	if !strings.Contains(line, "profile=") {
		t.Fatalf("ReadyLine() = %q", line)
	}
}
