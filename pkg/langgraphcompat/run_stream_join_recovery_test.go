package langgraphcompat

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

type silentReplayEventStore struct {
	mu     sync.Mutex
	events map[string][]harnessruntime.RunEvent
}

func newSilentReplayEventStore() *silentReplayEventStore {
	return &silentReplayEventStore{events: map[string][]harnessruntime.RunEvent{}}
}

func (s *silentReplayEventStore) NextRunEventIndex(runID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events[runID]) + 1
}

func (s *silentReplayEventStore) AppendRunEvent(runID string, event harnessruntime.RunEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[runID] = append(s.events[runID], event)
}

func (s *silentReplayEventStore) LoadRunEvents(runID string) []harnessruntime.RunEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]harnessruntime.RunEvent(nil), s.events[runID]...)
}

func (s *silentReplayEventStore) SubscribeRunEvents(_ string, _ int) (<-chan harnessruntime.RunEvent, func()) {
	ch := make(chan harnessruntime.RunEvent)
	return ch, func() { close(ch) }
}

func TestRunReplayStreamerJoinBackfillsFromStoreWhenSubscriptionIsSilent(t *testing.T) {
	prev := sseHeartbeatInterval
	sseHeartbeatInterval = 20 * time.Millisecond
	defer func() {
		sseHeartbeatInterval = prev
	}()

	store := newSilentReplayEventStore()
	server := &Server{
		runs:        map[string]*Run{},
		runRegistry: newRunRegistry(),
		eventStore:  store,
	}
	run := &Run{
		RunID:       "run-join-backfill",
		ThreadID:    "thread-join-backfill",
		AssistantID: "assistant-1",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Outcome:     harnessruntime.RunOutcomeDescriptor{RunStatus: "running"},
	}

	recorder := &flushBuffer{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.newRunReplayStreamer(recorder, recorder, newStreamModeFilter(nil)).Join(run)
	}()

	time.Sleep(30 * time.Millisecond)
	store.AppendRunEvent(run.RunID, harnessruntime.RunEvent{
		ID:       run.RunID + ":1",
		Event:    "chunk",
		RunID:    run.RunID,
		ThreadID: run.ThreadID,
		Data: map[string]any{
			"run_id":    run.RunID,
			"thread_id": run.ThreadID,
			"type":      "ai",
			"role":      "assistant",
			"delta":     "recovered hello",
			"content":   "recovered hello",
		},
	})
	store.AppendRunEvent(run.RunID, harnessruntime.RunEvent{
		ID:       run.RunID + ":2",
		Event:    "end",
		RunID:    run.RunID,
		ThreadID: run.ThreadID,
		Data: map[string]any{
			"run_id": run.RunID,
		},
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Join() did not return after backfilled end event")
	}

	body := recorder.String()
	if !strings.Contains(body, "event: chunk") || !strings.Contains(body, "recovered hello") {
		t.Fatalf("missing recovered chunk event: %q", body)
	}
	if !strings.Contains(body, "event: end") {
		t.Fatalf("missing recovered end event: %q", body)
	}
}
