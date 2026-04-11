package langgraphcompat

import (
	"net/http"
	"time"
)

func (s *Server) updateThreadState(threadID string, req map[string]any) (*ThreadState, int, string) {
	if threadID == "" {
		return nil, http.StatusBadRequest, "thread ID required"
	}

	s.sessionsMu.Lock()
	session, exists := s.sessions[threadID]
	if !exists {
		s.sessionsMu.Unlock()
		return nil, http.StatusNotFound, "thread not found"
	}

	s.applyThreadValues(session, extractThreadValues(req))
	metadata, _ := req["metadata"].(map[string]any)
	applyThreadMetadata(session, metadata)
	applyThreadConfigurable(session, req)
	applyThreadStatus(session, req)
	if !threadStateRequestProvidesCheckpoint(req) {
		clearSessionCheckpoint(session)
	}
	session.UpdatedAt = time.Now().UTC()
	s.sessionsMu.Unlock()

	if err := s.persistSessionSnapshot(session); err != nil {
		return nil, http.StatusInternalServerError, "failed to persist thread state"
	}
	if err := s.persistSessionFile(session); err != nil {
		return nil, http.StatusInternalServerError, "failed to persist thread state"
	}
	_ = s.appendThreadHistorySnapshot(threadID)
	return s.getThreadState(threadID), 0, ""
}

func (s *Server) threadHistorySlice(threadID string, limit int, limitProvided bool) ([]ThreadState, int, string) {
	state := s.getThreadState(threadID)
	if state == nil {
		return nil, http.StatusNotFound, "thread not found"
	}

	history := s.loadThreadHistory(threadID)
	if len(history) == 0 {
		history = []ThreadState{*state}
	}
	if !limitProvided && limit == 0 {
		limit = len(history)
	}
	if limit > len(history) {
		limit = len(history)
	}
	return history[:limit], 0, ""
}
