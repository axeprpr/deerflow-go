package langgraphcompat

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestCrossInstanceThreadStateGetHydratesFromSharedStore(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	sharedRoot := t.TempDir()
	baseConfig := harnessruntime.DefaultGatewayRuntimeNodeConfig("gateway-a", sharedRoot, "http://worker:8081/dispatch")
	baseConfig.State.Backend = harnessruntime.RuntimeStateStoreBackendSQLite
	baseConfig.State.SnapshotBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	baseConfig.State.EventBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	baseConfig.State.ThreadBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	baseConfig.State.Root = filepath.Join(sharedRoot, "runtime-state")

	serverA, err := NewServer(":0", "", "test-model", WithRuntimeNodeConfig(baseConfig))
	if err != nil {
		t.Fatalf("NewServer(serverA) error = %v", err)
	}
	serverB, err := NewServer(":0", "", "test-model", WithRuntimeNodeConfig(baseConfig))
	if err != nil {
		t.Fatalf("NewServer(serverB) error = %v", err)
	}

	threadID := "thread-cross-instance-state-get"
	storeA := serverA.ensureThreadStateStore()
	storeA.MarkThreadStatus(threadID, "busy")
	storeA.SetThreadMetadata(threadID, harnessruntime.DefaultRunIDMetadataKey, "run-state-1")
	storeA.SetThreadMetadata(threadID, harnessruntime.DefaultActiveRunMetadataKey, "run-state-1")
	storeA.SetThreadMetadata(threadID, "memory_user_id", "user-1")
	storeA.SetThreadMetadata(threadID, "memory_group_id", "group-1")
	storeA.SetThreadMetadata(threadID, "memory_namespace", "workspace-a")

	resp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodGet, "/threads/"+threadID+"/state", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"active_run_id":"run-state-1"`) {
		t.Fatalf("missing active_run_id in body: %s", body)
	}
	if !strings.Contains(body, `"memory_user_id":"user-1"`) || !strings.Contains(body, `"memory_group_id":"group-1"`) {
		t.Fatalf("missing memory scope metadata in body: %s", body)
	}
}

func TestCrossInstanceThreadStatePatchHydratesMissingSession(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	sharedRoot := t.TempDir()
	baseConfig := harnessruntime.DefaultGatewayRuntimeNodeConfig("gateway-a", sharedRoot, "http://worker:8081/dispatch")
	baseConfig.State.Backend = harnessruntime.RuntimeStateStoreBackendSQLite
	baseConfig.State.SnapshotBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	baseConfig.State.EventBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	baseConfig.State.ThreadBackend = harnessruntime.RuntimeStateStoreBackendSQLite
	baseConfig.State.Root = filepath.Join(sharedRoot, "runtime-state")

	serverA, err := NewServer(":0", "", "test-model", WithRuntimeNodeConfig(baseConfig))
	if err != nil {
		t.Fatalf("NewServer(serverA) error = %v", err)
	}
	serverB, err := NewServer(":0", "", "test-model", WithRuntimeNodeConfig(baseConfig))
	if err != nil {
		t.Fatalf("NewServer(serverB) error = %v", err)
	}

	threadID := "thread-cross-instance-state-patch"
	storeA := serverA.ensureThreadStateStore()
	storeA.MarkThreadStatus(threadID, "idle")
	storeA.SetThreadMetadata(threadID, "title", "Shared Thread")

	patch := strings.NewReader(`{"metadata":{"title":"Shared Thread Updated","memory_user_id":"user-2","memory_namespace":"workspace-b"}}`)
	resp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodPatch, "/threads/"+threadID+"/state", patch, map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", resp.Code, resp.Body.String())
	}

	serverA.sessionsMu.Lock()
	delete(serverA.sessions, threadID)
	serverA.sessionsMu.Unlock()

	getA := performCompatRequest(t, serverA.httpServer.Handler, http.MethodGet, "/threads/"+threadID+"/state", nil, nil)
	if getA.Code != http.StatusOK {
		t.Fatalf("getA status=%d body=%s", getA.Code, getA.Body.String())
	}
	body := getA.Body.String()
	if !strings.Contains(body, `"title":"Shared Thread Updated"`) {
		t.Fatalf("missing updated title metadata in cross-instance state: %s", body)
	}
	if !strings.Contains(body, `"memory_user_id":"user-2"`) || !strings.Contains(body, `"memory_namespace":"workspace-b"`) {
		t.Fatalf("missing updated memory metadata in cross-instance state: %s", body)
	}
}
