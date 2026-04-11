package langgraphcompat

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleThreadGet(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), threadIDStatusCode(r))
		return
	}

	thread, exists := s.findThreadResponse(threadID)
	if !exists {
		http.Error(w, "thread not found", http.StatusNotFound)
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
		http.Error(w, detail, code)
		return
	}
	writeJSON(w, http.StatusCreated, thread)
}

func (s *Server) handleThreadUpdate(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), threadIDStatusCode(r))
		return
	}

	var req map[string]any
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	thread, code, detail := s.updateThread(threadID, req)
	if detail != "" {
		http.Error(w, detail, code)
		return
	}
	writeJSON(w, http.StatusOK, thread)
}

func (s *Server) handleThreadDelete(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), threadIDStatusCode(r))
		return
	}

	code, detail := s.deleteThread(threadID)
	if detail != "" {
		http.Error(w, detail, code)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
