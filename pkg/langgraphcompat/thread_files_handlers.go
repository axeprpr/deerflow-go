package langgraphcompat

import "net/http"

func (s *Server) handleThreadFiles(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		writeDetailError(w, http.StatusBadRequest, "thread ID required")
		return
	}
	if err := validateThreadID(threadID); err != nil {
		writeDetailError(w, threadIDStatusCode(r), err.Error())
		return
	}

	s.sessionsMu.RLock()
	session, exists := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if !exists {
		writeDetailError(w, http.StatusNotFound, "thread not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"files": s.collectSessionFiles(session),
	})
}
