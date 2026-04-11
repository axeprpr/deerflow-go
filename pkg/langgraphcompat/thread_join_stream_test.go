package langgraphcompat

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
