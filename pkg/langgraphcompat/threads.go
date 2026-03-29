package langgraphcompat

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func (s *Server) handleThreadGet(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}

	s.sessionsMu.RLock()
	session, exists := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if !exists {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, s.threadResponse(session))
}

func (s *Server) handleThreadCreate(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if r.Body != nil {
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	threadID, _ := req["thread_id"].(string)
	if threadID == "" {
		threadID = uuid.New().String()
	}
	metadata, _ := req["metadata"].(map[string]any)

	session := s.ensureSession(threadID, metadata)
	writeJSON(w, http.StatusCreated, s.threadResponse(session))
}

func (s *Server) handleThreadUpdate(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}

	var req map[string]any
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	session, exists := s.sessions[threadID]
	if !exists {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}

	if metadata, ok := req["metadata"].(map[string]any); ok {
		for k, v := range metadata {
			session.Metadata[k] = v
		}
	}
	session.UpdatedAt = time.Now().UTC()

	writeJSON(w, http.StatusOK, s.threadResponse(session))
}

func (s *Server) handleThreadDelete(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}

	s.sessionsMu.Lock()
	delete(s.sessions, threadID)
	s.sessionsMu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleThreadSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Limit     int    `json:"limit"`
		Offset    int    `json:"offset"`
		SortBy    string `json:"sort_by"`
		SortOrder string `json:"sort_order"`
	}
	if r.Body != nil {
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}
	if req.SortBy == "" {
		req.SortBy = "updated_at"
	}
	if req.SortOrder == "" {
		req.SortOrder = "desc"
	}

	s.sessionsMu.RLock()
	threads := make([]map[string]any, 0, len(s.sessions))
	for _, session := range s.sessions {
		threads = append(threads, s.threadResponse(session))
	}
	s.sessionsMu.RUnlock()

	sort.Slice(threads, func(i, j int) bool {
		left := threads[i]
		right := threads[j]
		var less bool
		switch req.SortBy {
		case "created_at":
			less = left["created_at"].(string) < right["created_at"].(string)
		case "thread_id":
			less = left["thread_id"].(string) < right["thread_id"].(string)
		default:
			less = left["updated_at"].(string) < right["updated_at"].(string)
		}
		if strings.EqualFold(req.SortOrder, "asc") {
			return less
		}
		return !less
	})

	start := req.Offset
	if start > len(threads) {
		start = len(threads)
	}
	end := start + req.Limit
	if end > len(threads) {
		end = len(threads)
	}

	writeJSON(w, http.StatusOK, threads[start:end])
}

func (s *Server) handleThreadFiles(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}

	s.sessionsMu.RLock()
	session, exists := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if !exists {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}

	files := []tools.PresentFile{}
	if session.PresentFiles != nil {
		files = session.PresentFiles.List()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"files": files,
	})
}

func (s *Server) handleThreadStateGet(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
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
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}

	var req struct {
		Values map[string]any `json:"values"`
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	s.sessionsMu.Lock()
	session, exists := s.sessions[threadID]
	if !exists {
		s.sessionsMu.Unlock()
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}

	if title, ok := req.Values["title"].(string); ok {
		session.Metadata["title"] = title
	}
	session.UpdatedAt = time.Now().UTC()
	s.sessionsMu.Unlock()

	writeJSON(w, http.StatusOK, s.getThreadState(threadID))
}

func (s *Server) handleThreadStatePatch(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}

	var req struct {
		Metadata map[string]any `json:"metadata"`
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	s.sessionsMu.Lock()
	session, exists := s.sessions[threadID]
	if !exists {
		s.sessionsMu.Unlock()
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}
	for k, v := range req.Metadata {
		session.Metadata[k] = v
	}
	session.UpdatedAt = time.Now().UTC()
	s.sessionsMu.Unlock()

	writeJSON(w, http.StatusOK, s.getThreadState(threadID))
}

func (s *Server) handleThreadHistory(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}

	var req struct {
		Limit int `json:"limit"`
	}
	if r.Body != nil {
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	state := s.getThreadState(threadID)
	if state == nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}

	history := []ThreadState{*state}
	if req.Limit == 0 || req.Limit > len(history) {
		req.Limit = len(history)
	}
	writeJSON(w, http.StatusOK, history[:req.Limit])
}

func (s *Server) handleRunGet(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	run := s.getRun(runID)
	if run == nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"run_id":       run.RunID,
		"thread_id":    run.ThreadID,
		"assistant_id": run.AssistantID,
		"status":       run.Status,
		"created_at":   run.CreatedAt.Format(time.RFC3339Nano),
		"updated_at":   run.UpdatedAt.Format(time.RFC3339Nano),
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := s.healthStatus(r.Context())
	code := http.StatusOK
	if status.Status == "down" {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, status)
}

func (s *Server) handleThreadClarificationCreate(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}
	if s.getThreadState(threadID) == nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}
	s.clarifyAPI.HandleCreate(w, r, threadID)
}

func (s *Server) handleThreadClarificationGet(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}
	s.clarifyAPI.HandleGet(w, r, threadID, r.PathValue("id"))
}

func (s *Server) handleThreadClarificationResolve(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}
	s.clarifyAPI.HandleResolve(w, r, threadID, r.PathValue("id"))
}

func (s *Server) messagesToLangChain(messages []models.Message) []Message {
	result := make([]Message, 0, len(messages))
	for _, msg := range messages {
		msgType := "human"
		role := "human"

		switch msg.Role {
		case models.RoleAI:
			msgType = "ai"
			role = "assistant"
		case models.RoleSystem:
			msgType = "system"
			role = "system"
		case models.RoleTool:
			msgType = "tool"
			role = "tool"
		}

		result = append(result, Message{
			Type:    msgType,
			ID:      msg.ID,
			Role:    role,
			Content: msg.Content,
		})
	}
	return result
}

func (s *Server) threadResponse(session *Session) map[string]any {
	return map[string]any{
		"thread_id":  session.ThreadID,
		"created_at": session.CreatedAt.Format(time.RFC3339Nano),
		"updated_at": session.UpdatedAt.Format(time.RFC3339Nano),
		"metadata":   session.Metadata,
		"status":     session.Status,
		"config": map[string]any{
			"configurable": map[string]any{
				"agent_type": stringValue(session.Metadata["agent_type"]),
			},
		},
		"values": map[string]any{
			"title":     session.Metadata["title"],
			"artifacts": sessionArtifactPaths(session),
		},
	}
}

func (s *Server) getThreadState(threadID string) *ThreadState {
	s.sessionsMu.RLock()
	session, exists := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if !exists {
		return nil
	}

	values := map[string]any{
		"messages":  s.messagesToLangChain(session.Messages),
		"title":     stringValue(session.Metadata["title"]),
		"artifacts": sessionArtifactPaths(session),
	}

	return &ThreadState{
		CheckpointID: uuid.New().String(),
		Values:       values,
		Next:         []string{},
		Tasks:        []any{},
		Metadata: map[string]any{
			"thread_id": threadID,
			"step":      0,
		},
		CreatedAt: session.UpdatedAt.Format(time.RFC3339Nano),
	}
}

func (s *Server) ensureSession(threadID string, metadata map[string]any) *Session {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	if session, exists := s.sessions[threadID]; exists {
		if metadata != nil {
			for k, v := range metadata {
				session.Metadata[k] = v
			}
		}
		return session
	}

	if metadata == nil {
		metadata = make(map[string]any)
	}
	now := time.Now().UTC()
	session := &Session{
		ThreadID:     threadID,
		Messages:     []models.Message{},
		Metadata:     metadata,
		Status:       "idle",
		PresentFiles: tools.NewPresentFileRegistry(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.sessions[threadID] = session
	return session
}

func (s *Server) markThreadStatus(threadID string, status string) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	if session, exists := s.sessions[threadID]; exists {
		session.Status = status
		session.UpdatedAt = time.Now().UTC()
	}
}

func (s *Server) setThreadMetadata(threadID string, key string, value any) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	if session, exists := s.sessions[threadID]; exists {
		if session.Metadata == nil {
			session.Metadata = make(map[string]any)
		}
		session.Metadata[key] = value
		session.UpdatedAt = time.Now().UTC()
	}
}

func (s *Server) saveRun(run *Run) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	copyRun := *run
	copyRun.Events = append([]StreamEvent(nil), run.Events...)
	s.runs[run.RunID] = &copyRun
}

func (s *Server) appendRunEvent(runID string, event StreamEvent) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	if run, exists := s.runs[runID]; exists {
		run.Events = append(run.Events, event)
		run.UpdatedAt = time.Now().UTC()
	}
}

func (s *Server) nextRunEventIndex(runID string) int {
	s.runsMu.RLock()
	defer s.runsMu.RUnlock()
	if run, exists := s.runs[runID]; exists {
		return len(run.Events) + 1
	}
	return 1
}

func (s *Server) getRun(runID string) *Run {
	s.runsMu.RLock()
	defer s.runsMu.RUnlock()
	run, exists := s.runs[runID]
	if !exists {
		return nil
	}
	copyRun := *run
	copyRun.Events = append([]StreamEvent(nil), run.Events...)
	return &copyRun
}

func (s *Server) getLatestRunForThread(threadID string) *Run {
	s.runsMu.RLock()
	defer s.runsMu.RUnlock()

	var latest *Run
	for _, run := range s.runs {
		if run.ThreadID != threadID {
			continue
		}
		if latest == nil || run.CreatedAt.After(latest.CreatedAt) {
			copyRun := *run
			copyRun.Events = append([]StreamEvent(nil), run.Events...)
			latest = &copyRun
		}
	}
	return latest
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func sessionArtifactPaths(session *Session) []string {
	if session == nil || session.PresentFiles == nil {
		return []string{}
	}

	files := session.PresentFiles.List()
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	return paths
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
