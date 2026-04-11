package langgraphcompat

import (
	"context"
	"time"
)

func (s *Server) saveRun(run *Run) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	copyRun := *run
	copyRun.Events = append([]StreamEvent(nil), run.Events...)
	s.runs[run.RunID] = &copyRun
	_ = s.persistRunFile(&copyRun)
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
	s.runsMu.Lock()
	if run, exists := s.runs[runID]; exists {
		run.Events = append(run.Events, event)
		run.UpdatedAt = time.Now().UTC()
		_ = s.persistRunFile(run)
	}
	s.runsMu.Unlock()
	s.ensureRunRegistry().publish(runID, event)
}

func (s *Server) nextRunEventIndex(runID string) int {
	s.runsMu.RLock()
	defer s.runsMu.RUnlock()
	if run, exists := s.runs[runID]; exists {
		return len(run.Events) + 1
	}
	return 1
}

func (s *Server) getRun(runID string) *Run {
	s.runsMu.RLock()
	defer s.runsMu.RUnlock()
	run, exists := s.runs[runID]
	if !exists {
		return nil
	}
	copyRun := *run
	copyRun.Events = append([]StreamEvent(nil), run.Events...)
	return &copyRun
}

func (s *Server) getLatestRunForThread(threadID string) *Run {
	s.runsMu.RLock()
	defer s.runsMu.RUnlock()

	var latest *Run
	for _, run := range s.runs {
		if run.ThreadID != threadID {
			continue
		}
		if latest == nil || run.CreatedAt.After(latest.CreatedAt) {
			copyRun := *run
			copyRun.Events = append([]StreamEvent(nil), run.Events...)
			latest = &copyRun
		}
	}
	return latest
}
