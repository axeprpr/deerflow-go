package langgraphcompat

import (
	"net/http"
	"time"
)

func (s *Server) getThreadMemoryScope(threadID string) (threadMemoryScope, int, string) {
	s.sessionsMu.RLock()
	session, exists := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if !exists {
		return threadMemoryScope{}, http.StatusNotFound, "thread not found"
	}
	return threadMemoryScopeFromSession(session), 0, ""
}

func (s *Server) updateThreadMemoryScope(threadID string, scope threadMemoryScope) (map[string]any, int, string) {
	s.sessionsMu.Lock()
	session, exists := s.sessions[threadID]
	if !exists {
		s.sessionsMu.Unlock()
		return nil, http.StatusNotFound, "thread not found"
	}
	applyThreadMemoryScope(session, scope)
	session.UpdatedAt = time.Now().UTC()
	s.sessionsMu.Unlock()

	if err := s.persistSessionFile(session); err != nil {
		return nil, http.StatusInternalServerError, "failed to persist thread"
	}
	return map[string]any{"memory_scope": scope.Response()}, 0, ""
}

func (s *Server) deleteThreadMemoryScope(threadID string) (map[string]any, int, string) {
	return s.updateThreadMemoryScope(threadID, threadMemoryScope{})
}
