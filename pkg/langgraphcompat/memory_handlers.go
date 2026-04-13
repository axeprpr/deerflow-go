package langgraphcompat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
)

func (s *Server) handleMemoryGet(w http.ResponseWriter, r *http.Request) {
	scope, ok := decodeGatewayMemoryScope(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.gatewayMemoryGet(r.Context(), scope))
}

func (s *Server) handleMemoryExport(w http.ResponseWriter, r *http.Request) {
	scope, ok := decodeGatewayMemoryScope(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.gatewayMemoryExport(r.Context(), scope))
}

func (s *Server) handleMemoryImport(w http.ResponseWriter, r *http.Request) {
	scope, ok := decodeGatewayMemoryScope(w, r)
	if !ok {
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

func (s *Server) handleMemoryReload(w http.ResponseWriter, r *http.Request) {
	scope, ok := decodeGatewayMemoryScope(w, r)
	if !ok {
		return
	}
	mem, err := s.gatewayMemoryReload(r.Context(), scope)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "Failed to reload memory data."})
		return
	}
	writeJSON(w, http.StatusOK, mem)
}

func (s *Server) handleMemoryClear(w http.ResponseWriter, r *http.Request) {
	scope, ok := decodeGatewayMemoryScope(w, r)
	if !ok {
		return
	}
	mem, err := s.gatewayMemoryClear(r.Context(), scope)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "Failed to clear memory data."})
		return
	}
	writeJSON(w, http.StatusOK, mem)
}

func (s *Server) handleMemoryFactCreate(w http.ResponseWriter, r *http.Request) {
	scope, ok := decodeGatewayMemoryScope(w, r)
	if !ok {
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

func (s *Server) handleMemoryFactDelete(w http.ResponseWriter, r *http.Request) {
	scope, ok := decodeGatewayMemoryScope(w, r)
	if !ok {
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

func (s *Server) handleMemoryFactUpdate(w http.ResponseWriter, r *http.Request) {
	scope, ok := decodeGatewayMemoryScope(w, r)
	if !ok {
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

func decodeGatewayMemoryScope(w http.ResponseWriter, r *http.Request) (pkgmemory.Scope, bool) {
	scope, err := gatewayMemoryScopeFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return pkgmemory.Scope{}, false
	}
	return scope, true
}
