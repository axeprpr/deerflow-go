package langgraphcompat

import "net/http"

func (s *Server) handleThreadFiles(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), threadIDStatusCode(r))
		return
	}

	s.sessionsMu.RLock()
	session, exists := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if !exists {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"files": s.collectSessionFiles(session),
	})
}
