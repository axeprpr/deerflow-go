package langgraphcompat

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleThreadGet(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		writeDetailError(w, http.StatusBadRequest, "thread ID required")
		return
	}
	if err := validateThreadID(threadID); err != nil {
		writeDetailError(w, threadIDStatusCode(r), err.Error())
		return
	}

	thread, exists := s.findThreadResponse(threadID)
	if !exists {
		writeDetailError(w, http.StatusNotFound, "thread not found")
		return
	}
	writeJSON(w, http.StatusOK, thread)
}

func (s *Server) handleThreadCreate(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if r.Body != nil {
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	thread, code, detail := s.createThread(req)
	if detail != "" {
		if detail == "invalid thread_id" && code == 0 {
			code = threadIDStatusCode(r)
		}
		writeDetailError(w, code, detail)
		return
	}
	writeJSON(w, http.StatusCreated, thread)
}

func (s *Server) handleThreadUpdate(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		writeDetailError(w, http.StatusBadRequest, "thread ID required")
		return
	}
	if err := validateThreadID(threadID); err != nil {
		writeDetailError(w, threadIDStatusCode(r), err.Error())
		return
	}

	var req map[string]any
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDetailError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	thread, code, detail := s.updateThread(threadID, req)
	if detail != "" {
		writeDetailError(w, code, detail)
		return
	}
	writeJSON(w, http.StatusOK, thread)
}

func (s *Server) handleThreadDelete(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		writeDetailError(w, http.StatusBadRequest, "thread ID required")
		return
	}
	if err := validateThreadID(threadID); err != nil {
		writeDetailError(w, threadIDStatusCode(r), err.Error())
		return
	}

	code, detail := s.deleteThread(threadID)
	if detail != "" {
		writeDetailError(w, code, detail)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
