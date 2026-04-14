package langgraphcompat

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestSharedStateReplayChainAcrossGatewayInstances(t *testing.T) {
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
		RunID:       "run-cross-instance-replay",
		ThreadID:    "thread-cross-instance-replay",
		AssistantID: "assistant-1",
		Status:      "success",
		CreatedAt:   now.Add(-time.Second),
		UpdatedAt:   now,
		Outcome:     harnessruntime.RunOutcomeDescriptor{RunStatus: "success"},
	}
	serverA.saveRun(run)
	serverA.appendRunEvent(run.RunID, StreamEvent{
		ID:       run.RunID + ":1",
		Event:    "chunk",
		RunID:    run.RunID,
		ThreadID: run.ThreadID,
		Data: map[string]any{
			"run_id":    run.RunID,
			"thread_id": run.ThreadID,
			"type":      "ai",
			"role":      "assistant",
			"delta":     "cross-instance-replay-chunk",
			"content":   "cross-instance-replay-chunk",
		},
	})
	serverA.appendRunEvent(run.RunID, StreamEvent{
		ID:       run.RunID + ":2",
		Event:    "end",
		RunID:    run.RunID,
		ThreadID: run.ThreadID,
		Data: map[string]any{
			"run_id": run.RunID,
		},
	})

	listResp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodGet, "/threads/"+run.ThreadID+"/runs", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	if !strings.Contains(listResp.Body.String(), run.RunID) {
		t.Fatalf("list body missing run id: %s", listResp.Body.String())
	}

	getResp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodGet, "/threads/"+run.ThreadID+"/runs/"+run.RunID, nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	if !strings.Contains(getResp.Body.String(), `"status":"success"`) {
		t.Fatalf("get body missing success status: %s", getResp.Body.String())
	}

	streamResp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodGet, "/threads/"+run.ThreadID+"/runs/"+run.RunID+"/stream?streamMode=events", nil, nil)
	if streamResp.Code != http.StatusOK {
		t.Fatalf("stream status=%d body=%s", streamResp.Code, streamResp.Body.String())
	}
	streamBody := streamResp.Body.String()
	if !strings.Contains(streamBody, "cross-instance-replay-chunk") {
		t.Fatalf("stream body missing replay chunk: %s", streamBody)
	}
	if got := strings.Count(streamBody, "event: end"); got != 1 {
		t.Fatalf("stream end event count=%d body=%s", got, streamBody)
	}

	joinResp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodGet, "/threads/"+run.ThreadID+"/stream?streamMode=events", nil, nil)
	if joinResp.Code != http.StatusOK {
		t.Fatalf("join status=%d body=%s", joinResp.Code, joinResp.Body.String())
	}
	joinBody := joinResp.Body.String()
	if !strings.Contains(joinBody, "cross-instance-replay-chunk") {
		t.Fatalf("join body missing replay chunk: %s", joinBody)
	}
	if got := strings.Count(joinBody, "event: end"); got != 1 {
		t.Fatalf("join end event count=%d body=%s", got, joinBody)
	}
}
