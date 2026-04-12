package langgraphcompat

import (
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

type compatRunStateStore struct {
	server *Server
	memory *harnessruntime.InMemoryRunStore
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
	return &compatRunStateStore{
		server: server,
		memory: harnessruntime.NewInMemoryRunStore(),
	}
}

func (s *compatRunStateStore) LoadRunSnapshot(runID string) (harnessruntime.RunSnapshot, bool) {
	if s == nil || s.memory == nil {
		return harnessruntime.RunSnapshot{}, false
	}
	return s.memory.LoadRunSnapshot(runID)
}

func (s *compatRunStateStore) ListRunSnapshots(threadID string) []harnessruntime.RunSnapshot {
	if s == nil || s.memory == nil {
		return nil
	}
	return s.memory.ListRunSnapshots(threadID)
}

func (s *compatRunStateStore) SaveRunSnapshot(snapshot harnessruntime.RunSnapshot) {
	if s == nil || s.memory == nil {
		return
	}
	if strings.TrimSpace(snapshot.Record.RunID) == "" {
		return
	}
	s.memory.SaveRunSnapshot(snapshot)
	if s.server != nil {
		s.server.saveRunSnapshotState(snapshot, true)
	}
}

func (s *compatRunStateStore) NextRunEventIndex(runID string) int {
	if s == nil || s.memory == nil {
		return 1
	}
	return s.memory.NextRunEventIndex(runID)
}

func (s *compatRunStateStore) AppendRunEvent(runID string, event harnessruntime.RunEvent) {
	if s == nil || s.memory == nil {
		return
	}
	snapshot, ok := s.memory.LoadRunSnapshot(runID)
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
	snapshot.Events = append(snapshot.Events, event)
	s.SaveRunSnapshot(snapshot)
}

func (s *compatRunStateStore) LoadRunEvents(runID string) []harnessruntime.RunEvent {
	if s == nil || s.memory == nil {
		return nil
	}
	return s.memory.LoadRunEvents(runID)
}

