package langgraphcompat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) handleMemoryGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.gatewayMemoryGet())
}

func (s *Server) handleMemoryExport(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.gatewayMemoryExport())
}

func (s *Server) handleMemoryImport(w http.ResponseWriter, r *http.Request) {
	var payload gatewayMemoryResponse
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	mem, err := s.gatewayMemoryImport(r.Context(), payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to import memory data"})
		return
	}
	writeJSON(w, http.StatusOK, mem)
}

func (s *Server) handleMemoryReload(w http.ResponseWriter, r *http.Request) {
	mem, err := s.gatewayMemoryReload(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	writeJSON(w, http.StatusOK, mem)
}

func (s *Server) handleMemoryClear(w http.ResponseWriter, r *http.Request) {
	mem, err := s.gatewayMemoryClear(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "Failed to clear memory data."})
		return
	}
	writeJSON(w, http.StatusOK, mem)
}

func (s *Server) handleMemoryFactCreate(w http.ResponseWriter, r *http.Request) {
	var req memoryFactCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	mem, err := s.gatewayMemoryCreateFact(r.Context(), req)
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

func (s *Server) handleMemoryFactDelete(w http.ResponseWriter, r *http.Request) {
	factID := strings.TrimSpace(r.PathValue("fact_id"))
	mem, err := s.gatewayMemoryDeleteFact(r.Context(), factID)
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

func (s *Server) handleMemoryFactUpdate(w http.ResponseWriter, r *http.Request) {
	factID := strings.TrimSpace(r.PathValue("fact_id"))
	var req memoryFactPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	mem, err := s.gatewayMemoryUpdateFact(r.Context(), factID, req)
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

func (s *Server) handleMemoryConfigGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.memoryConfig)
}

func (s *Server) handleMemoryStatusGet(w http.ResponseWriter, r *http.Request) {
	s.uiStateMu.RLock()
	mem := s.getMemoryLocked()
	s.uiStateMu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"config": s.memoryConfig,
		"data":   mem,
	})
}
