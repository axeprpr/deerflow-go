package langgraphcompat

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestThreadJoinStreamAcrossGatewayInstancesWithSharedState(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	prev := sseHeartbeatInterval
	sseHeartbeatInterval = 20 * time.Millisecond
	defer func() {
		sseHeartbeatInterval = prev
	}()

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
		RunID:       "run-cross-instance",
		ThreadID:    "thread-cross-instance",
		AssistantID: "assistant-1",
		Status:      "running",
		CreatedAt:   now,
		UpdatedAt:   now,
		Outcome:     harnessruntime.RunOutcomeDescriptor{RunStatus: "running"},
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
			"delta":     "chunk-from-a-before-join",
			"content":   "chunk-from-a-before-join",
		},
	})

	bodyCh := make(chan string, 1)
	go func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/threads/"+run.ThreadID+"/stream?streamMode=events", nil)
		serverB.httpServer.Handler.ServeHTTP(rec, req)
		bodyCh <- rec.Body.String()
	}()

	deadline := time.Now().Add(2 * time.Second)
	joined := false
	for time.Now().Before(deadline) {
		select {
		case body := <-bodyCh:
			t.Fatalf("join stream returned before end event: %q", body)
		default:
		}
		if serverB.runSubscriberCount(run.RunID) > 0 {
			joined = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !joined {
		t.Fatalf("timed out waiting for join subscriber on %q", run.RunID)
	}

	serverA.appendRunEvent(run.RunID, StreamEvent{
		ID:       run.RunID + ":2",
		Event:    "chunk",
		RunID:    run.RunID,
		ThreadID: run.ThreadID,
		Data: map[string]any{
			"run_id":    run.RunID,
			"thread_id": run.ThreadID,
			"type":      "ai",
			"role":      "assistant",
			"delta":     "chunk-from-a-after-join",
			"content":   "chunk-from-a-after-join",
		},
	})
	serverA.appendRunEvent(run.RunID, StreamEvent{
		ID:       run.RunID + ":3",
		Event:    "end",
		RunID:    run.RunID,
		ThreadID: run.ThreadID,
		Data: map[string]any{
			"run_id": run.RunID,
		},
	})

	select {
	case body := <-bodyCh:
		if !strings.Contains(body, "chunk-from-a-before-join") {
			t.Fatalf("missing replay chunk from serverA: %q", body)
		}
		if !strings.Contains(body, "chunk-from-a-after-join") {
			t.Fatalf("missing live chunk from serverA: %q", body)
		}
		if !strings.Contains(body, "event: end") {
			t.Fatalf("missing end event: %q", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cross-instance join stream response")
	}
}
