package langgraphcompat

import (
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

type compatRunStateStore struct {
	server        *Server
	snapshots     harnessruntime.RunSnapshotStore
	events        harnessruntime.RunEventStore
	replaceEvents harnessruntime.RunEventReplaceStore
}

func (s *Server) ensureSnapshotStore() harnessruntime.RunSnapshotStore {
	if s == nil {
		return nil
	}
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	if s.snapshotStore == nil {
		s.snapshotStore = newCompatRunStateStore(s)
	}
	return s.snapshotStore
}

func (s *Server) ensureEventStore() harnessruntime.RunEventStore {
	if s == nil {
		return nil
	}
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	if s.eventStore == nil {
		if s.snapshotStore == nil {
			s.snapshotStore = newCompatRunStateStore(s)
		}
		s.eventStore = s.snapshotStore.(harnessruntime.RunEventStore)
	}
	return s.eventStore
}

func newCompatRunStateStore(server *Server) *compatRunStateStore {
	var store harnessruntime.RunSnapshotStore
	var events harnessruntime.RunEventStore
	if server != nil {
		store = server.runtimeNode.BuildRunSnapshotStore()
		events = server.runtimeNode.BuildRunEventStore()
	}
	if store == nil {
		store = harnessruntime.NewInMemoryRunStore()
	}
	if events == nil {
		if shared, ok := store.(harnessruntime.RunEventStore); ok {
			events = shared
		}
	}
	return &compatRunStateStore{
		server:        server,
		snapshots:     store,
		events:        events,
		replaceEvents: asRunEventReplaceStore(events),
	}
}

func (s *compatRunStateStore) LoadRunSnapshot(runID string) (harnessruntime.RunSnapshot, bool) {
	if s == nil || s.snapshots == nil {
		return harnessruntime.RunSnapshot{}, false
	}
	snapshot, ok := s.snapshots.LoadRunSnapshot(runID)
	if !ok {
		return harnessruntime.RunSnapshot{}, false
	}
	if s.events != nil {
		snapshot.Events = s.events.LoadRunEvents(runID)
	}
	return snapshot, true
}

func (s *compatRunStateStore) ListRunSnapshots(threadID string) []harnessruntime.RunSnapshot {
	if s == nil || s.snapshots == nil {
		return nil
	}
	snapshots := s.snapshots.ListRunSnapshots(threadID)
	if s.events == nil {
		return snapshots
	}
	for i := range snapshots {
		snapshots[i].Events = s.events.LoadRunEvents(snapshots[i].Record.RunID)
	}
	return snapshots
}

func (s *compatRunStateStore) SaveRunSnapshot(snapshot harnessruntime.RunSnapshot) {
	if s == nil || s.snapshots == nil {
		return
	}
	if strings.TrimSpace(snapshot.Record.RunID) == "" {
		return
	}
	s.snapshots.SaveRunSnapshot(snapshot)
	if s.replaceEvents != nil {
		s.replaceEvents.ReplaceRunEvents(snapshot.Record.RunID, snapshot.Events)
	}
	if s.server != nil {
		s.server.saveRunSnapshotState(snapshot, true)
	}
}

func (s *compatRunStateStore) NextRunEventIndex(runID string) int {
	if s == nil || s.events == nil {
		return 1
	}
	return s.events.NextRunEventIndex(runID)
}

func (s *compatRunStateStore) AppendRunEvent(runID string, event harnessruntime.RunEvent) {
	if s == nil || s.snapshots == nil {
		return
	}
	snapshot, ok := s.snapshots.LoadRunSnapshot(runID)
	if !ok {
		snapshot = harnessruntime.RunSnapshot{}
	}
	if strings.TrimSpace(snapshot.Record.RunID) == "" {
		snapshot.Record.RunID = strings.TrimSpace(runID)
	}
	if strings.TrimSpace(snapshot.Record.ThreadID) == "" {
		snapshot.Record.ThreadID = strings.TrimSpace(event.ThreadID)
	}
	snapshot.Record.UpdatedAt = time.Now().UTC()
	if s.events != nil {
		s.events.AppendRunEvent(runID, event)
		snapshot.Events = s.events.LoadRunEvents(runID)
	} else {
		snapshot.Events = append(snapshot.Events, event)
	}
	s.SaveRunSnapshot(snapshot)
}

func (s *compatRunStateStore) LoadRunEvents(runID string) []harnessruntime.RunEvent {
	if s == nil || s.events == nil {
		return nil
	}
	return s.events.LoadRunEvents(runID)
}

func asRunEventReplaceStore(store harnessruntime.RunEventStore) harnessruntime.RunEventReplaceStore {
	replace, _ := store.(harnessruntime.RunEventReplaceStore)
	return replace
}
