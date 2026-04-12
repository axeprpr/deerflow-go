package langgraphcompat

import (
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

type localRunSnapshotStore struct {
	server *Server
}

type localRunEventStore struct {
	snapshots harnessruntime.RunSnapshotStore
}

func (s *Server) ensureSnapshotStore() harnessruntime.RunSnapshotStore {
	if s == nil {
		return nil
	}
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	if s.snapshotStore == nil {
		s.snapshotStore = newLocalRunSnapshotStore(s)
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
			s.snapshotStore = newLocalRunSnapshotStore(s)
		}
		s.eventStore = newLocalRunEventStore(s.snapshotStore)
	}
	return s.eventStore
}

func newLocalRunSnapshotStore(server *Server) harnessruntime.RunSnapshotStore {
	return localRunSnapshotStore{server: server}
}

func newLocalRunEventStore(snapshots harnessruntime.RunSnapshotStore) harnessruntime.RunEventStore {
	return localRunEventStore{snapshots: snapshots}
}

func (s localRunSnapshotStore) LoadRunSnapshot(runID string) (harnessruntime.RunSnapshot, bool) {
	if s.server == nil {
		return harnessruntime.RunSnapshot{}, false
	}
	return s.server.loadRunSnapshotState(runID)
}

func (s localRunSnapshotStore) ListRunSnapshots(threadID string) []harnessruntime.RunSnapshot {
	if s.server == nil {
		return nil
	}
	return s.server.listRunSnapshotsState(threadID)
}

func (s localRunSnapshotStore) SaveRunSnapshot(snapshot harnessruntime.RunSnapshot) {
	if s.server == nil {
		return
	}
	s.server.saveRunSnapshotState(snapshot, true)
}

func (s localRunEventStore) NextRunEventIndex(runID string) int {
	if s.snapshots == nil {
		return 1
	}
	snapshot, ok := s.snapshots.LoadRunSnapshot(runID)
	if !ok {
		return 1
	}
	return len(snapshot.Events) + 1
}

func (s localRunEventStore) AppendRunEvent(runID string, event harnessruntime.RunEvent) {
	if s.snapshots == nil {
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
	snapshot.Events = append(snapshot.Events, event)
	s.snapshots.SaveRunSnapshot(snapshot)
}

func (s localRunEventStore) LoadRunEvents(runID string) []harnessruntime.RunEvent {
	if s.snapshots == nil {
		return nil
	}
	snapshot, ok := s.snapshots.LoadRunSnapshot(runID)
	if !ok {
		return nil
	}
	return append([]harnessruntime.RunEvent(nil), snapshot.Events...)
}
