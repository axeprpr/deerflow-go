package langgraphcompat

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestCrossInstanceJoinRemainsConsistentUnderConcurrentGateways(t *testing.T) {
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
	serverC, err := NewServer(":0", "", "test-model", WithRuntimeNodeConfig(baseConfig))
	if err != nil {
		t.Fatalf("NewServer(serverC) error = %v", err)
	}

	now := time.Now().UTC()
	run := &Run{
		RunID:           "run-cross-instance-join-concurrent",
		ThreadID:        "thread-cross-instance-join-concurrent",
		AssistantID:     "assistant-1",
		Attempt:         2,
		ResumeFromEvent: 1,
		ResumeReason:    "gateway-recovery",
		Status:          "running",
		CreatedAt:       now,
		UpdatedAt:       now,
		Outcome: harnessruntime.RunOutcomeDescriptor{
			RunStatus:       "running",
			Attempt:         2,
			ResumeFromEvent: 1,
			ResumeReason:    "gateway-recovery",
		},
	}
	serverA.saveRun(run)
	serverA.appendRunEvent(run.RunID, StreamEvent{
		ID:              run.RunID + ":1",
		Event:           "chunk",
		RunID:           run.RunID,
		ThreadID:        run.ThreadID,
		Attempt:         2,
		ResumeFromEvent: 1,
		ResumeReason:    "gateway-recovery",
		Data: map[string]any{
			"run_id":    run.RunID,
			"thread_id": run.ThreadID,
			"type":      "ai",
			"role":      "assistant",
			"delta":     "replay-before-join",
			"content":   "replay-before-join",
		},
	})

	const joinWorkers = 6
	type joinResult struct {
		server string
		body   string
	}
	bodyCh := make(chan joinResult, joinWorkers)
	var wg sync.WaitGroup
	wg.Add(joinWorkers)
	for i := 0; i < joinWorkers/2; i++ {
		go func() {
			defer wg.Done()
			resp := performCompatRequest(t, serverB.httpServer.Handler, http.MethodGet, "/threads/"+run.ThreadID+"/stream?streamMode=events", nil, nil)
			if resp.Code != http.StatusOK {
				bodyCh <- joinResult{server: "gateway-b", body: "status=" + strconv.Itoa(resp.Code) + " body=" + resp.Body.String()}
				return
			}
			bodyCh <- joinResult{server: "gateway-b", body: resp.Body.String()}
		}()
		go func() {
			defer wg.Done()
			resp := performCompatRequest(t, serverC.httpServer.Handler, http.MethodGet, "/threads/"+run.ThreadID+"/stream?streamMode=events", nil, nil)
			if resp.Code != http.StatusOK {
				bodyCh <- joinResult{server: "gateway-c", body: "status=" + strconv.Itoa(resp.Code) + " body=" + resp.Body.String()}
				return
			}
			bodyCh <- joinResult{server: "gateway-c", body: resp.Body.String()}
		}()
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if serverB.runSubscriberCount(run.RunID) >= joinWorkers/2 && serverC.runSubscriberCount(run.RunID) >= joinWorkers/2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if serverB.runSubscriberCount(run.RunID) < joinWorkers/2 || serverC.runSubscriberCount(run.RunID) < joinWorkers/2 {
		t.Fatalf("subscriber counts before end: b=%d c=%d want=%d", serverB.runSubscriberCount(run.RunID), serverC.runSubscriberCount(run.RunID), joinWorkers/2)
	}

	serverA.appendRunEvent(run.RunID, StreamEvent{
		ID:              run.RunID + ":2",
		Event:           "chunk",
		RunID:           run.RunID,
		ThreadID:        run.ThreadID,
		Attempt:         2,
		ResumeFromEvent: 1,
		ResumeReason:    "gateway-recovery",
		Data: map[string]any{
			"run_id":    run.RunID,
			"thread_id": run.ThreadID,
			"type":      "ai",
			"role":      "assistant",
			"delta":     "live-after-join",
			"content":   "live-after-join",
		},
	})
	serverA.appendRunEvent(run.RunID, StreamEvent{
		ID:              run.RunID + ":3",
		Event:           "end",
		RunID:           run.RunID,
		ThreadID:        run.ThreadID,
		Attempt:         2,
		ResumeFromEvent: 1,
		ResumeReason:    "gateway-recovery",
		Data: map[string]any{
			"run_id": run.RunID,
		},
	})

	wg.Wait()
	close(bodyCh)

	gotBodies := 0
	for item := range bodyCh {
		gotBodies++
		if !strings.Contains(item.body, "replay-before-join") {
			t.Fatalf("%s missing replay chunk: %q", item.server, item.body)
		}
		if !strings.Contains(item.body, "live-after-join") {
			t.Fatalf("%s missing live chunk: %q", item.server, item.body)
		}
		if strings.Count(item.body, "event: end") != 1 {
			t.Fatalf("%s end event count mismatch body=%q", item.server, item.body)
		}
	}
	if gotBodies != joinWorkers {
		t.Fatalf("join body count=%d want=%d", gotBodies, joinWorkers)
	}
}

func TestCrossInstanceReplayAndRunGetRemainConsistentUnderConcurrency(t *testing.T) {
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

	now := time.Now().UTC()
	run := &Run{
		RunID:           "run-cross-instance-replay-concurrent",
		ThreadID:        "thread-cross-instance-replay-concurrent",
		AssistantID:     "assistant-1",
		Attempt:         3,
		ResumeFromEvent: 1,
		ResumeReason:    "worker-reconnect",
		Status:          "success",
		CreatedAt:       now.Add(-time.Second),
		UpdatedAt:       now,
		Outcome: harnessruntime.RunOutcomeDescriptor{
			RunStatus:       "success",
			Attempt:         3,
			ResumeFromEvent: 1,
			ResumeReason:    "worker-reconnect",
		},
	}
	serverA.saveRun(run)
	serverA.appendRunEvent(run.RunID, StreamEvent{
		ID:              run.RunID + ":2",
		Event:           "chunk",
		RunID:           run.RunID,
		ThreadID:        run.ThreadID,
		Attempt:         3,
		ResumeFromEvent: 1,
		ResumeReason:    "worker-reconnect",
		Data: map[string]any{
			"run_id":    run.RunID,
			"thread_id": run.ThreadID,
			"type":      "ai",
			"role":      "assistant",
			"delta":     "replay-stable",
			"content":   "replay-stable",
		},
	})
	serverA.appendRunEvent(run.RunID, StreamEvent{
		ID:              run.RunID + ":3",
		Event:           "end",
		RunID:           run.RunID,
		ThreadID:        run.ThreadID,
		Attempt:         3,
		ResumeFromEvent: 1,
		ResumeReason:    "worker-reconnect",
		Data: map[string]any{
			"run_id": run.RunID,
		},
	})

	const workers = 24
	errCh := make(chan string, workers*2)
	var wg sync.WaitGroup
	wg.Add(workers * 2)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if err := waitForCrossInstanceReplayAndRunGetConsistency(t, serverB.httpServer.Handler, run.ThreadID, run.RunID, 2*time.Second); err != nil {
				errCh <- "gateway-b " + err.Error()
			}
		}()
		go func() {
			defer wg.Done()
			if err := waitForCrossInstanceReplayAndRunGetConsistency(t, serverC.httpServer.Handler, run.ThreadID, run.RunID, 2*time.Second); err != nil {
				errCh <- "gateway-c " + err.Error()
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for errMsg := range errCh {
		t.Fatal(errMsg)
	}
}

func waitForCrossInstanceReplayAndRunGetConsistency(t *testing.T, handler any, threadID string, runID string, timeout time.Duration) error {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr string
	for {
		streamResp := performCompatRequest(t, handler, http.MethodGet, "/threads/"+threadID+"/runs/"+runID+"/stream?streamMode=events", nil, nil)
		if streamResp.Code != http.StatusOK {
			lastErr = "stream status=" + strconv.Itoa(streamResp.Code) + " body=" + streamResp.Body.String()
		} else {
			streamBody := streamResp.Body.String()
			if !strings.Contains(streamBody, "replay-stable") || strings.Count(streamBody, "event: end") != 1 {
				lastErr = "stream body=" + streamBody
			} else {
				getResp := performCompatRequest(t, handler, http.MethodGet, "/threads/"+threadID+"/runs/"+runID, nil, nil)
				if getResp.Code != http.StatusOK {
					lastErr = "get status=" + strconv.Itoa(getResp.Code) + " body=" + getResp.Body.String()
				} else {
					getBody := getResp.Body.String()
					if !strings.Contains(getBody, `"attempt":3`) || !strings.Contains(getBody, `"resume_from_event":1`) || !strings.Contains(getBody, `"resume_reason":"worker-reconnect"`) {
						lastErr = "get body=" + getBody
					} else {
						return nil
					}
				}
			}
		}
		if time.Now().After(deadline) {
			if lastErr == "" {
				lastErr = "timeout without detailed failure"
			}
			return fmt.Errorf("replay/get consistency timeout after %s: %s", timeout, lastErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
