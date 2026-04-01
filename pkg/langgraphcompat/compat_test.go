package langgraphcompat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewServerDefersSandboxCreation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	s, err := NewServer(":0", "", "test-model")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	sandboxDir := filepath.Join(tmp, "deerflow-langgraph-sandbox", "langgraph")
	if _, err := os.Stat(sandboxDir); !os.IsNotExist(err) {
		t.Fatalf("sandbox dir exists immediately after startup: err=%v", err)
	}

	if _, err := s.getOrCreateSandbox(); err != nil {
		t.Fatalf("getOrCreateSandbox() error = %v", err)
	}
	if _, err := os.Stat(sandboxDir); err != nil {
		t.Fatalf("sandbox dir missing after lazy init: %v", err)
	}
}
