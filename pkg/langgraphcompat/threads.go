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
		threadID, _ = req["threadId"].(string)
	}
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
		Limit      int      `json:"limit"`
		Offset     int      `json:"offset"`
		SortBy     string   `json:"sort_by"`
		SortByX    string   `json:"sortBy"`
		SortOrder  string   `json:"sort_order"`
		SortOrderX string   `json:"sortOrder"`
		Select     []string `json:"select"`
	}
	if r.Body != nil {
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}
	req.SortBy = firstNonEmpty(req.SortBy, req.SortByX)
	req.SortOrder = firstNonEmpty(req.SortOrder, req.SortOrderX)
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

	selected := make([]map[string]any, 0, end-start)
	for _, thread := range threads[start:end] {
		selected = append(selected, selectThreadFields(thread, req.Select))
	}
	writeJSON(w, http.StatusOK, selected)
}

func selectThreadFields(thread map[string]any, selectFields []string) map[string]any {
	if len(selectFields) == 0 {
		return thread
	}
	selected := make(map[string]any, len(selectFields))
	for _, field := range selectFields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if value, ok := thread[field]; ok {
			selected[field] = value
		}
	}
	if _, ok := selected["thread_id"]; !ok {
		if value, exists := thread["thread_id"]; exists {
			selected["thread_id"] = value
		}
	}
	return selected
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
	if todos, ok := normalizeTodos(req.Values["todos"]); ok {
		session.Metadata["todos"] = todos
	}
	if sandboxState, ok := normalizeStringMap(req.Values["sandbox"]); ok {
		session.Metadata["sandbox"] = sandboxState
	}
	if viewedImages, ok := normalizeViewedImages(req.Values["viewed_images"]); ok {
		session.Metadata["viewed_images"] = viewedImages
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
		Values   map[string]any `json:"values"`
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
	if todos, ok := normalizeTodos(req.Values["todos"]); ok {
		session.Metadata["todos"] = todos
	}
	if sandboxState, ok := normalizeStringMap(req.Values["sandbox"]); ok {
		session.Metadata["sandbox"] = sandboxState
	}
	if viewedImages, ok := normalizeViewedImages(req.Values["viewed_images"]); ok {
		session.Metadata["viewed_images"] = viewedImages
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

func (s *Server) handleThreadClarificationsList(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}
	if s.getThreadState(threadID) == nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}
	s.clarifyAPI.HandleList(w, r, threadID)
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

		converted := Message{
			Type:    msgType,
			ID:      msg.ID,
			Role:    role,
			Content: msg.Content,
		}
		if additionalKwargs := stringMetadataToAny(msg.Metadata); len(additionalKwargs) > 0 {
			converted.AdditionalKwargs = additionalKwargs
		}
		if len(msg.ToolCalls) > 0 {
			converted.ToolCalls = convertToolCalls(msg.ToolCalls)
		}
		if msg.ToolResult != nil {
			converted.Name = msg.ToolResult.ToolName
			converted.ToolCallID = msg.ToolResult.CallID
			converted.Data = map[string]any{
				"status":   string(msg.ToolResult.Status),
				"error":    msg.ToolResult.Error,
				"duration": msg.ToolResult.Duration.String(),
			}
			if len(msg.ToolResult.Data) > 0 {
				converted.Data["data"] = msg.ToolResult.Data
			}
			if converted.Content == "" {
				converted.Content = firstNonEmpty(msg.ToolResult.Content, msg.ToolResult.Error)
			}
		}
		result = append(result, converted)
	}
	return result
}

func convertToolCalls(calls []models.ToolCall) []ToolCall {
	out := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		name := call.Name
		args := call.Arguments
		if normalizedArgs, ok := normalizePresentFileCall(call); ok {
			name = "present_files"
			args = normalizedArgs
		}
		out = append(out, ToolCall{
			ID:   call.ID,
			Name: name,
			Args: args,
		})
	}
	return out
}

func normalizePresentFileCall(call models.ToolCall) (map[string]any, bool) {
	if call.Name != "present_file" {
		return nil, false
	}
	path, _ := call.Arguments["path"].(string)
	if strings.TrimSpace(path) == "" {
		return nil, false
	}
	return map[string]any{
		"filepaths": []string{path},
	}, true
}

func stringMetadataToAny(metadata map[string]string) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = value
	}
	return out
}

func (s *Server) threadResponse(session *Session) map[string]any {
	values := s.threadValues(session)
	return map[string]any{
		"thread_id":  session.ThreadID,
		"created_at": session.CreatedAt.Format(time.RFC3339Nano),
		"updated_at": session.UpdatedAt.Format(time.RFC3339Nano),
		"metadata":   session.Metadata,
		"status":     session.Status,
		"config": map[string]any{
			"configurable": s.threadConfigurable(session),
		},
		"values": values,
	}
}

func (s *Server) getThreadState(threadID string) *ThreadState {
	s.sessionsMu.RLock()
	session, exists := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if !exists {
		return nil
	}

	values := s.threadValues(session)
	values["messages"] = s.messagesToLangChain(session.Messages)

	return &ThreadState{
		CheckpointID: uuid.New().String(),
		Values:       values,
		Config: map[string]any{
			"configurable": s.threadConfigurable(session),
		},
		Next:      []string{},
		Tasks:     []any{},
		Metadata:  threadMetadata(session),
		CreatedAt: session.UpdatedAt.Format(time.RFC3339Nano),
	}
}

func (s *Server) threadValues(session *Session) map[string]any {
	values := map[string]any{
		"title":          stringValue(session.Metadata["title"]),
		"artifacts":      sessionArtifactPaths(session),
		"todos":          todosFromMetadata(session.Metadata["todos"]),
		"sandbox":        mapFromMetadata(session.Metadata["sandbox"]),
		"thread_data":    s.threadDataState(session.ThreadID),
		"uploaded_files": s.uploadedFilesState(session.ThreadID),
		"viewed_images":  viewedImagesFromMetadata(session.Metadata["viewed_images"]),
	}
	return values
}

func (s *Server) threadConfigurable(session *Session) map[string]any {
	configurable := map[string]any{
		"thread_id":        session.ThreadID,
		"agent_type":       stringValue(session.Metadata["agent_type"]),
		"model_name":       stringValue(session.Metadata["model_name"]),
		"is_plan_mode":     false,
		"thinking_enabled": true,
		"subagent_enabled": false,
	}
	if reasoningEffort := stringValue(session.Metadata["reasoning_effort"]); reasoningEffort != "" {
		configurable["reasoning_effort"] = reasoningEffort
	}
	if value, ok := session.Metadata["thinking_enabled"].(bool); ok {
		configurable["thinking_enabled"] = value
	}
	if value, ok := session.Metadata["is_plan_mode"].(bool); ok {
		configurable["is_plan_mode"] = value
	}
	if value, ok := session.Metadata["subagent_enabled"].(bool); ok {
		configurable["subagent_enabled"] = value
	}
	return configurable
}

func threadMetadata(session *Session) map[string]any {
	metadata := map[string]any{
		"thread_id": session.ThreadID,
		"step":      0,
	}
	for key, value := range session.Metadata {
		metadata[key] = value
	}
	return metadata
}

func (s *Server) threadDataState(threadID string) map[string]any {
	return map[string]any{
		"workspace_path": s.workspaceDir(threadID),
		"uploads_path":   s.uploadsDir(threadID),
		"outputs_path":   s.outputsDir(threadID),
	}
}

func (s *Server) workspaceDir(threadID string) string {
	return filepath.Join(s.threadRoot(threadID), "workspace")
}

func (s *Server) outputsDir(threadID string) string {
	return filepath.Join(s.threadRoot(threadID), "outputs")
}

func (s *Server) uploadedFilesState(threadID string) []map[string]any {
	entries, err := os.ReadDir(s.uploadsDir(threadID))
	if err != nil {
		return []map[string]any{}
	}
	files := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, s.uploadInfo(threadID, filepath.Join(s.uploadsDir(threadID), entry.Name()), entry.Name(), info.Size(), info.ModTime().Unix()))
	}
	sort.Slice(files, func(i, j int) bool {
		li := toInt64(files[i]["modified"])
		lj := toInt64(files[j]["modified"])
		if li == lj {
			return asString(files[i]["filename"]) < asString(files[j]["filename"])
		}
		return li > lj
	})
	return files
}

func todosFromMetadata(raw any) []map[string]any {
	if todos, ok := normalizeTodos(raw); ok {
		return todos
	}
	return []map[string]any{}
}

func normalizeTodos(raw any) ([]map[string]any, bool) {
	items, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	todos := make([]map[string]any, 0, len(items))
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		todo := map[string]any{}
		if content := strings.TrimSpace(stringValue(itemMap["content"])); content != "" {
			todo["content"] = content
		}
		status := strings.TrimSpace(stringValue(itemMap["status"]))
		switch status {
		case "pending", "in_progress", "completed":
			todo["status"] = status
		case "":
		default:
			todo["status"] = "pending"
		}
		if len(todo) > 0 {
			todos = append(todos, todo)
		}
	}
	return todos, true
}

func mapFromMetadata(raw any) map[string]any {
	if values, ok := normalizeStringMap(raw); ok {
		return values
	}
	return map[string]any{}
}

func normalizeStringMap(raw any) (map[string]any, bool) {
	items, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	values := make(map[string]any, len(items))
	for key, value := range items {
		text := strings.TrimSpace(stringValue(value))
		if text == "" {
			continue
		}
		values[key] = text
	}
	return values, true
}

func viewedImagesFromMetadata(raw any) map[string]any {
	if values, ok := normalizeViewedImages(raw); ok {
		return values
	}
	return map[string]any{}
}

func normalizeViewedImages(raw any) (map[string]any, bool) {
	items, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	values := make(map[string]any, len(items))
	for path, item := range items {
		image, ok := item.(map[string]any)
		if !ok {
			continue
		}
		normalized := make(map[string]any, 2)
		if base64 := strings.TrimSpace(stringValue(image["base64"])); base64 != "" {
			normalized["base64"] = base64
		}
		if mimeType := strings.TrimSpace(stringValue(image["mime_type"])); mimeType != "" {
			normalized["mime_type"] = mimeType
		}
		values[path] = normalized
	}
	return values, true
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
	if _, exists := metadata["thread_id"]; !exists {
		metadata["thread_id"] = threadID
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
	if session == nil {
		return []string{}
	}

	seen := make(map[string]struct{})
	paths := make([]string, 0)
	if session.PresentFiles != nil {
		for _, file := range session.PresentFiles.List() {
			if addArtifactPath(&paths, seen, file.Path) {
				continue
			}
		}
	}
	for _, message := range session.Messages {
		for _, path := range messageArtifactPaths(message) {
			addArtifactPath(&paths, seen, path)
		}
	}
	return paths
}

func addArtifactPath(paths *[]string, seen map[string]struct{}, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if _, exists := seen[path]; exists {
		return true
	}
	seen[path] = struct{}{}
	*paths = append(*paths, path)
	return true
}

func messageArtifactPaths(message models.Message) []string {
	paths := make([]string, 0)
	for _, call := range message.ToolCalls {
		switch call.Name {
		case "present_file":
			if path, _ := call.Arguments["path"].(string); strings.TrimSpace(path) != "" {
				paths = append(paths, path)
			}
		case "present_files":
			paths = append(paths, anyStringSlice(call.Arguments["filepaths"])...)
		}
	}
	if message.ToolResult != nil {
		switch message.ToolResult.ToolName {
		case "present_file":
			if path, _ := message.ToolResult.Data["path"].(string); strings.TrimSpace(path) != "" {
				paths = append(paths, path)
			}
		case "present_files":
			paths = append(paths, anyStringSlice(message.ToolResult.Data["filepaths"])...)
		}
	}
	return paths
}

func anyStringSlice(v any) []string {
	switch items := v.(type) {
	case []string:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if strings.TrimSpace(item) != "" {
				out = append(out, strings.TrimSpace(item))
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if text := strings.TrimSpace(stringFromAny(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
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
