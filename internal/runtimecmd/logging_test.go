package runtimecmd

import (
	"strings"
	"testing"
)

func TestNodeConfigStartupLines(t *testing.T) {
	cfg := DefaultRuntimeWorkerNodeConfig()
	lines := cfg.StartupLines()
	if len(lines) == 0 {
		t.Fatal("StartupLines() returned no lines")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "runtime node starting role=worker") {
		t.Fatalf("StartupLines() = %q", joined)
	}
	if !strings.Contains(joined, "worker_addr=") {
		t.Fatalf("StartupLines() = %q", joined)
	}
}
