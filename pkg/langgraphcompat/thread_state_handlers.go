package langgraphcompat

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

func (s *Server) handleThreadStateGet(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), threadIDStatusCode(r))
		return
	}

	state := s.getThreadState(threadID)
	if state == nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleThreadStatePost(w http.ResponseWriter, r *http.Request) {
	s.handleThreadStateWrite(w, r)
}

func (s *Server) handleThreadStatePatch(w http.ResponseWriter, r *http.Request) {
	s.handleThreadStateWrite(w, r)
}

func (s *Server) handleThreadStateWrite(w http.ResponseWriter, r *http.Request) {
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

	state, code, detail := s.updateThreadState(threadID, req)
	if detail != "" {
		http.Error(w, detail, code)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleThreadHistory(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), threadIDStatusCode(r))
		return
	}

	limit, limitProvided := decodeThreadHistoryRequest(r)
	history, code, detail := s.threadHistorySlice(threadID, limit, limitProvided)
	if detail != "" {
		http.Error(w, detail, code)
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func decodeThreadHistoryRequest(r *http.Request) (int, bool) {
	var raw map[string]any
	var req struct {
		Limit     int `json:"limit"`
		PageSize  int `json:"page_size"`
		PageSizeX int `json:"pageSize"`
	}
	limitProvided := false
	if r.Body != nil {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 {
			_ = json.Unmarshal(body, &raw)
			_ = json.Unmarshal(body, &req)
			_, hasLimit := raw["limit"]
			_, hasPageSize := raw["pageSize"]
			_, hasPageSizeSnake := raw["page_size"]
			limitProvided = hasLimit || hasPageSize || hasPageSizeSnake
		}
	}
	if req.Limit == 0 {
		req.Limit = req.PageSize
	}
	if req.Limit == 0 {
		req.Limit = req.PageSizeX
	}
	if !limitProvided && req.Limit == 0 {
		query := r.URL.Query()
		if rawLimit := firstNonEmpty(query.Get("limit"), query.Get("pageSize"), query.Get("page_size")); rawLimit != "" {
			req.Limit, _ = strconv.Atoi(rawLimit)
			limitProvided = true
		}
	}
	if req.Limit < 0 {
		req.Limit = 0
	}
	return req.Limit, limitProvided
}
