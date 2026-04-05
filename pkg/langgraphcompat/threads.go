package langgraphcompat

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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
	_ = s.persistSessionFile(session)
	_ = s.appendThreadHistorySnapshot(threadID)
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
	if err := s.persistSessionFile(session); err != nil {
		http.Error(w, "failed to persist thread", http.StatusInternalServerError)
		return
	}
	_ = s.appendThreadHistorySnapshot(threadID)

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
	s.deleteRunsForThread(threadID)
	_ = os.RemoveAll(filepath.Dir(s.threadRoot(threadID)))

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
	if err := s.persistSessionFile(session); err != nil {
		http.Error(w, "failed to persist thread state", http.StatusInternalServerError)
		return
	}
	_ = s.appendThreadHistorySnapshot(threadID)

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
	if err := s.persistSessionFile(session); err != nil {
		http.Error(w, "failed to persist thread state", http.StatusInternalServerError)
		return
	}
	_ = s.appendThreadHistorySnapshot(threadID)

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

	history := s.loadThreadHistory(threadID)
	if len(history) == 0 {
		history = []ThreadState{*state}
	}
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

func (s *Server) handleThreadScopedRunGet(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	runID := r.PathValue("run_id")
	run := s.getRun(runID)
	if run == nil || run.ThreadID != threadID {
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

func (s *Server) handleThreadRunsList(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	s.runsMu.RLock()
	runs := make([]map[string]any, 0)
	for _, run := range s.runs {
		if run.ThreadID != threadID {
			continue
		}
		runs = append(runs, map[string]any{
			"run_id":       run.RunID,
			"thread_id":    run.ThreadID,
			"assistant_id": run.AssistantID,
			"status":       run.Status,
			"created_at":   run.CreatedAt.Format(time.RFC3339Nano),
			"updated_at":   run.UpdatedAt.Format(time.RFC3339Nano),
		})
	}
	s.runsMu.RUnlock()
	sort.Slice(runs, func(i, j int) bool {
		return runs[i]["created_at"].(string) > runs[j]["created_at"].(string)
	})
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
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
	_ = s.persistSessionFile(session)
	return session
}

func (s *Server) markThreadStatus(threadID string, status string) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	if session, exists := s.sessions[threadID]; exists {
		session.Status = status
		session.UpdatedAt = time.Now().UTC()
		_ = s.persistSessionFile(session)
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
		_ = s.persistSessionFile(session)
	}
}

func (s *Server) saveRun(run *Run) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	copyRun := *run
	copyRun.Events = append([]StreamEvent(nil), run.Events...)
	s.runs[run.RunID] = &copyRun
	_ = s.persistRunFile(&copyRun)
}

func (s *Server) appendRunEvent(runID string, event StreamEvent) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	if run, exists := s.runs[runID]; exists {
		run.Events = append(run.Events, event)
		run.UpdatedAt = time.Now().UTC()
		_ = s.persistRunFile(run)
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

type persistedSession struct {
	ThreadID  string           `json:"thread_id"`
	Messages  []models.Message `json:"messages"`
	Metadata  map[string]any   `json:"metadata"`
	Status    string           `json:"status"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

type persistedRun struct {
	RunID       string        `json:"run_id"`
	ThreadID    string        `json:"thread_id"`
	AssistantID string        `json:"assistant_id"`
	Status      string        `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	Events      []StreamEvent `json:"events"`
	Error       string        `json:"error,omitempty"`
}

const maxThreadHistorySnapshots = 20

func (s *Server) threadStatePath(threadID string) string {
	return filepath.Join(s.threadRoot(threadID), "thread.json")
}

func (s *Server) runStatePath(runID string) string {
	return filepath.Join(s.dataRoot, "runs", runID+".json")
}

func (s *Server) threadHistoryPath(threadID string) string {
	return filepath.Join(s.threadRoot(threadID), "thread_history.json")
}

func (s *Server) persistSessionFile(session *Session) error {
	if session == nil {
		return nil
	}
	if err := os.MkdirAll(s.threadRoot(session.ThreadID), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(persistedSession{
		ThreadID:  session.ThreadID,
		Messages:  session.Messages,
		Metadata:  session.Metadata,
		Status:    session.Status,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.threadStatePath(session.ThreadID), data, 0o644)
}

func (s *Server) persistRunFile(run *Run) error {
	if run == nil {
		return nil
	}
	path := s.runStatePath(run.RunID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(persistedRun{
		RunID:       run.RunID,
		ThreadID:    run.ThreadID,
		AssistantID: run.AssistantID,
		Status:      run.Status,
		CreatedAt:   run.CreatedAt,
		UpdatedAt:   run.UpdatedAt,
		Events:      run.Events,
		Error:       run.Error,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (s *Server) loadPersistedThreads() {
	root := filepath.Join(s.dataRoot, "threads")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "user-data", "thread.json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var persisted persistedSession
		if err := json.Unmarshal(data, &persisted); err != nil {
			continue
		}
		if persisted.ThreadID == "" {
			persisted.ThreadID = entry.Name()
		}
		s.sessions[persisted.ThreadID] = &Session{
			ThreadID:     persisted.ThreadID,
			Messages:     persisted.Messages,
			Metadata:     persisted.Metadata,
			Status:       firstNonEmpty(persisted.Status, "idle"),
			PresentFiles: tools.NewPresentFileRegistry(),
			CreatedAt:    persisted.CreatedAt,
			UpdatedAt:    persisted.UpdatedAt,
		}
	}
}

func (s *Server) loadPersistedRuns() {
	root := filepath.Join(s.dataRoot, "runs")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		var persisted persistedRun
		if err := json.Unmarshal(data, &persisted); err != nil {
			continue
		}
		if persisted.RunID == "" {
			continue
		}
		s.runs[persisted.RunID] = &Run{
			RunID:       persisted.RunID,
			ThreadID:    persisted.ThreadID,
			AssistantID: persisted.AssistantID,
			Status:      persisted.Status,
			CreatedAt:   persisted.CreatedAt,
			UpdatedAt:   persisted.UpdatedAt,
			Events:      persisted.Events,
			Error:       persisted.Error,
		}
	}
}

func (s *Server) appendThreadHistorySnapshot(threadID string) error {
	state := s.getThreadState(threadID)
	if state == nil {
		return nil
	}
	history := s.loadThreadHistory(threadID)
	if len(history) > 0 && history[0].CreatedAt == state.CreatedAt {
		return nil
	}
	history = append([]ThreadState{*state}, history...)
	if len(history) > maxThreadHistorySnapshots {
		history = history[:maxThreadHistorySnapshots]
	}
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.threadHistoryPath(threadID), data, 0o644)
}

func (s *Server) loadThreadHistory(threadID string) []ThreadState {
	data, err := os.ReadFile(s.threadHistoryPath(threadID))
	if err != nil {
		return nil
	}
	var history []ThreadState
	if err := json.Unmarshal(data, &history); err != nil {
		return nil
	}
	return history
}

func (s *Server) deleteRunsForThread(threadID string) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	for runID, run := range s.runs {
		if run.ThreadID != threadID {
			continue
		}
		delete(s.runs, runID)
		_ = os.Remove(s.runStatePath(runID))
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
