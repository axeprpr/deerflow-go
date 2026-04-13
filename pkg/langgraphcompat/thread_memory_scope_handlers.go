package langgraphcompat

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleThreadMemoryScopeGet(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		writeDetailError(w, http.StatusBadRequest, "thread ID required")
		return
	}
	if err := validateThreadID(threadID); err != nil {
		writeDetailError(w, threadIDStatusCode(r), err.Error())
		return
	}
	scope, code, detail := s.getThreadMemoryScope(threadID)
	if detail != "" {
		writeDetailError(w, code, detail)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memory_scope": scope.Response()})
}

func (s *Server) handleThreadMemoryScopePut(w http.ResponseWriter, r *http.Request) {
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
	scope := threadMemoryScopeFromAny(firstNonNil(req["memory_scope"], req["memoryScope"]))
	resp, code, detail := s.updateThreadMemoryScope(threadID, scope)
	if detail != "" {
		writeDetailError(w, code, detail)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleThreadMemoryScopeDelete(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		writeDetailError(w, http.StatusBadRequest, "thread ID required")
		return
	}
	if err := validateThreadID(threadID); err != nil {
		writeDetailError(w, threadIDStatusCode(r), err.Error())
		return
	}
	resp, code, detail := s.deleteThreadMemoryScope(threadID)
	if detail != "" {
		writeDetailError(w, code, detail)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
