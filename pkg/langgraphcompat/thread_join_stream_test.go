package langgraphcompat

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestThreadJoinStreamRejectsInvalidThreadID(t *testing.T) {
	_, handler := newCompatTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/threads/bad%20id/stream", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestThreadJoinStreamReturnsNotFoundForUnknownThread(t *testing.T) {
	_, handler := newCompatTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/threads/missing-thread/stream", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestThreadJoinStreamIgnoresCompletedLatestRun(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-join-finished", nil)
	now := time.Now().UTC()
	s.saveRun(&Run{
		RunID:     "run-completed",
		ThreadID:  "thread-join-finished",
		Status:    "success",
		CreatedAt: now,
		UpdatedAt: now,
		Events: []StreamEvent{{
			ID:       "run-completed:1",
			Event:    "messages-tuple",
			Data:     Message{Type: "ai", ID: "msg-finished", Role: "assistant", Content: "stale"},
			RunID:    "run-completed",
			ThreadID: "thread-join-finished",
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/threads/thread-join-finished/stream", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, ": no active run") {
		t.Fatalf("expected no active run marker in %q", body)
	}
	if strings.Contains(body, "msg-finished") {
		t.Fatalf("expected completed run events to be skipped, body=%q", body)
	}
}

func TestThreadJoinStreamFollowsLatestActiveRunOnly(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-join-active", nil)
	now := time.Now().UTC()
	s.saveRun(&Run{
		RunID:     "run-old-running",
		ThreadID:  "thread-join-active",
		Status:    "running",
		CreatedAt: now.Add(-2 * time.Minute),
		UpdatedAt: now.Add(-2 * time.Minute),
	})
	s.saveRun(&Run{
		RunID:     "run-new-completed",
		ThreadID:  "thread-join-active",
		Status:    "success",
		CreatedAt: now.Add(-1 * time.Minute),
		UpdatedAt: now.Add(-1 * time.Minute),
		Events: []StreamEvent{{
			ID:       "run-new-completed:1",
			Event:    "messages-tuple",
			Data:     Message{Type: "ai", ID: "msg-completed", Role: "assistant", Content: "completed"},
			RunID:    "run-new-completed",
			ThreadID: "thread-join-active",
		}},
	})
	s.saveRun(&Run{
		RunID:     "run-new-running",
		ThreadID:  "thread-join-active",
		Status:    "running",
		CreatedAt: now,
		UpdatedAt: now,
	})

	bodyCh := make(chan string, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/threads/thread-join-active/stream", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		bodyCh <- rec.Body.String()
	}()

	waitForRunSubscriber(t, s, "run-new-running")
	if got := s.runSubscriberCount("run-old-running"); got != 0 {
		t.Fatalf("expected old running run to have no subscribers, got %d", got)
	}

	s.appendRunEvent("run-new-running", StreamEvent{
		ID:       "run-new-running:1",
		Event:    "messages-tuple",
		Data:     Message{Type: "ai", ID: "msg-live", Role: "assistant", Content: "live"},
		RunID:    "run-new-running",
		ThreadID: "thread-join-active",
	})
	s.appendRunEvent("run-new-running", StreamEvent{
		ID:       "run-new-running:2",
		Event:    "end",
		Data:     map[string]any{},
		RunID:    "run-new-running",
		ThreadID: "thread-join-active",
	})

	select {
	case body := <-bodyCh:
		if !strings.Contains(body, `"content":"live"`) {
			t.Fatalf("expected latest active run payload in %q", body)
		}
		if strings.Contains(body, "completed") {
			t.Fatalf("expected completed run payload to be skipped in %q", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for active join stream response")
	}
}

func TestThreadJoinStreamPrefersOwnedActiveRun(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-join-owned-active"
	s.ensureSession(threadID, nil)
	now := time.Now().UTC()
	s.saveRun(&Run{
		RunID:     "run-owned-running",
		ThreadID:  threadID,
		Status:    "running",
		CreatedAt: now.Add(-time.Minute),
		UpdatedAt: now.Add(-time.Minute),
	})
	s.saveRun(&Run{
		RunID:     "run-newer-running",
		ThreadID:  threadID,
		Status:    "running",
		CreatedAt: now,
		UpdatedAt: now,
	})
	s.ensureThreadStateStore().SetThreadMetadata(threadID, harnessruntime.DefaultActiveRunMetadataKey, "run-owned-running")

	bodyCh := make(chan string, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/threads/"+threadID+"/stream", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		bodyCh <- rec.Body.String()
	}()

	waitForRunSubscriber(t, s, "run-owned-running")
	if got := s.runSubscriberCount("run-newer-running"); got != 0 {
		t.Fatalf("expected newer run to have no subscribers, got %d", got)
	}

	s.appendRunEvent("run-owned-running", StreamEvent{
		ID:       "run-owned-running:1",
		Event:    "messages-tuple",
		Data:     Message{Type: "ai", ID: "msg-owned", Role: "assistant", Content: "owned-run"},
		RunID:    "run-owned-running",
		ThreadID: threadID,
	})
	s.appendRunEvent("run-owned-running", StreamEvent{
		ID:       "run-owned-running:2",
		Event:    "end",
		Data:     map[string]any{"run_id": "run-owned-running"},
		RunID:    "run-owned-running",
		ThreadID: threadID,
	})

	select {
	case body := <-bodyCh:
		if !strings.Contains(body, `"content":"owned-run"`) {
			t.Fatalf("expected owned active run payload in %q", body)
		}
		if strings.Contains(body, "run-newer-running") {
			t.Fatalf("expected newer run payload to be skipped in %q", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for owned active join stream response")
	}
}

func TestThreadJoinStreamFallsBackWhenOwnedActiveRunIsMissing(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-join-owned-missing"
	s.ensureSession(threadID, nil)
	now := time.Now().UTC()
	s.saveRun(&Run{
		RunID:     "run-older-running",
		ThreadID:  threadID,
		Status:    "running",
		CreatedAt: now.Add(-time.Minute),
		UpdatedAt: now.Add(-time.Minute),
	})
	s.saveRun(&Run{
		RunID:     "run-latest-running",
		ThreadID:  threadID,
		Status:    "running",
		CreatedAt: now,
		UpdatedAt: now,
	})
	s.ensureThreadStateStore().SetThreadMetadata(threadID, harnessruntime.DefaultActiveRunMetadataKey, "run-missing")

	bodyCh := make(chan string, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/threads/"+threadID+"/stream", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		bodyCh <- rec.Body.String()
	}()

	waitForRunSubscriber(t, s, "run-latest-running")
	if got := s.runSubscriberCount("run-older-running"); got != 0 {
		t.Fatalf("expected older run to have no subscribers, got %d", got)
	}

	s.appendRunEvent("run-latest-running", StreamEvent{
		ID:       "run-latest-running:1",
		Event:    "messages-tuple",
		Data:     Message{Type: "ai", ID: "msg-latest", Role: "assistant", Content: "latest-run"},
		RunID:    "run-latest-running",
		ThreadID: threadID,
	})
	s.appendRunEvent("run-latest-running", StreamEvent{
		ID:       "run-latest-running:2",
		Event:    "end",
		Data:     map[string]any{"run_id": "run-latest-running"},
		RunID:    "run-latest-running",
		ThreadID: threadID,
	})

	select {
	case body := <-bodyCh:
		if !strings.Contains(body, `"content":"latest-run"`) {
			t.Fatalf("expected fallback latest run payload in %q", body)
		}
		if strings.Contains(body, "run-older-running") {
			t.Fatalf("expected older run payload to be skipped in %q", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for fallback join stream response")
	}
}
