package langgraphcompat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type testLLMProvider struct{}

func (testLLMProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Message: models.Message{Role: models.RoleAI}}, nil
}

func (testLLMProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

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

	_ = s.newAgent(harness.AgentSpec{})

	sandboxDir := filepath.Join(tmp, "deerflow-langgraph-sandbox", "langgraph")
	if _, err := os.Stat(sandboxDir); err != nil {
		t.Fatalf("sandbox dir missing after newAgent lazy init: %v", err)
	}
}

func TestNewServerDefaultsMainRunMaxTurnsToUpstreamRecursionLimit(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	s, err := NewServer(":0", "", "test-model")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if s.maxTurns != 100 {
		t.Fatalf("maxTurns = %d, want 100", s.maxTurns)
	}
}

func TestDefaultRuntimeProviderUsesConfiguredProvider(t *testing.T) {
	t.Setenv("DEFAULT_LLM_PROVIDER", "openai")
	if got := defaultRuntimeProvider(); got != "openai" {
		t.Fatalf("defaultRuntimeProvider() = %q, want %q", got, "openai")
	}
}

func TestNewServerUsesStableRuntimeBoundaries(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	s, err := NewServer(":0", "", "test-model")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if s.sandboxManager == nil {
		t.Fatal("sandboxManager = nil")
	}
	if s.runDispatcher == nil {
		t.Fatal("runDispatcher = nil")
	}
	if s.defaultRunDispatcher() != s.runDispatcher {
		t.Fatal("defaultRunDispatcher() did not reuse stable dispatcher")
	}
	if s.defaultSandboxRuntime(nil) == nil {
		t.Fatal("defaultSandboxRuntime() = nil")
	}
	if s.runtimeSystem == nil || s.runtimeSystem.RemoteWorker == nil || s.runtimeSystem.RemoteWorker.Server() == nil {
		t.Fatalf("runtimeSystem remote worker = %#v", s.runtimeSystem)
	}
	if s.runtimeSystem.RuntimeView() != s.runtime {
		t.Fatalf("runtimeSystem runtime = %#v want %#v", s.runtimeSystem.RuntimeView(), s.runtime)
	}
}

func TestCompatFallbacksRebuildRuntimeSystemFromNodeConfig(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	s, err := NewServer(":0", "", "test-model")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	s.runtimeSystem = nil
	s.runDispatcher = nil
	s.sandboxManager = nil

	if got := s.defaultRunDispatcher(); got == nil {
		t.Fatal("defaultRunDispatcher() = nil")
	}
	if got := s.defaultSandboxRuntime(nil); got == nil {
		t.Fatal("defaultSandboxRuntime() = nil")
	}
	if s.runtimeSystem == nil {
		t.Fatal("runtimeSystem = nil after fallback rebuild")
	}
	if s.runtimeSystem.Dispatcher != s.runDispatcher {
		t.Fatal("dispatcher was not rebound through runtimeSystem")
	}
	if s.runtimeSystem.RemoteWorker == nil || s.runtimeSystem.RemoteWorker.Server() == nil {
		t.Fatalf("runtimeSystem remote worker = %#v", s.runtimeSystem.RemoteWorker)
	}
}

func TestNewServerWithRuntimeNodeConfigUsesConfiguredRole(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	config := harnessruntime.DefaultGatewayRuntimeNodeConfig("gateway-node", t.TempDir(), "http://worker:8081/dispatch")
	s, err := NewServer(":0", "", "test-model", WithRuntimeNodeConfig(config))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if s.runtimeNode.Role != harnessruntime.RuntimeNodeRoleGateway {
		t.Fatalf("runtimeNode.Role = %q", s.runtimeNode.Role)
	}
	if s.runtimeSystem == nil {
		t.Fatal("runtimeSystem = nil")
	}
	if s.runtimeSystem.Config.Role != harnessruntime.RuntimeNodeRoleGateway {
		t.Fatalf("runtimeSystem.Config.Role = %q", s.runtimeSystem.Config.Role)
	}
	if s.runtimeSystem.RemoteWorker != nil {
		t.Fatalf("gateway node remote worker = %#v, want nil", s.runtimeSystem.RemoteWorker)
	}
}

func TestNewServerWithMaxTurnsAppliesBeforeBootstrap(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	s, err := NewServer(":0", "", "test-model", WithMaxTurns(23))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if s.maxTurns != 23 {
		t.Fatalf("maxTurns = %d, want 23", s.maxTurns)
	}
	if s.runtime == nil {
		t.Fatal("runtime = nil")
	}
}

func TestNewServerWithLLMProviderAppliesBeforeBootstrap(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	provider := testLLMProvider{}
	s, err := NewServer(":0", "", "test-model", WithLLMProvider(provider))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if s.llmProvider != provider {
		t.Fatalf("llmProvider = %#v, want %#v", s.llmProvider, provider)
	}
}

func TestNewServerDoesNotServeFrontendAtRoot(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	s, err := NewServer(":0", "", "test-model")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	rec := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("root status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestNewServerAllowsFrontendCORSPreflight(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	s, err := NewServer(":0", "", "test-model")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodOptions, "http://example.com/api/models", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want localhost UI origin", got)
	}
}
