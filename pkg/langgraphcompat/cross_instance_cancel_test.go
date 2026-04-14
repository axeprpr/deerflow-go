package langgraphcompat

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestCrossInstanceCancelFallsBackForStaleDetachedRun(t *testing.T) {
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

	now := time.Now().UTC().Add(-2 * detachedRunCancelGracePeriod)
	run := &Run{
		RunID:       "run-cross-instance-cancel",
		ThreadID:    "thread-cross-instance-cancel",
		AssistantID: "assistant-1",
		Status:      "running",
		CreatedAt:   now,
		UpdatedAt:   now,
		Outcome:     harnessruntime.RunOutcomeDescriptor{RunStatus: "running"},
	}
	serverA.saveRun(run)

	cancelResp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodPost, "/threads/"+run.ThreadID+"/runs/"+run.RunID+"/cancel", nil, nil)
	if cancelResp.Code != http.StatusAccepted {
		t.Fatalf("cancel status=%d body=%s", cancelResp.Code, cancelResp.Body.String())
	}

	getResp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodGet, "/threads/"+run.ThreadID+"/runs/"+run.RunID, nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	if !strings.Contains(getResp.Body.String(), `"status":"interrupted"`) {
		t.Fatalf("get body missing interrupted status: %s", getResp.Body.String())
	}

	streamResp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodGet, "/threads/"+run.ThreadID+"/runs/"+run.RunID+"/stream?streamMode=events", nil, nil)
	if streamResp.Code != http.StatusOK {
		t.Fatalf("stream status=%d body=%s", streamResp.Code, streamResp.Body.String())
	}
	streamBody := streamResp.Body.String()
	if got := strings.Count(streamBody, "event: end"); got != 1 {
		t.Fatalf("stream end event count=%d body=%s", got, streamBody)
	}
	if !strings.Contains(streamBody, `"status":"interrupted"`) {
		t.Fatalf("stream body missing interrupted end payload: %s", streamBody)
	}
}

func TestCrossInstanceCancelDoesNotForceCancelFreshDetachedRun(t *testing.T) {
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

	now := time.Now().UTC()
	run := &Run{
		RunID:       "run-cross-instance-cancel-fresh",
		ThreadID:    "thread-cross-instance-cancel-fresh",
		AssistantID: "assistant-1",
		Status:      "running",
		CreatedAt:   now,
		UpdatedAt:   now,
		Outcome:     harnessruntime.RunOutcomeDescriptor{RunStatus: "running"},
	}
	serverA.saveRun(run)

	cancelResp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodPost, "/threads/"+run.ThreadID+"/runs/"+run.RunID+"/cancel", nil, nil)
	if cancelResp.Code != http.StatusConflict {
		t.Fatalf("cancel status=%d body=%s", cancelResp.Code, cancelResp.Body.String())
	}
}
