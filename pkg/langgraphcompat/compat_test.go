package langgraphcompat

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"net/http"
	"net/http/httptest"

	"github.com/axeprpr/deerflow-go/pkg/agent"
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

func TestNewAgentLazilyInitializesSandbox(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	s, err := NewServer(":0", "", "test-model")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if s.sandbox != nil {
		t.Fatal("sandbox initialized before agent creation")
	}

	_ = s.newAgent(agent.AgentConfig{})

	if s.sandbox == nil {
		t.Fatal("sandbox = nil after lazy agent initialization")
	}
	sandboxDir := filepath.Join(tmp, "deerflow-langgraph-sandbox", "langgraph")
	if _, err := os.Stat(sandboxDir); err != nil {
		t.Fatalf("sandbox dir missing after newAgent lazy init: %v", err)
	}
}

func TestNewServerServesEmbeddedFrontendAtRoot(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html><body>stub frontend</body></html>")},
	}

	s, err := NewServer(":0", "", "test-model", WithFrontendFS(frontend))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	rec := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("root status = %d, want %d", rec.Code, http.StatusOK)
	}

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != "<html><body>stub frontend</body></html>" {
		t.Fatalf("root body = %q", string(body))
	}
}
