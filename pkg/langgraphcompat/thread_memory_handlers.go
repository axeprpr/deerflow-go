package langgraphcompat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) threadScopedMemoryScope(w http.ResponseWriter, r *http.Request) (string, bool) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	if threadID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "thread ID required"})
		return "", false
	}
	if err := validateThreadID(threadID); err != nil {
		writeDetailError(w, threadIDStatusCode(r), err.Error())
		return "", false
	}
	return threadID, true
}

func (s *Server) handleThreadMemoryGet(w http.ResponseWriter, r *http.Request) {
	threadID, ok := s.threadScopedMemoryScope(w, r)
	if !ok {
		return
	}
	scope, err := s.gatewayMemoryScopeFromThreadID(threadID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.gatewayMemoryGet(r.Context(), scope))
}

func (s *Server) handleThreadMemoryExport(w http.ResponseWriter, r *http.Request) {
	threadID, ok := s.threadScopedMemoryScope(w, r)
	if !ok {
		return
	}
	scope, err := s.gatewayMemoryScopeFromThreadID(threadID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.gatewayMemoryExport(r.Context(), scope))
}

func (s *Server) handleThreadMemoryImport(w http.ResponseWriter, r *http.Request) {
	threadID, ok := s.threadScopedMemoryScope(w, r)
	if !ok {
		return
	}
	scope, err := s.gatewayMemoryScopeFromThreadID(threadID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	var payload gatewayMemoryResponse
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	mem, err := s.gatewayMemoryImport(r.Context(), scope, payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "Failed to import memory data."})
		return
	}
	writeJSON(w, http.StatusOK, mem)
}

func (s *Server) handleThreadMemoryReload(w http.ResponseWriter, r *http.Request) {
	threadID, ok := s.threadScopedMemoryScope(w, r)
	if !ok {
		return
	}
	scope, err := s.gatewayMemoryScopeFromThreadID(threadID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	mem, err := s.gatewayMemoryReload(r.Context(), scope)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "Failed to reload memory data."})
		return
	}
	writeJSON(w, http.StatusOK, mem)
}

func (s *Server) handleThreadMemoryClear(w http.ResponseWriter, r *http.Request) {
	threadID, ok := s.threadScopedMemoryScope(w, r)
	if !ok {
		return
	}
	scope, err := s.gatewayMemoryScopeFromThreadID(threadID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	mem, err := s.gatewayMemoryClear(r.Context(), scope)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "Failed to clear memory data."})
		return
	}
	writeJSON(w, http.StatusOK, mem)
}

func (s *Server) handleThreadMemoryFactCreate(w http.ResponseWriter, r *http.Request) {
	threadID, ok := s.threadScopedMemoryScope(w, r)
	if !ok {
		return
	}
	scope, err := s.gatewayMemoryScopeFromThreadID(threadID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	var req memoryFactCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	mem, err := s.gatewayMemoryCreateFact(r.Context(), scope, req)
	if err != nil {
		status := http.StatusInternalServerError
		detail := "Failed to create memory fact."
		switch err.Error() {
		case "Memory fact content cannot be empty.", "Invalid confidence value; must be between 0 and 1.":
			status = http.StatusBadRequest
			detail = err.Error()
		}
		writeJSON(w, status, map[string]any{"detail": detail})
		return
	}
	writeJSON(w, http.StatusOK, mem)
}

func (s *Server) handleThreadMemoryFactDelete(w http.ResponseWriter, r *http.Request) {
	threadID, ok := s.threadScopedMemoryScope(w, r)
	if !ok {
		return
	}
	scope, err := s.gatewayMemoryScopeFromThreadID(threadID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	factID := strings.TrimSpace(r.PathValue("fact_id"))
	mem, err := s.gatewayMemoryDeleteFact(r.Context(), scope, factID)
	if err != nil {
		if err.Error() == fmt.Sprintf("Memory fact '%s' not found", factID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"detail": fmt.Sprintf("Memory fact '%s' not found.", factID)})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "Failed to delete memory fact."})
		return
	}
	writeJSON(w, http.StatusOK, mem)
}

func (s *Server) handleThreadMemoryFactUpdate(w http.ResponseWriter, r *http.Request) {
	threadID, ok := s.threadScopedMemoryScope(w, r)
	if !ok {
		return
	}
	scope, err := s.gatewayMemoryScopeFromThreadID(threadID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	factID := strings.TrimSpace(r.PathValue("fact_id"))
	var req memoryFactPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	mem, err := s.gatewayMemoryUpdateFact(r.Context(), scope, factID, req)
	if err != nil {
		status := http.StatusInternalServerError
		detail := "Failed to update memory fact."
		switch err.Error() {
		case fmt.Sprintf("Memory fact '%s' not found", factID):
			status = http.StatusNotFound
			detail = err.Error()
		case "Memory fact content cannot be empty.", "Invalid confidence value; must be between 0 and 1.":
			status = http.StatusBadRequest
			detail = err.Error()
		}
		writeJSON(w, status, map[string]any{"detail": detail})
		return
	}
	writeJSON(w, http.StatusOK, mem)
}
