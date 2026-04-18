package langgraphcompat

import (
	"net/http"
	"path/filepath"
	"strings"
	"sync"
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

func TestCrossInstanceCancelKeepsOwnedActiveRunForJoinSelection(t *testing.T) {
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
	threadID := "thread-cross-instance-cancel-owned-active"
	staleRun := &Run{
		RunID:       "run-cross-instance-stale",
		ThreadID:    threadID,
		AssistantID: "assistant-1",
		Status:      "running",
		CreatedAt:   now.Add(-3 * detachedRunCancelGracePeriod),
		UpdatedAt:   now.Add(-2 * detachedRunCancelGracePeriod),
		Outcome:     harnessruntime.RunOutcomeDescriptor{RunStatus: "running"},
	}
	activeRun := &Run{
		RunID:       "run-cross-instance-active",
		ThreadID:    threadID,
		AssistantID: "assistant-1",
		Status:      "running",
		CreatedAt:   now.Add(-time.Minute),
		UpdatedAt:   now,
		Outcome:     harnessruntime.RunOutcomeDescriptor{RunStatus: "running"},
	}
	serverA.saveRun(staleRun)
	serverA.saveRun(activeRun)
	serverA.ensureThreadStateStore().MarkThreadStatus(threadID, "busy")
	serverA.ensureThreadStateStore().SetThreadMetadata(threadID, harnessruntime.DefaultRunIDMetadataKey, activeRun.RunID)
	serverA.ensureThreadStateStore().SetThreadMetadata(threadID, harnessruntime.DefaultActiveRunMetadataKey, activeRun.RunID)

	cancelResp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodPost, "/threads/"+threadID+"/runs/"+staleRun.RunID+"/cancel", nil, nil)
	if cancelResp.Code != http.StatusAccepted {
		t.Fatalf("cancel status=%d body=%s", cancelResp.Code, cancelResp.Body.String())
	}
	threadState, ok := serverB.ensureThreadStateStore().LoadThreadRuntimeState(threadID)
	if !ok {
		t.Fatal("thread state missing after stale cancel")
	}
	if got := threadState.Status; got != "busy" {
		t.Fatalf("thread status after stale cancel = %q, want busy", got)
	}
	if got := threadState.Metadata[harnessruntime.DefaultActiveRunMetadataKey]; got != activeRun.RunID {
		t.Fatalf("active run metadata after stale cancel = %v, want %q", got, activeRun.RunID)
	}

	staleGet := performCompatRequest(t, serverB.httpServer.Handler, http.MethodGet, "/threads/"+threadID+"/runs/"+staleRun.RunID, nil, nil)
	if staleGet.Code != http.StatusOK {
		t.Fatalf("stale get status=%d body=%s", staleGet.Code, staleGet.Body.String())
	}
	if !strings.Contains(staleGet.Body.String(), `"status":"interrupted"`) {
		t.Fatalf("stale run body missing interrupted status: %s", staleGet.Body.String())
	}

	bodyCh := make(chan string, 1)
	go func() {
		resp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodGet, "/threads/"+threadID+"/stream?streamMode=events", nil, nil)
		bodyCh <- resp.Body.String()
	}()

	deadline := time.Now().Add(2 * time.Second)
	joined := false
	for time.Now().Before(deadline) {
		if serverB.runSubscriberCount(activeRun.RunID) > 0 {
			joined = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !joined {
		t.Fatalf("timed out waiting for join subscriber on %q", activeRun.RunID)
	}
	if got := serverB.runSubscriberCount(staleRun.RunID); got != 0 {
		t.Fatalf("stale run subscribers = %d, want 0", got)
	}

	serverA.appendRunEvent(activeRun.RunID, StreamEvent{
		ID:       activeRun.RunID + ":1",
		Event:    "chunk",
		RunID:    activeRun.RunID,
		ThreadID: activeRun.ThreadID,
		Data: map[string]any{
			"run_id":    activeRun.RunID,
			"thread_id": activeRun.ThreadID,
			"type":      "ai",
			"role":      "assistant",
			"delta":     "active-survives-cancel",
			"content":   "active-survives-cancel",
		},
	})
	serverA.appendRunEvent(activeRun.RunID, StreamEvent{
		ID:       activeRun.RunID + ":2",
		Event:    "end",
		RunID:    activeRun.RunID,
		ThreadID: activeRun.ThreadID,
		Data: map[string]any{
			"run_id": activeRun.RunID,
		},
	})

	select {
	case body := <-bodyCh:
		if !strings.Contains(body, "active-survives-cancel") {
			t.Fatalf("missing active run payload after stale cancel: %q", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for join stream after stale cancel")
	}
}

func TestCrossInstanceCancelIsIdempotentUnderConcurrentStaleRequests(t *testing.T) {
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

	now := time.Now().UTC().Add(-3 * detachedRunCancelGracePeriod)
	run := &Run{
		RunID:       "run-cross-instance-concurrent-cancel",
		ThreadID:    "thread-cross-instance-concurrent-cancel",
		AssistantID: "assistant-1",
		Status:      "running",
		CreatedAt:   now,
		UpdatedAt:   now,
		Outcome:     harnessruntime.RunOutcomeDescriptor{RunStatus: "running"},
	}
	serverA.saveRun(run)

	const workers = 12
	statuses := make(chan int, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			resp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodPost, "/threads/"+run.ThreadID+"/runs/"+run.RunID+"/cancel", nil, nil)
			statuses <- resp.Code
		}()
	}
	wg.Wait()
	close(statuses)

	accepted := 0
	conflict := 0
	for status := range statuses {
		switch status {
		case http.StatusAccepted:
			accepted++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected cancel status=%d", status)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted cancel count=%d want=1 (conflict=%d)", accepted, conflict)
	}

	getResp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodGet, "/threads/"+run.ThreadID+"/runs/"+run.RunID, nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	if !strings.Contains(getResp.Body.String(), `"status":"interrupted"`) {
		t.Fatalf("run body missing interrupted status: %s", getResp.Body.String())
	}

	events := serverB.ensureEventStore().LoadRunEvents(run.RunID)
	endEvents := 0
	for _, evt := range events {
		if evt.Event == "end" {
			endEvents++
		}
	}
	if endEvents != 1 {
		t.Fatalf("end event count=%d want=1 events=%+v", endEvents, events)
	}
}

func TestCrossInstanceCancelIsIdempotentAcrossConcurrentGateways(t *testing.T) {
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
	serverC, err := NewServer(":0", "", "test-model", WithRuntimeNodeConfig(baseConfig))
	if err != nil {
		t.Fatalf("NewServer(serverC) error = %v", err)
	}

	now := time.Now().UTC().Add(-3 * detachedRunCancelGracePeriod)
	run := &Run{
		RunID:       "run-cross-instance-gateway-concurrent-cancel",
		ThreadID:    "thread-cross-instance-gateway-concurrent-cancel",
		AssistantID: "assistant-1",
		Status:      "running",
		CreatedAt:   now,
		UpdatedAt:   now,
		Outcome:     harnessruntime.RunOutcomeDescriptor{RunStatus: "running"},
	}
	serverA.saveRun(run)

	const workersPerGateway = 8
	statuses := make(chan int, workersPerGateway*2)
	var wg sync.WaitGroup
	wg.Add(workersPerGateway * 2)
	for i := 0; i < workersPerGateway; i++ {
		go func() {
			defer wg.Done()
			resp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodPost, "/threads/"+run.ThreadID+"/runs/"+run.RunID+"/cancel", nil, nil)
			statuses <- resp.Code
		}()
		go func() {
			defer wg.Done()
			resp := performCompatRequest(t, serverC.httpServer.Handler, http.MethodPost, "/threads/"+run.ThreadID+"/runs/"+run.RunID+"/cancel", nil, nil)
			statuses <- resp.Code
		}()
	}
	wg.Wait()
	close(statuses)

	accepted := 0
	conflict := 0
	for status := range statuses {
		switch status {
		case http.StatusAccepted:
			accepted++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected cancel status=%d", status)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted cancel count=%d want=1 (conflict=%d)", accepted, conflict)
	}
}
