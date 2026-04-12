package langgraphcompat

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func (s *Server) saveRun(run *Run) {
	if s == nil {
		return
	}
	if store := s.ensureSnapshotStore(); store != nil {
		store.SaveRunSnapshot(runSnapshotFromRun(run))
		return
	}
	s.saveRunSnapshotState(runSnapshotFromRun(run), true)
}

func (s *Server) setRunCancel(runID string, cancel context.CancelFunc) {
	s.ensureRunRegistry().setCancel(runID, cancel)
}

func (s *Server) clearRunCancel(runID string) {
	s.ensureRunRegistry().clearCancel(runID)
}

func (s *Server) cancelActiveRun(runID string) bool {
	return s.ensureRunRegistry().cancel(runID)
}

func (s *Server) appendRunEvent(runID string, event StreamEvent) {
	if s == nil {
		return
	}
	if store := s.ensureEventStore(); store != nil {
		store.AppendRunEvent(runID, runtimeEventFromStreamEvent(event))
	} else {
		s.appendRunEventState(runID, event, true)
	}
	s.ensureRunRegistry().publish(runID, event)
}

func (s *Server) nextRunEventIndex(runID string) int {
	if s == nil {
		return 1
	}
	if store := s.ensureEventStore(); store != nil {
		return store.NextRunEventIndex(runID)
	}
	if snapshot, ok := s.loadRunSnapshotState(runID); ok {
		return len(snapshot.Events) + 1
	}
	return 1
}

func (s *Server) getRun(runID string) *Run {
	if s == nil {
		return nil
	}
	if store := s.ensureSnapshotStore(); store != nil {
		snapshot, ok := store.LoadRunSnapshot(runID)
		if !ok {
			return nil
		}
		return runFromSnapshot(snapshot)
	}
	snapshot, ok := s.loadRunSnapshotState(runID)
	if !ok {
		return nil
	}
	return runFromSnapshot(snapshot)
}

func (s *Server) getLatestRunForThread(threadID string) *Run {
	if s == nil {
		return nil
	}
	var snapshots []harnessruntime.RunSnapshot
	if store := s.ensureSnapshotStore(); store != nil {
		snapshots = store.ListRunSnapshots(threadID)
	}
	var latest harnessruntime.RunSnapshot
	found := false
	for _, snapshot := range snapshots {
		if !found || snapshot.Record.CreatedAt.After(latest.Record.CreatedAt) {
			latest = snapshot
			found = true
		}
	}
	if !found {
		return nil
	}
	return runFromSnapshot(latest)
}
