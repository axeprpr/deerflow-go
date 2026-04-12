package langgraphcompat

import "net/http"

func (s *Server) handleThreadClarificationCreate(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		writeDetailError(w, http.StatusBadRequest, "thread ID required")
		return
	}
	if s.getThreadState(threadID) == nil {
		writeDetailError(w, http.StatusNotFound, "thread not found")
		return
	}
	s.clarifyAPI.HandleCreate(w, r, threadID)
}

func (s *Server) handleThreadClarificationsList(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		writeDetailError(w, http.StatusBadRequest, "thread ID required")
		return
	}
	if s.getThreadState(threadID) == nil {
		writeDetailError(w, http.StatusNotFound, "thread not found")
		return
	}
	s.clarifyAPI.HandleList(w, r, threadID)
}

func (s *Server) handleThreadClarificationGet(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		writeDetailError(w, http.StatusBadRequest, "thread ID required")
		return
	}
	s.clarifyAPI.HandleGet(w, r, threadID, r.PathValue("id"))
}

func (s *Server) handleThreadClarificationResolve(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		writeDetailError(w, http.StatusBadRequest, "thread ID required")
		return
	}
	s.clarifyAPI.HandleResolve(w, r, threadID, r.PathValue("id"))
}
