package langgraphcompat

import (
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func (s *Server) loadRunSnapshotState(runID string) (harnessruntime.RunSnapshot, bool) {
	runID = strings.TrimSpace(runID)
	if s == nil || runID == "" {
		return harnessruntime.RunSnapshot{}, false
	}
	s.runsMu.RLock()
	defer s.runsMu.RUnlock()
	run, exists := s.runs[runID]
	if !exists {
		return harnessruntime.RunSnapshot{}, false
	}
	return runSnapshotFromRun(run), true
}

func (s *Server) listRunSnapshotsState(threadID string) []harnessruntime.RunSnapshot {
	if s == nil {
		return nil
	}
	threadID = strings.TrimSpace(threadID)
	s.runsMu.RLock()
	defer s.runsMu.RUnlock()

	snapshots := make([]harnessruntime.RunSnapshot, 0, len(s.runs))
	for _, run := range s.runs {
		if threadID != "" && run.ThreadID != threadID {
			continue
		}
		snapshots = append(snapshots, runSnapshotFromRun(run))
	}
	return snapshots
}

func (s *Server) saveRunSnapshotState(snapshot harnessruntime.RunSnapshot, persist bool) {
	if s == nil {
		return
	}
	run := runFromSnapshot(snapshot)
	if run == nil || strings.TrimSpace(run.RunID) == "" {
		return
	}
	s.runsMu.Lock()
	copyRun := *run
	copyRun.Events = append([]StreamEvent(nil), run.Events...)
	s.runs[run.RunID] = &copyRun
	s.runsMu.Unlock()
	if persist {
		_ = s.persistRunSnapshot(snapshot)
	}
}

func (s *Server) appendRunEventState(runID string, event StreamEvent, persist bool) {
	if s == nil || strings.TrimSpace(runID) == "" {
		return
	}
	s.runsMu.Lock()
	if run, exists := s.runs[runID]; exists {
		run.Events = append(run.Events, event)
		run.UpdatedAt = time.Now().UTC()
		if persist {
			_ = s.persistRunFile(run)
		}
	}
	s.runsMu.Unlock()
}

