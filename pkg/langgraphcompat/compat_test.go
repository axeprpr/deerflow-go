package langgraphcompat

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type testLLMProvider struct{}
type failingLLMProvider struct{}

func (testLLMProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Message: models.Message{Role: models.RoleAI}}, nil
}

func (testLLMProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (failingLLMProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, errors.New("gateway provider should not execute")
}

func (failingLLMProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, errors.New("gateway provider should not execute")
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

func TestServerStartStartsAllInOneRemoteWorker(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	apiAddr := freeTCPAddr(t)
	workerAddr := freeTCPAddr(t)
	config := harnessruntime.DefaultRuntimeNodeConfig("all-in-one-node", t.TempDir())
	config.RemoteWorker.Addr = workerAddr

	s, err := NewServer(apiAddr, "", "test-model", WithRuntimeNodeConfig(config), WithLLMProvider(testLLMProvider{}))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Start()
	}()

	waitForHTTP(t, "http://"+apiAddr+"/health")
	waitForHTTP(t, "http://"+workerAddr+"/health")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestGatewayRoleDispatchesRunsToRemoteWorker(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	sharedRoot := t.TempDir()
	workerAddr := freeTCPAddr(t)
	workerConfig := harnessruntime.DefaultWorkerRuntimeNodeConfig("remote-worker", sharedRoot)
	workerConfig.RemoteWorker.Addr = workerAddr
	workerConfig.State.Backend = harnessruntime.RuntimeStateStoreBackendSQLite
	workerConfig.State.SnapshotBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	workerConfig.State.EventBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	workerConfig.State.ThreadBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	workerConfig.State.Root = filepath.Join(sharedRoot, "runtime-state")
	_, workerLauncher, err := harnessruntime.BuildDefaultRuntimeSystemLauncherWithMemory(
		context.Background(),
		workerConfig,
		sharedRoot,
		fakeLLMProvider{},
		nil,
		32,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("BuildDefaultRuntimeSystemLauncherWithMemory() error = %v", err)
	}

	workerErrCh := make(chan error, 1)
	go func() {
		workerErrCh <- workerLauncher.Start()
	}()
	waitForHTTP(t, "http://"+workerAddr+harnessruntime.DefaultRemoteWorkerHealthPath)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := workerLauncher.Close(shutdownCtx); err != nil {
			t.Fatalf("worker Close() error = %v", err)
		}
		if err := <-workerErrCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("worker Start() error = %v", err)
		}
	}()

	gatewayConfig := harnessruntime.DefaultGatewayRuntimeNodeConfig("remote-gateway", sharedRoot, "http://"+workerAddr+harnessruntime.DefaultRemoteWorkerDispatchPath)
	gatewayConfig.State.Backend = harnessruntime.RuntimeStateStoreBackendSQLite
	gatewayConfig.State.SnapshotBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	gatewayConfig.State.EventBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	gatewayConfig.State.ThreadBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	gatewayConfig.State.Root = filepath.Join(sharedRoot, "runtime-state")
	gateway, err := NewServer(":0", "", "test-model", WithRuntimeNodeConfig(gatewayConfig), WithLLMProvider(failingLLMProvider{}))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if gateway.runtimeSystem == nil || gateway.runtimeSystem.RemoteWorker != nil {
		t.Fatalf("gateway runtimeSystem remote worker = %#v", gateway.runtimeSystem)
	}

	resp := performCompatRequest(t, gateway.httpServer.Handler, http.MethodPost, "/runs/stream", strings.NewReader(`{"thread_id":"thread-remote-worker","input":{"messages":[{"role":"user","content":"hello"}]}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `event: metadata`) {
		t.Fatalf("body = %q", resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `event: chunk`) || !strings.Contains(resp.Body.String(), `hello from fake llm`) {
		t.Fatalf("body missing remote replayed chunk: %q", resp.Body.String())
	}
	if got := strings.Count(resp.Body.String(), "event: end"); got != 1 {
		t.Fatalf("end event count = %d body=%q", got, resp.Body.String())
	}

	var stored *Run
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runID := findRunIDForThread(gateway, "thread-remote-worker")
		stored = gateway.getRun(runID)
		if stored != nil && stored.Status == "success" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if stored == nil {
		t.Fatal("stored run missing")
	}
	if stored.Status != "success" {
		t.Fatalf("run status = %q err=%q", stored.Status, stored.Error)
	}
	record, ok := gateway.ensureSnapshotStore().LoadRunSnapshot(stored.RunID)
	if !ok {
		t.Fatal("shared snapshot missing")
	}
	if record.Record.Outcome.RunStatus != "success" {
		t.Fatalf("snapshot outcome = %+v", record.Record.Outcome)
	}
	endCount := 0
	for _, event := range gateway.ensureEventStore().LoadRunEvents(stored.RunID) {
		if event.Event == "end" {
			endCount++
		}
	}
	if endCount != 1 {
		t.Fatalf("stored end event count = %d", endCount)
	}
}

type flushBuffer struct {
	mu     sync.Mutex
	header http.Header
	body   bytes.Buffer
	status int
}

func (b *flushBuffer) Header() http.Header {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.header == nil {
		b.header = make(http.Header)
	}
	return b.header
}

func (b *flushBuffer) WriteHeader(status int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status = status
}

func (b *flushBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.body.Write(p)
}

func (b *flushBuffer) Flush() {}

func (b *flushBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.body.String()
}

func TestRemoteJoinStreamPollsSharedEventStore(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	sharedRoot := t.TempDir()
	config := harnessruntime.DefaultGatewayRuntimeNodeConfig("remote-gateway", sharedRoot, "http://worker:8081/dispatch")
	config.State.Backend = harnessruntime.RuntimeStateStoreBackendSQLite
	config.State.SnapshotBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	config.State.EventBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	config.State.ThreadBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	config.State.Root = filepath.Join(sharedRoot, "runtime-state")

	server, err := NewServer(":0", "", "test-model", WithRuntimeNodeConfig(config))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	run := &Run{
		RunID:       "run-remote-join",
		ThreadID:    "thread-remote-join",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Outcome:     harnessruntime.RunOutcomeDescriptor{RunStatus: "running"},
		AssistantID: "assistant-1",
	}
	server.saveRun(run)

	recorder := &flushBuffer{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.newRunReplayStreamer(recorder, recorder, newStreamModeFilter(nil)).Join(run)
	}()

	time.Sleep(150 * time.Millisecond)
	harnessruntime.NewEventLogService(server.ensureEventStore()).Record(run.RunID, run.ThreadID, "chunk", map[string]any{
		"run_id":    run.RunID,
		"thread_id": run.ThreadID,
		"type":      "ai",
		"role":      "assistant",
		"delta":     "remote hello",
		"content":   "remote hello",
	})
	harnessruntime.NewEventLogService(server.ensureEventStore()).Record(run.RunID, run.ThreadID, "end", map[string]any{
		"run_id": run.RunID,
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Join() did not return")
	}

	body := recorder.String()
	if !strings.Contains(body, "event: chunk") || !strings.Contains(body, "remote hello") {
		t.Fatalf("body missing replayed remote chunk: %q", body)
	}
	if !strings.Contains(body, "event: end") {
		t.Fatalf("body missing remote end: %q", body)
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

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return addr
}

func waitForHTTP(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("waitForHTTP(%q) error = %v", url, err)
			}
			t.Fatalf("waitForHTTP(%q) timed out", url)
		}
		time.Sleep(25 * time.Millisecond)
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
