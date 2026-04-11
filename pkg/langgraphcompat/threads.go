package langgraphcompat

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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

	threadID, _ := req["thread_id"].(string)
	if threadID == "" {
		threadID, _ = req["threadId"].(string)
	}
	if threadID == "" {
		threadID = uuid.New().String()
	}
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), threadIDStatusCode(r))
		return
	}
	metadata, _ := req["metadata"].(map[string]any)

	session := s.ensureSession(threadID, metadata)
	s.applyThreadValues(session, extractThreadValues(req))
	applyThreadConfigurable(session, req)
	applyThreadStatus(session, req)
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

	s.sessionsMu.Lock()
	session, exists := s.sessions[threadID]
	if !exists {
		s.sessionsMu.Unlock()
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}

	if metadata, ok := req["metadata"].(map[string]any); ok {
		applyThreadMetadata(session, metadata)
	}
	s.applyThreadValues(session, extractThreadValues(req))
	applyThreadConfigurable(session, req)
	applyThreadStatus(session, req)
	session.UpdatedAt = time.Now().UTC()
	s.sessionsMu.Unlock()
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
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), threadIDStatusCode(r))
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
	var raw map[string]any
	var req struct {
		Query      string         `json:"query"`
		Status     string         `json:"status"`
		Limit      int            `json:"limit"`
		PageSize   int            `json:"page_size"`
		PageSizeX  int            `json:"pageSize"`
		Offset     int            `json:"offset"`
		SortBy     string         `json:"sort_by"`
		SortByX    string         `json:"sortBy"`
		SortOrder  string         `json:"sort_order"`
		SortOrderX string         `json:"sortOrder"`
		Select     []string       `json:"select"`
		Metadata   map[string]any `json:"metadata"`
		Values     map[string]any `json:"values"`
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
	queryValues := r.URL.Query()
	if req.Query == "" {
		req.Query = strings.TrimSpace(queryValues.Get("query"))
	}

	if req.Limit == 0 {
		req.Limit = req.PageSize
	}
	if req.Limit == 0 {
		req.Limit = req.PageSizeX
	}
	if req.Limit == 0 {
		if rawLimit := firstNonEmpty(queryValues.Get("limit"), queryValues.Get("pageSize"), queryValues.Get("page_size")); rawLimit != "" {
			req.Limit, _ = strconv.Atoi(rawLimit)
			limitProvided = true
		}
	}
	if req.Limit < 0 {
		req.Limit = 0
	}
	if !limitProvided && req.Limit == 0 {
		req.Limit = 50
	}
	if req.Offset == 0 {
		if rawOffset := strings.TrimSpace(queryValues.Get("offset")); rawOffset != "" {
			req.Offset, _ = strconv.Atoi(rawOffset)
		}
	}
	if req.Offset < 0 {
		req.Offset = 0
	}
	req.SortBy = firstNonEmpty(req.SortBy, req.SortByX)
	req.SortOrder = firstNonEmpty(req.SortOrder, req.SortOrderX)
	req.SortBy = firstNonEmpty(req.SortBy, queryValues.Get("sortBy"), queryValues.Get("sort_by"))
	req.SortOrder = firstNonEmpty(req.SortOrder, queryValues.Get("sortOrder"), queryValues.Get("sort_order"))
	if len(req.Select) == 0 {
		if rawSelect := strings.TrimSpace(queryValues.Get("select")); rawSelect != "" {
			for _, item := range strings.Split(rawSelect, ",") {
				item = strings.TrimSpace(item)
				if item != "" {
					req.Select = append(req.Select, item)
				}
			}
		}
	}
	req.SortBy = normalizeThreadFieldName(req.SortBy)
	if req.SortBy == "" {
		req.SortBy = "updated_at"
	}
	if req.SortOrder == "" {
		req.SortOrder = "desc"
	}

	writeJSON(w, http.StatusOK, s.searchThreadResponses(threadSearchRequest{
		Query:     req.Query,
		Status:    req.Status,
		Limit:     req.Limit,
		Offset:    req.Offset,
		SortBy:    req.SortBy,
		SortOrder: req.SortOrder,
		Select:    req.Select,
		Metadata:  req.Metadata,
		Values:    req.Values,
	}))
}

func applyThreadStatus(session *Session, raw map[string]any) {
	if session == nil || len(raw) == 0 {
		return
	}
	status := strings.TrimSpace(stringFromAny(raw["status"]))
	if status == "" {
		return
	}
	session.Status = status
}

func applyThreadConfigurable(session *Session, raw map[string]any) {
	if session == nil || len(raw) == 0 {
		return
	}
	config := normalizeThreadConfig(mapFromAny(raw["config"]))
	if len(config) == 0 {
		if configurable := mapFromAny(raw["configurable"]); len(configurable) > 0 {
			config = normalizeThreadConfig(map[string]any{"configurable": configurable})
		}
	}
	configurable := mapFromAny(config["configurable"])
	if len(configurable) == 0 {
		return
	}
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	if session.Configurable == nil {
		session.Configurable = defaultThreadConfig(session.ThreadID)
	}
	for _, key := range []string{"thread_id", "agent_type", "agent_name", "model_name", "mode", "reasoning_effort", "thinking_enabled", "is_plan_mode", "subagent_enabled", "temperature", "max_tokens"} {
		if value, ok := configurable[key]; ok {
			session.Metadata[key] = value
			session.Configurable[key] = value
		}
	}
	if _, ok := session.Configurable["thread_id"]; !ok {
		session.Configurable["thread_id"] = session.ThreadID
	}
}

func normalizeUploadedFilesValue(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	case []any:
		return uploadedFilesFromMetadata(typed)
	default:
		return nil
	}
}

func (s *Server) applyThreadValues(session *Session, values map[string]any) {
	if session == nil || len(values) == 0 {
		return
	}
	if session.Values == nil {
		session.Values = map[string]any{}
	}
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	if hasAnyKey(values, "messages") {
		if rawMessages, ok := values["messages"].([]any); ok {
			session.Messages = s.convertToMessages(session.ThreadID, rawMessages)
		} else {
			session.Messages = nil
		}
	}
	if hasAnyKey(values, "title") {
		if title, ok := values["title"].(string); ok {
			title = strings.TrimSpace(title)
			session.Values["title"] = title
			if title == "" {
				delete(session.Metadata, "title")
			} else {
				session.Metadata["title"] = title
			}
		} else if values["title"] == nil {
			session.Values["title"] = ""
			delete(session.Metadata, "title")
		}
	}
	if hasAnyKey(values, "todos") {
		if todos, ok := normalizeTodos(values["todos"]); ok {
			session.Values["todos"] = todos
			if len(todos) > 0 {
				session.Metadata["todos"] = todos
			} else {
				delete(session.Metadata, "todos")
			}
		} else {
			session.Values["todos"] = []map[string]any{}
			delete(session.Metadata, "todos")
		}
	}
	if hasAnyKey(values, "sandbox") {
		if sandboxState, ok := normalizeStringMap(values["sandbox"]); ok {
			session.Values["sandbox"] = sandboxState
			if len(sandboxState) > 0 {
				session.Metadata["sandbox"] = sandboxState
			} else {
				delete(session.Metadata, "sandbox")
			}
		} else {
			session.Values["sandbox"] = map[string]any{}
			delete(session.Metadata, "sandbox")
		}
	}
	if hasAnyKey(values, "artifacts") {
		artifacts := anyStringSlice(values["artifacts"])
		session.Values["artifacts"] = artifacts
		if len(artifacts) > 0 {
			session.Metadata["artifacts"] = artifacts
		} else {
			delete(session.Metadata, "artifacts")
		}
	}
	if hasAnyKey(values, "viewed_images", "viewedImages") {
		if viewedImages, ok := normalizeViewedImages(firstNonNil(values["viewed_images"], values["viewedImages"])); ok {
			session.Values["viewed_images"] = viewedImages
			if len(viewedImages) > 0 {
				session.Metadata["viewed_images"] = viewedImages
			} else {
				delete(session.Metadata, "viewed_images")
			}
		} else {
			session.Values["viewed_images"] = map[string]any{}
			delete(session.Metadata, "viewed_images")
		}
	}
	if hasAnyKey(values, "uploaded_files", "uploadedFiles") {
		if uploadedFiles := normalizeUploadedFilesValue(firstNonNil(values["uploaded_files"], values["uploadedFiles"])); uploadedFiles != nil {
			session.Values["uploaded_files"] = uploadedFiles
			if len(uploadedFiles) > 0 {
				items := make([]any, 0, len(uploadedFiles))
				for _, file := range uploadedFiles {
					items = append(items, file)
				}
				session.Metadata["uploaded_files"] = items
			} else {
				delete(session.Metadata, "uploaded_files")
			}
		} else {
			session.Values["uploaded_files"] = []map[string]any{}
			delete(session.Metadata, "uploaded_files")
		}
	}
	if hasAnyKey(values, "thread_data", "threadData") {
		if threadData, ok := normalizeStringMap(firstNonNil(values["thread_data"], values["threadData"])); ok && len(threadData) > 0 {
			session.Metadata["thread_data"] = threadData
		} else {
			delete(session.Metadata, "thread_data")
		}
	}
	for key, value := range values {
		switch key {
		case "messages", "title", "todos", "sandbox", "artifacts", "viewed_images", "viewedImages", "uploaded_files", "uploadedFiles", "thread_data", "threadData":
			continue
		default:
			session.Values[key] = value
		}
	}
}

func hasAnyKey(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := values[key]; ok {
			return true
		}
	}
	return false
}

func applyThreadMetadata(session *Session, metadata map[string]any) {
	if session == nil || len(metadata) == 0 {
		return
	}
	normalized := make(map[string]any, len(metadata))
	for k, v := range metadata {
		normalized[k] = v
	}
	normalized = normalizePersistedThreadMetadata(normalized)
	for k, v := range normalized {
		session.Metadata[k] = v
	}
}

func extractThreadValues(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	values := map[string]any{}
	for canonicalKey, aliases := range map[string][]string{
		"messages":       {"messages"},
		"title":          {"title"},
		"todos":          {"todos"},
		"sandbox":        {"sandbox"},
		"artifacts":      {"artifacts"},
		"viewed_images":  {"viewed_images", "viewedImages"},
		"uploaded_files": {"uploaded_files", "uploadedFiles"},
		"thread_data":    {"thread_data", "threadData"},
	} {
		for _, alias := range aliases {
			if value, ok := raw[alias]; ok {
				values[canonicalKey] = value
				break
			}
		}
	}
	if nested, ok := raw["values"].(map[string]any); ok {
		for key, value := range nested {
			values[key] = value
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func (s *Server) handleThreadFiles(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), threadIDStatusCode(r))
		return
	}

	s.sessionsMu.RLock()
	session, exists := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if !exists {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}

	files := s.collectSessionFiles(session)

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

func (s *Server) handleThreadStatePatch(w http.ResponseWriter, r *http.Request) {
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

	history, code, detail := s.threadHistorySlice(threadID, req.Limit, limitProvided)
	if detail != "" {
		http.Error(w, detail, code)
		return
	}
	writeJSON(w, http.StatusOK, history)
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
			Content: rewriteAssistantArtifactLinks(msg.SessionID, msg.Content, msg.Role),
		}
		if msg.Role == models.RoleHuman {
			if multi := decodeMultiContent(msg.Metadata); len(multi) > 0 {
				converted.Content = multi
			}
		}
		if usageMetadata := usageMetadataFromMessageMetadata(msg.Metadata); len(usageMetadata) > 0 {
			converted.UsageMetadata = usageMetadata
		}
		if additionalKwargs := additionalKwargsFromMessageMetadata(msg.Metadata); len(additionalKwargs) > 0 {
			converted.AdditionalKwargs = additionalKwargs
		}
		if len(msg.ToolCalls) > 0 {
			converted.ToolCalls = convertToolCalls(msg.ToolCalls)
		}
		if msg.ToolResult != nil {
			converted.Name = msg.ToolResult.ToolName
			converted.ToolCallID = msg.ToolResult.CallID
			converted.Status = toolMessageStatus(msg)
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

func additionalKwargsFromMessageMetadata(metadata map[string]string) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	if decoded := decodeAdditionalKwargs(metadata); len(decoded) > 0 {
		return decoded
	}
	out := make(map[string]any)
	for key, value := range metadata {
		if strings.HasPrefix(key, "usage_") || key == "message_status" || key == "multi_content" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func usageMetadataFromMessageMetadata(metadata map[string]string) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		raw, ok := metadata["usage_"+key]
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
			out[key] = n
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toolMessageStatus(msg models.Message) string {
	if msg.Metadata != nil {
		if status := strings.TrimSpace(msg.Metadata["message_status"]); status != "" {
			return status
		}
	}
	if msg.ToolResult == nil {
		return ""
	}
	switch msg.ToolResult.Status {
	case models.CallStatusCompleted:
		return "success"
	case models.CallStatusFailed:
		return "error"
	case models.CallStatusRunning:
		return "running"
	case models.CallStatusPending:
		return "pending"
	default:
		return ""
	}
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

func (s *Server) getThreadState(threadID string) *ThreadState {
	s.sessionsMu.RLock()
	session, exists := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if !exists {
		return nil
	}

	values := s.threadValues(session)
	values["messages"] = s.messagesToLangChain(session.Messages)

	next := stringSliceFromAny(session.Metadata["next"])
	tasks := anySlice(session.Metadata["tasks"])
	interrupts := anySlice(session.Metadata["interrupts"])
	checkpoint := checkpointObjectFromMetadata(session.Metadata, "")
	parentCheckpoint := checkpointObjectFromMetadata(session.Metadata, "parent_")

	return &ThreadState{
		CheckpointID:       firstNonEmpty(stringValue(session.Metadata["checkpoint_id"]), session.CheckpointID),
		ParentCheckpointID: stringValue(session.Metadata["parent_checkpoint_id"]),
		Checkpoint:         checkpoint,
		ParentCheckpoint:   parentCheckpoint,
		Values:             values,
		Config: map[string]any{
			"configurable": s.threadConfigurable(session),
		},
		Next:       append([]string(nil), next...),
		Tasks:      append([]any(nil), tasks...),
		Interrupts: append([]any(nil), interrupts...),
		Metadata:   threadMetadata(session),
		CreatedAt:  firstNonZeroTime(session.CreatedAt, session.UpdatedAt).Format(time.RFC3339Nano),
	}
}

func (s *Server) threadValues(session *Session) map[string]any {
	values := copyMetadataMap(session.Values)
	if values == nil {
		values = map[string]any{}
	}
	agentName := strings.TrimSpace(stringValue(session.Metadata["agent_name"]))
	if agentName == "" {
		agentName = strings.TrimSpace(stringValue(values["created_agent_name"]))
	}
	threadKind := "chat"
	routePath := "/workspace/chats/" + session.ThreadID
	if agentName != "" {
		threadKind = "agent"
		routePath = "/workspace/agents/" + agentName + "/chats/" + session.ThreadID
	}
	for key, value := range map[string]any{
		"title":          stringValue(session.Metadata["title"]),
		"artifacts":      s.sessionArtifactPaths(session),
		"uploaded_files": s.messageUploadedFilesState(session),
		"viewed_images":  viewedImagesFromMetadata(session.Metadata["viewed_images"]),
		"agent_name":     agentName,
		"thread_kind":    threadKind,
		"route_path":     routePath,
	} {
		if _, exists := values[key]; !exists {
			values[key] = value
		}
	}
	if _, exists := values["todos"]; !exists {
		todos := todosFromMetadata(session.Metadata["todos"])
		if len(session.Todos) > 0 {
			todos = todosToAny(session.Todos)
		}
		values["todos"] = todos
	}
	if _, exists := values["sandbox"]; !exists {
		sandboxState := mapFromMetadata(session.Metadata["sandbox"])
		if len(sandboxState) == 0 {
			sandboxState = map[string]any{"sandbox_id": "local"}
		}
		values["sandbox"] = sandboxState
	}
	values["thread_data"] = s.restoredThreadDataState(session)
	values["messages"] = s.messagesToLangChain(session.Messages)
	return values
}

func (s *Server) threadConfigurable(session *Session) map[string]any {
	configurable := defaultThreadConfig(session.ThreadID)
	for key, value := range copyMetadataMap(session.Configurable) {
		configurable[key] = value
	}
	mode := strings.TrimSpace(stringValue(configurable["mode"]))
	if mode == "" {
		mode = deriveThreadMode(session.Metadata)
	}
	thinkingEnabled := mode != "flash"
	isPlanMode := mode == "pro" || mode == "ultra"
	subagentEnabled := mode == "ultra"
	for key, value := range map[string]any{
		"thread_id":        session.ThreadID,
		"agent_type":       stringValue(session.Metadata["agent_type"]),
		"agent_name":       stringValue(session.Metadata["agent_name"]),
		"model_name":       stringValue(session.Metadata["model_name"]),
		"mode":             mode,
		"reasoning_effort": deriveReasoningEffort(session.Metadata, mode),
		"is_plan_mode":     isPlanMode,
		"thinking_enabled": thinkingEnabled,
		"subagent_enabled": subagentEnabled,
	} {
		if _, exists := configurable[key]; !exists || configurable[key] == "" {
			configurable[key] = value
		}
	}
	if value, ok := float64FromAny(session.Metadata["temperature"]); ok {
		if _, exists := configurable["temperature"]; !exists {
			configurable["temperature"] = value
		}
	}
	if value := toInt64(session.Metadata["max_tokens"]); value > 0 {
		if _, exists := configurable["max_tokens"]; !exists {
			configurable["max_tokens"] = value
		}
	}
	if value, ok := session.Metadata["thinking_enabled"].(bool); ok {
		if session.Configurable == nil {
			configurable["thinking_enabled"] = value
		} else if _, exists := session.Configurable["thinking_enabled"]; !exists {
			configurable["thinking_enabled"] = value
		}
	}
	if value, ok := session.Metadata["is_plan_mode"].(bool); ok {
		if session.Configurable == nil {
			configurable["is_plan_mode"] = value
		} else if _, exists := session.Configurable["is_plan_mode"]; !exists {
			configurable["is_plan_mode"] = value
		}
	}
	if value, ok := session.Metadata["subagent_enabled"].(bool); ok {
		if session.Configurable == nil {
			configurable["subagent_enabled"] = value
		} else if _, exists := session.Configurable["subagent_enabled"]; !exists {
			configurable["subagent_enabled"] = value
		}
	}
	return configurable
}

func threadMetadata(session *Session) map[string]any {
	metadata := map[string]any{
		"thread_id": session.ThreadID,
	}
	for key, value := range session.Metadata {
		metadata[key] = value
	}
	if _, ok := metadata["step"]; !ok {
		metadata["step"] = 0
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

func (s *Server) restoredThreadDataState(session *Session) map[string]any {
	if session == nil {
		return map[string]any{}
	}
	current := s.threadDataState(session.ThreadID)
	if threadDataExists(current) {
		return current
	}
	if restored := mapFromMetadata(session.Metadata["thread_data"]); hasThreadWorkspace(restored) {
		return restored
	}
	return current
}

func hasThreadWorkspace(data map[string]any) bool {
	if len(data) == 0 {
		return false
	}
	for _, key := range []string{"workspace_path", "uploads_path", "outputs_path"} {
		if strings.TrimSpace(stringFromAny(data[key])) != "" {
			return true
		}
	}
	return false
}

func threadDataExists(data map[string]any) bool {
	if len(data) == 0 {
		return false
	}
	for _, key := range []string{"workspace_path", "uploads_path", "outputs_path"} {
		path := strings.TrimSpace(stringFromAny(data[key]))
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func checkpointObjectFromMetadata(metadata map[string]any, prefix string) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := map[string]any{}
	if value := strings.TrimSpace(stringValue(metadata[prefix+"checkpoint_id"])); value != "" {
		out["checkpoint_id"] = value
	}
	if value := strings.TrimSpace(stringValue(metadata[prefix+"checkpoint_ns"])); value != "" {
		out["checkpoint_ns"] = value
	}
	if value := strings.TrimSpace(stringValue(metadata[prefix+"checkpoint_thread_id"])); value != "" {
		out["thread_id"] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstNonZeroTime(times ...time.Time) time.Time {
	for _, ts := range times {
		if !ts.IsZero() {
			return ts
		}
	}
	return time.Time{}
}

func float64FromAny(v any) (float64, bool) {
	switch value := v.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func deriveThreadMode(metadata map[string]any) string {
	if mode := strings.TrimSpace(stringValue(metadata["mode"])); mode != "" {
		return mode
	}
	subagentEnabled, _ := metadata["subagent_enabled"].(bool)
	isPlanMode, _ := metadata["is_plan_mode"].(bool)
	thinkingEnabled, thinkingSet := metadata["thinking_enabled"].(bool)
	if subagentEnabled {
		return "ultra"
	}
	if isPlanMode {
		return "pro"
	}
	if thinkingSet && thinkingEnabled {
		return "thinking"
	}
	return "flash"
}

func deriveReasoningEffort(metadata map[string]any, mode string) string {
	if effort := strings.TrimSpace(stringValue(metadata["reasoning_effort"])); effort != "" {
		return effort
	}
	switch mode {
	case "ultra":
		return "high"
	case "pro":
		return "medium"
	case "thinking":
		return "low"
	default:
		return "minimal"
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
	companionFiles := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.EqualFold(filepath.Ext(name), ".md") {
			base := strings.TrimSuffix(name, filepath.Ext(name))
			for _, ext := range []string{".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx", ".csv", ".tsv", ".json", ".txt", ".log", ".ini", ".cfg", ".conf", ".env", ".toml", ".xml", ".html", ".htm", ".xhtml", ".yaml", ".yml"} {
				if _, err := os.Stat(filepath.Join(s.uploadsDir(threadID), base+ext)); err == nil {
					companionFiles[name] = struct{}{}
					break
				}
			}
		}
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := companionFiles[entry.Name()]; ok {
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

func (s *Server) messageUploadedFilesState(session *Session) []map[string]any {
	if session == nil {
		return []map[string]any{}
	}
	files := s.uploadedFilesState(session.ThreadID)
	if len(files) > 0 {
		return files
	}
	if restored := uploadedFilesFromMetadata(session.Metadata["uploaded_files"]); len(restored) > 0 {
		return restored
	}
	return []map[string]any{}
}

func uploadedFilesFromMetadata(raw any) []map[string]any {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		file, ok := item.(map[string]any)
		if !ok {
			continue
		}
		filename := strings.TrimSpace(stringFromAny(file["filename"]))
		if filename == "" {
			continue
		}
		normalized := map[string]any{
			"filename": filename,
		}
		if path := strings.TrimSpace(stringFromAny(firstNonNil(file["path"], file["virtual_path"]))); path != "" {
			normalized["path"] = path
		}
		if size := toInt64(file["size"]); size > 0 {
			normalized["size"] = size
		}
		if status := strings.TrimSpace(stringFromAny(file["status"])); status != "" {
			normalized["status"] = status
		}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func todosFromMetadata(raw any) []map[string]any {
	if todos, ok := normalizeTodos(raw); ok {
		return todos
	}
	return []map[string]any{}
}

func normalizeTodos(raw any) ([]map[string]any, bool) {
	var items []any
	switch typed := raw.(type) {
	case []any:
		items = typed
	case []map[string]any:
		items = make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
	default:
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
			applyThreadMetadata(session, metadata)
		}
		if session.Values == nil {
			session.Values = map[string]any{}
		}
		if session.Configurable == nil {
			session.Configurable = defaultThreadConfig(threadID)
		}
		return session
	}

	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata = normalizePersistedThreadMetadata(metadata)
	if _, exists := metadata["thread_id"]; !exists {
		metadata["thread_id"] = threadID
	}
	checkpointID := firstNonEmpty(stringValue(metadata["checkpoint_id"]), stringValue(metadata["checkpointId"]))
	if checkpointID == "" && len(metadata) > 1 {
		checkpointID = uuid.New().String()
	}
	if checkpointID != "" {
		metadata["checkpoint_id"] = checkpointID
		if _, exists := metadata["checkpoint_thread_id"]; !exists {
			metadata["checkpoint_thread_id"] = threadID
		}
	}
	now := time.Now().UTC()
	session := &Session{
		CheckpointID: checkpointID,
		ThreadID:     threadID,
		Messages:     []models.Message{},
		Values:       map[string]any{},
		Metadata:     metadata,
		Configurable: defaultThreadConfig(threadID),
		Status:       "idle",
		PresentFiles: tools.NewPresentFileRegistry(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.sessions[threadID] = session
	_ = s.persistSessionFile(session)
	return session
}

func threadStateRequestProvidesCheckpoint(req map[string]any) bool {
	metadata, _ := req["metadata"].(map[string]any)
	if len(metadata) == 0 {
		return false
	}
	for _, key := range []string{
		"checkpoint_id",
		"checkpointId",
		"parent_checkpoint_id",
		"parentCheckpointId",
		"checkpoint_ns",
		"checkpointNs",
		"parent_checkpoint_ns",
		"parentCheckpointNs",
		"checkpoint_thread_id",
		"checkpointThreadId",
		"parent_checkpoint_thread_id",
		"parentCheckpointThreadId",
		"checkpoint",
		"checkpointObject",
		"parent_checkpoint",
		"parentCheckpoint",
	} {
		if _, ok := metadata[key]; ok {
			return true
		}
	}
	return false
}

func clearSessionCheckpoint(session *Session) {
	if session == nil {
		return
	}
	session.CheckpointID = ""
	if session.Metadata == nil {
		return
	}
	delete(session.Metadata, "checkpoint_id")
	delete(session.Metadata, "checkpoint_ns")
	delete(session.Metadata, "checkpoint_thread_id")
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

func (s *Server) deleteThreadMetadata(threadID string, key string) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	if session, exists := s.sessions[threadID]; exists {
		if session.Metadata != nil {
			delete(session.Metadata, key)
		}
		session.UpdatedAt = time.Now().UTC()
		_ = s.persistSessionFile(session)
	}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func (s *Server) sessionArtifactPaths(session *Session) []string {
	files := s.collectArtifactFiles(session)
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	return paths
}

func sessionArtifactPaths(session *Session) []string {
	if session == nil {
		return nil
	}
	seen := make(map[string]struct{})
	paths := make([]string, 0)
	if session.PresentFiles != nil {
		for _, file := range session.PresentFiles.List() {
			addArtifactPath(&paths, seen, file.Path)
		}
	}
	for _, message := range session.Messages {
		for _, path := range messageArtifactPaths(message) {
			addArtifactPath(&paths, seen, path)
		}
	}
	for _, path := range anyStringSlice(session.Metadata["artifacts"]) {
		addArtifactPath(&paths, seen, path)
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

var artifactMarkdownLinkPattern = regexp.MustCompile(`(?m)(\[[^\]]*?\])\((/mnt/user-data/(?:uploads|outputs|workspace)/[^)\n]*?\.[A-Za-z0-9]+)\)`)
var artifactBarePathPattern = regexp.MustCompile(`(?m)(^|[\s>:"'` + "`" + `])(/mnt/user-data/(?:uploads|outputs|workspace)/[^)\]\n]*?\.[A-Za-z0-9]+)`)

func rewriteAssistantArtifactLinks(threadID string, content any, role models.Role) any {
	if role != models.RoleAI {
		return content
	}
	text, ok := content.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return content
	}
	return rewriteArtifactLinksInText(threadID, text)
}

func rewriteArtifactLinksInText(threadID, text string) string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || strings.TrimSpace(text) == "" {
		return text
	}
	rewritten := artifactMarkdownLinkPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := artifactMarkdownLinkPattern.FindStringSubmatch(match)
		if len(parts) != 3 || strings.HasPrefix(parts[1], "![") {
			return match
		}
		return parts[1] + "(" + artifactURLForThread(threadID, parts[2]) + ")"
	})
	return artifactBarePathPattern.ReplaceAllStringFunc(rewritten, func(match string) string {
		parts := artifactBarePathPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		return parts[1] + artifactURLForThread(threadID, parts[2])
	})
}

func artifactURLForThread(threadID, virtualPath string) string {
	virtualPath = strings.TrimSpace(virtualPath)
	if virtualPath == "" {
		return virtualPath
	}
	trimmed := strings.TrimPrefix(virtualPath, "/")
	segments := strings.Split(trimmed, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return "/api/threads/" + strings.TrimSpace(threadID) + "/artifacts/" + strings.Join(segments, "/")
}

func messageArtifactPaths(message models.Message) []string {
	paths := make([]string, 0)
	for _, call := range message.ToolCalls {
		if call.Status != models.CallStatusCompleted {
			continue
		}
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
		if message.ToolResult.Status != models.CallStatusCompleted {
			return paths
		}
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

func (s *Server) collectSessionFiles(session *Session) []tools.PresentFile {
	if session == nil {
		return nil
	}
	seen := make(map[string]struct{})
	files := make([]tools.PresentFile, 0)
	add := func(file tools.PresentFile) {
		file = s.normalizePresentFile(session.ThreadID, file)
		if file.ID == "" || file.Path == "" || !presentFileExists(file) {
			return
		}
		if _, ok := seen[file.Path]; ok {
			return
		}
		seen[file.Path] = struct{}{}
		files = append(files, file)
	}
	if session.PresentFiles != nil {
		for _, file := range session.PresentFiles.List() {
			add(file)
		}
	}
	for _, root := range []struct {
		dir          string
		virtual      string
		markdownOnly bool
	}{
		{dir: s.uploadsDir(session.ThreadID), virtual: "/mnt/user-data/uploads", markdownOnly: false},
		{dir: s.workspaceDir(session.ThreadID), virtual: "/mnt/user-data/workspace", markdownOnly: false},
		{dir: s.outputsDir(session.ThreadID), virtual: "/mnt/user-data/outputs", markdownOnly: false},
	} {
		for _, file := range collectPresentFiles(root.dir, root.virtual, root.markdownOnly) {
			add(file)
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].CreatedAt.Equal(files[j].CreatedAt) {
			return files[i].Path < files[j].Path
		}
		return files[i].CreatedAt.After(files[j].CreatedAt)
	})
	return files
}

func (s *Server) collectArtifactFiles(session *Session) []tools.PresentFile {
	if session == nil {
		return nil
	}
	files := make([]tools.PresentFile, 0)
	seen := make(map[string]struct{})
	addExisting := func(file tools.PresentFile) {
		file = s.normalizePresentFile(session.ThreadID, file)
		if file.ID == "" || file.Path == "" || !presentFileExists(file) {
			return
		}
		if _, ok := seen[file.Path]; ok {
			return
		}
		seen[file.Path] = struct{}{}
		files = append(files, file)
	}
	addKnownPath := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		files = append(files, tools.PresentFile{
			ID:          autodiscoveredPresentFileID(path),
			Path:        path,
			VirtualPath: path,
			ArtifactURL: artifactURLForThread(session.ThreadID, path),
			Extension:   strings.ToLower(filepath.Ext(path)),
		})
	}
	if session.PresentFiles != nil {
		for _, file := range session.PresentFiles.List() {
			addExisting(file)
		}
	}
	for _, message := range session.Messages {
		for _, path := range messageArtifactPaths(message) {
			addKnownPath(path)
		}
	}
	for _, path := range anyStringSlice(session.Metadata["artifacts"]) {
		addKnownPath(path)
	}
	autoFiles := make([]tools.PresentFile, 0)
	for _, file := range collectPresentFiles(s.outputsDir(session.ThreadID), "/mnt/user-data/outputs", false) {
		file = s.normalizePresentFile(session.ThreadID, file)
		if file.ID == "" || file.Path == "" || !presentFileExists(file) {
			continue
		}
		if _, ok := seen[file.Path]; ok {
			continue
		}
		autoFiles = append(autoFiles, file)
	}
	for _, file := range collectPresentFiles(s.uploadsDir(session.ThreadID), "/mnt/user-data/uploads", true) {
		file = s.normalizePresentFile(session.ThreadID, file)
		if file.ID == "" || file.Path == "" || !presentFileExists(file) {
			continue
		}
		if _, ok := seen[file.Path]; ok {
			continue
		}
		autoFiles = append(autoFiles, file)
	}
	sort.Slice(autoFiles, func(i, j int) bool {
		if autoFiles[i].CreatedAt.Equal(autoFiles[j].CreatedAt) {
			return autoFiles[i].Path < autoFiles[j].Path
		}
		return autoFiles[i].CreatedAt.After(autoFiles[j].CreatedAt)
	})
	files = append(files, autoFiles...)
	return files
}

func collectPresentFiles(root, virtualPrefix string, markdownOnly bool) []tools.PresentFile {
	entries := collectArtifactFiles(root, virtualPrefix)
	files := make([]tools.PresentFile, 0, len(entries))
	for _, file := range entries {
		if markdownOnly && !strings.EqualFold(filepath.Ext(file.Path), ".md") {
			continue
		}
		files = append(files, file)
	}
	return files
}

func (s *Server) normalizePresentFile(threadID string, file tools.PresentFile) tools.PresentFile {
	file.Path = strings.TrimSpace(file.Path)
	if file.Path == "" {
		return tools.PresentFile{}
	}
	if strings.TrimSpace(file.SourcePath) == "" {
		file.SourcePath = s.resolveArtifactPath(threadID, file.Path)
	}
	if file.ID == "" {
		file.ID = autodiscoveredPresentFileID(file.Path)
	}
	if info, err := os.Stat(file.SourcePath); err == nil {
		if file.CreatedAt.IsZero() {
			file.CreatedAt = info.ModTime().UTC()
		}
		if file.Size == 0 {
			file.Size = info.Size()
		}
	}
	if file.MimeType == "" {
		file.MimeType = detectArtifactMimeType(file.SourcePath)
	}
	file.VirtualPath = file.Path
	file.ArtifactURL = artifactURLForThread(threadID, file.Path)
	file.Extension = strings.ToLower(filepath.Ext(file.Path))
	return file
}

func presentFileExists(file tools.PresentFile) bool {
	path := strings.TrimSpace(file.SourcePath)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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

func anySlice(v any) []any {
	switch items := v.(type) {
	case []any:
		return items
	case []string:
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, item)
		}
		return out
	default:
		if v != nil {
			return []any{v}
		}
		return nil
	}
}

type persistedThreadSession struct {
	ThreadID  string           `json:"thread_id"`
	Messages  []models.Message `json:"messages"`
	Metadata  map[string]any   `json:"metadata"`
	Status    string           `json:"status"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

type persistedThreadRun struct {
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

func derivePersistedThreadStatus(raw map[string]any, fallback string) string {
	if status := strings.TrimSpace(fallback); status != "" {
		return status
	}
	if items := anySlice(raw["interrupts"]); len(items) > 0 {
		return "interrupted"
	}
	if items := anySlice(raw["tasks"]); len(items) > 0 {
		return "busy"
	}
	if items := anySlice(raw["next"]); len(items) > 0 {
		return "busy"
	}
	return "idle"
}

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
	data, err := json.MarshalIndent(persistedThreadSession{
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
	data, err := json.MarshalIndent(persistedThreadRun{
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
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal(data, &wrapper); err == nil {
			if nested, ok := wrapper["thread"]; ok && len(nested) > 0 {
				data = nested
			} else if nested, ok := wrapper["data"]; ok && len(nested) > 0 {
				data = nested
			}
		}
		var persisted persistedThreadSession
		if err := json.Unmarshal(data, &persisted); err != nil {
			continue
		}
		var raw map[string]any
		_ = json.Unmarshal(data, &raw)
		if persisted.Metadata == nil {
			persisted.Metadata = mapFromAny(raw["metadata"])
		}
		values := normalizeThreadValues(mapFromAny(raw["values"]))
		if len(values) == 0 {
			values = normalizeThreadValues(raw)
		}
		if len(persisted.Messages) == 0 {
			if rawMessages, ok := values["messages"].([]any); ok {
				persisted.Messages = s.convertToMessages(entry.Name(), rawMessages)
			}
		}
		if persisted.Metadata == nil {
			persisted.Metadata = map[string]any{}
		}
		for metadataKey, valueKeys := range map[string][]string{
			"title":          {"title"},
			"todos":          {"todos"},
			"sandbox":        {"sandbox"},
			"viewed_images":  {"viewed_images", "viewedImages"},
			"artifacts":      {"artifacts"},
			"uploaded_files": {"uploaded_files", "uploadedFiles"},
			"thread_data":    {"thread_data", "threadData"},
		} {
			for _, valueKey := range valueKeys {
				if value, ok := values[valueKey]; ok {
					persisted.Metadata[metadataKey] = value
					break
				}
			}
		}
		config := normalizeThreadConfig(mapFromAny(raw["config"]))
		if len(config) == 0 {
			if configurable := mapFromAny(raw["configurable"]); len(configurable) > 0 {
				config = normalizeThreadConfig(map[string]any{"configurable": configurable})
			}
		}
		if len(config) == 0 {
			config = normalizeThreadConfig(raw)
		}
		if configurable := mapFromAny(config["configurable"]); len(configurable) > 0 {
			if persisted.Metadata == nil {
				persisted.Metadata = map[string]any{}
			}
			for _, key := range []string{"thread_id", "agent_type", "agent_name", "model_name", "mode", "reasoning_effort", "thinking_enabled", "is_plan_mode", "subagent_enabled", "temperature", "max_tokens"} {
				if value, ok := configurable[key]; ok {
					persisted.Metadata[key] = value
				}
			}
		}
		for key, aliases := range map[string][]string{
			"checkpoint_id":        {"checkpoint_id", "checkpointId"},
			"parent_checkpoint_id": {"parent_checkpoint_id", "parentCheckpointId"},
		} {
			for _, alias := range aliases {
				if value, ok := raw[alias]; ok {
					persisted.Metadata[key] = value
					break
				}
			}
		}
		for _, key := range []string{"interrupts", "tasks", "next"} {
			if value, ok := raw[key]; ok {
				persisted.Metadata[key] = value
			}
		}
		if checkpoint := normalizeCheckpointObject(mapFromAny(firstNonNil(raw["checkpoint"], raw["checkpointObject"]))); len(checkpoint) > 0 {
			if value, ok := checkpoint["checkpoint_id"]; ok {
				persisted.Metadata["checkpoint_id"] = value
			}
			if value, ok := checkpoint["checkpoint_ns"]; ok {
				persisted.Metadata["checkpoint_ns"] = value
			}
			if value, ok := checkpoint["thread_id"]; ok {
				persisted.Metadata["checkpoint_thread_id"] = value
			}
		}
		if checkpoint := normalizeCheckpointObject(mapFromAny(firstNonNil(raw["parent_checkpoint"], raw["parentCheckpoint"]))); len(checkpoint) > 0 {
			if value, ok := checkpoint["checkpoint_id"]; ok {
				persisted.Metadata["parent_checkpoint_id"] = value
			}
			if value, ok := checkpoint["checkpoint_ns"]; ok {
				persisted.Metadata["parent_checkpoint_ns"] = value
			}
			if value, ok := checkpoint["thread_id"]; ok {
				persisted.Metadata["parent_checkpoint_thread_id"] = value
			}
		}
		if persisted.ThreadID == "" {
			persisted.ThreadID = firstNonEmpty(stringValue(raw["threadId"]), stringValue(raw["thread_id"]))
		}
		if persisted.CreatedAt.IsZero() {
			persisted.CreatedAt = timeValue(firstNonNil(raw["createdAt"], raw["created_at"]))
		}
		if persisted.UpdatedAt.IsZero() {
			persisted.UpdatedAt = timeValue(firstNonNil(raw["updatedAt"], raw["updated_at"]))
		}
		if persisted.UpdatedAt.IsZero() {
			persisted.UpdatedAt = persisted.CreatedAt
		}
		if persisted.ThreadID == "" {
			persisted.ThreadID = entry.Name()
		}
		normalizedTopLevelMetadata := normalizePersistedThreadMetadata(mapFromAny(raw))
		persisted.Metadata = normalizePersistedThreadMetadata(persisted.Metadata)
		for _, key := range []string{
			"thread_id",
			"assistant_id",
			"graph_id",
			"run_id",
			"checkpoint_id",
			"parent_checkpoint_id",
			"checkpoint_ns",
			"parent_checkpoint_ns",
			"checkpoint_thread_id",
			"parent_checkpoint_thread_id",
			"step",
			"mode",
			"model_name",
			"reasoning_effort",
			"agent_name",
			"agent_type",
			"thinking_enabled",
			"is_plan_mode",
			"subagent_enabled",
			"temperature",
			"max_tokens",
		} {
			if _, ok := persisted.Metadata[key]; !ok {
				if value, ok := normalizedTopLevelMetadata[key]; ok {
					persisted.Metadata[key] = value
				}
			}
		}
		s.sessions[persisted.ThreadID] = &Session{
			ThreadID:     persisted.ThreadID,
			Messages:     persisted.Messages,
			Metadata:     persisted.Metadata,
			Status:       derivePersistedThreadStatus(raw, persisted.Status),
			PresentFiles: tools.NewPresentFileRegistry(),
			CreatedAt:    persisted.CreatedAt,
			UpdatedAt:    persisted.UpdatedAt,
		}
	}
}

func normalizePersistedThreadMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return metadata
	}
	if _, ok := metadata["viewed_images"]; !ok {
		if value, ok := metadata["viewedImages"]; ok {
			metadata["viewed_images"] = value
		}
	}
	if _, ok := metadata["model_name"]; !ok {
		if value, ok := metadata["modelName"]; ok {
			metadata["model_name"] = value
		} else if value, ok := metadata["model"]; ok {
			metadata["model_name"] = value
		}
	}
	if _, ok := metadata["thread_id"]; !ok {
		if value, ok := metadata["threadId"]; ok {
			metadata["thread_id"] = value
		}
	}
	if _, ok := metadata["assistant_id"]; !ok {
		if value, ok := metadata["assistantId"]; ok {
			metadata["assistant_id"] = value
		}
	}
	if _, ok := metadata["graph_id"]; !ok {
		if value, ok := metadata["graphId"]; ok {
			metadata["graph_id"] = value
		}
	}
	if _, ok := metadata["run_id"]; !ok {
		if value, ok := metadata["runId"]; ok {
			metadata["run_id"] = value
		}
	}
	if _, ok := metadata["checkpoint_id"]; !ok {
		if value, ok := metadata["checkpointId"]; ok {
			metadata["checkpoint_id"] = value
		}
	}
	if _, ok := metadata["parent_checkpoint_id"]; !ok {
		if value, ok := metadata["parentCheckpointId"]; ok {
			metadata["parent_checkpoint_id"] = value
		}
	}
	if _, ok := metadata["checkpoint_ns"]; !ok {
		if value, ok := metadata["checkpointNs"]; ok {
			metadata["checkpoint_ns"] = value
		}
	}
	if _, ok := metadata["parent_checkpoint_ns"]; !ok {
		if value, ok := metadata["parentCheckpointNs"]; ok {
			metadata["parent_checkpoint_ns"] = value
		}
	}
	if _, ok := metadata["checkpoint_thread_id"]; !ok {
		if value, ok := metadata["checkpointThreadId"]; ok {
			metadata["checkpoint_thread_id"] = value
		}
	}
	if _, ok := metadata["parent_checkpoint_thread_id"]; !ok {
		if value, ok := metadata["parentCheckpointThreadId"]; ok {
			metadata["parent_checkpoint_thread_id"] = value
		}
	}
	if _, ok := metadata["agent_type"]; !ok {
		if value, ok := metadata["agentType"]; ok {
			metadata["agent_type"] = value
		}
	}
	if _, ok := metadata["reasoning_effort"]; !ok {
		if value, ok := metadata["reasoningEffort"]; ok {
			metadata["reasoning_effort"] = value
		}
	}
	if _, ok := metadata["agent_name"]; !ok {
		if value, ok := metadata["agentName"]; ok {
			metadata["agent_name"] = value
		}
	}
	if _, ok := metadata["thinking_enabled"]; !ok {
		if value, ok := metadata["thinkingEnabled"]; ok {
			metadata["thinking_enabled"] = value
		}
	}
	if _, ok := metadata["is_plan_mode"]; !ok {
		if value, ok := metadata["isPlanMode"]; ok {
			metadata["is_plan_mode"] = value
		}
	}
	if _, ok := metadata["subagent_enabled"]; !ok {
		if value, ok := metadata["subagentEnabled"]; ok {
			metadata["subagent_enabled"] = value
		}
	}
	if _, ok := metadata["temperature"]; !ok {
		if value, ok := metadata["Temperature"]; ok {
			metadata["temperature"] = value
		}
	}
	if _, ok := metadata["max_tokens"]; !ok {
		if value, ok := metadata["maxTokens"]; ok {
			metadata["max_tokens"] = value
		}
	}
	if checkpoint := normalizeCheckpointObject(mapFromAny(firstNonNil(metadata["checkpoint"], metadata["checkpointObject"]))); len(checkpoint) > 0 {
		if value, ok := checkpoint["checkpoint_id"]; ok {
			metadata["checkpoint_id"] = value
		}
		if value, ok := checkpoint["checkpoint_ns"]; ok {
			metadata["checkpoint_ns"] = value
		}
		if value, ok := checkpoint["thread_id"]; ok {
			metadata["checkpoint_thread_id"] = value
		}
	}
	if checkpoint := normalizeCheckpointObject(mapFromAny(firstNonNil(metadata["parent_checkpoint"], metadata["parentCheckpoint"]))); len(checkpoint) > 0 {
		if value, ok := checkpoint["checkpoint_id"]; ok {
			metadata["parent_checkpoint_id"] = value
		}
		if value, ok := checkpoint["checkpoint_ns"]; ok {
			metadata["parent_checkpoint_ns"] = value
		}
		if value, ok := checkpoint["thread_id"]; ok {
			metadata["parent_checkpoint_thread_id"] = value
		}
	}
	return metadata
}

func normalizePersistedRunEvents(events []StreamEvent, rawItems []any) []StreamEvent {
	if len(events) == 0 || len(events) != len(rawItems) {
		return events
	}
	for i, rawItem := range rawItems {
		raw := mapFromAny(rawItem)
		if raw == nil {
			continue
		}
		if events[i].ID == "" {
			events[i].ID = stringValue(firstNonNil(raw["id"], raw["ID"]))
		}
		if events[i].Event == "" {
			events[i].Event = stringValue(firstNonNil(raw["event"], raw["Event"]))
		}
		if events[i].Data == nil {
			events[i].Data = firstNonNil(raw["data"], raw["Data"])
		}
		if events[i].RunID == "" {
			events[i].RunID = stringValue(firstNonNil(raw["runId"], raw["run_id"], raw["RunID"]))
		}
		if events[i].ThreadID == "" {
			events[i].ThreadID = stringValue(firstNonNil(raw["threadId"], raw["thread_id"], raw["ThreadID"]))
		}
	}
	return events
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
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal(data, &wrapper); err == nil {
			if nested, ok := wrapper["run"]; ok && len(nested) > 0 {
				data = nested
			} else if nested, ok := wrapper["data"]; ok && len(nested) > 0 {
				data = nested
			}
		}
		var persisted persistedThreadRun
		if err := json.Unmarshal(data, &persisted); err != nil {
			continue
		}
		var raw map[string]any
		_ = json.Unmarshal(data, &raw)
		if persisted.RunID == "" {
			persisted.RunID = stringValue(raw["runId"])
		}
		if persisted.ThreadID == "" {
			persisted.ThreadID = stringValue(raw["threadId"])
		}
		if persisted.AssistantID == "" {
			persisted.AssistantID = stringValue(raw["assistantId"])
		}
		if persisted.CreatedAt.IsZero() {
			persisted.CreatedAt = timeValue(firstNonNil(raw["createdAt"], raw["created_at"]))
		}
		if persisted.UpdatedAt.IsZero() {
			persisted.UpdatedAt = timeValue(firstNonNil(raw["updatedAt"], raw["updated_at"]))
		}
		if rawEvents, ok := raw["events"].([]any); ok {
			persisted.Events = normalizePersistedRunEvents(persisted.Events, rawEvents)
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

func timeValue(value any) time.Time {
	switch typed := value.(type) {
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(typed)); err == nil {
			return parsed
		}
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func (s *Server) appendThreadHistorySnapshot(threadID string) error {
	state := s.getThreadState(threadID)
	if state == nil {
		return nil
	}
	s.sessionsMu.RLock()
	session := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if session != nil {
		state.CreatedAt = firstNonZeroTime(session.UpdatedAt, session.CreatedAt).Format(time.RFC3339Nano)
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
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil {
		if nested, ok := wrapper["history"]; ok && len(nested) > 0 {
			data = nested
		} else if nested, ok := wrapper["items"]; ok && len(nested) > 0 {
			data = nested
		} else if nested, ok := wrapper["data"]; ok && len(nested) > 0 {
			data = nested
		}
	}
	var history []ThreadState
	if err := json.Unmarshal(data, &history); err == nil {
		var rawItems []map[string]any
		if err := json.Unmarshal(data, &rawItems); err == nil {
			return normalizeLoadedThreadHistory(history, rawItems)
		}
		return history
	}
	var rawItems []map[string]any
	if err := json.Unmarshal(data, &rawItems); err == nil {
		history = make([]ThreadState, len(rawItems))
		return normalizeLoadedThreadHistory(history, rawItems)
	}
	return nil
}

func normalizeCheckpointObject(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	out := map[string]any{}
	if value := firstNonEmpty(stringValue(raw["checkpoint_id"]), stringValue(raw["checkpointId"])); value != "" {
		out["checkpoint_id"] = value
	}
	if value := firstNonEmpty(stringValue(raw["checkpoint_ns"]), stringValue(raw["checkpointNs"])); value != "" {
		out["checkpoint_ns"] = value
	}
	if value := firstNonEmpty(stringValue(raw["thread_id"]), stringValue(raw["threadId"])); value != "" {
		out["thread_id"] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeThreadValues(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		out[key] = value
	}
	for canonicalKey, aliases := range map[string][]string{
		"title":          {"title"},
		"todos":          {"todos"},
		"sandbox":        {"sandbox"},
		"viewed_images":  {"viewed_images", "viewedImages"},
		"artifacts":      {"artifacts"},
		"uploaded_files": {"uploaded_files", "uploadedFiles"},
		"thread_data":    {"thread_data", "threadData"},
		"messages":       {"messages"},
	} {
		for _, alias := range aliases {
			if value, ok := raw[alias]; ok {
				out[canonicalKey] = value
				break
			}
		}
	}
	return out
}

func normalizeThreadConfig(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		out[key] = value
	}
	configurable := mapFromAny(out["configurable"])
	if len(configurable) == 0 {
		configurable = raw
	}
	if len(configurable) == 0 {
		return out
	}
	normalized := make(map[string]any, len(configurable))
	for key, value := range configurable {
		normalized[key] = value
	}
	for canonicalKey, aliases := range map[string][]string{
		"thread_id":        {"thread_id", "threadId"},
		"agent_type":       {"agent_type", "agentType"},
		"agent_name":       {"agent_name", "agentName"},
		"model_name":       {"model_name", "modelName", "model"},
		"reasoning_effort": {"reasoning_effort", "reasoningEffort"},
		"thinking_enabled": {"thinking_enabled", "thinkingEnabled"},
		"is_plan_mode":     {"is_plan_mode", "isPlanMode"},
		"subagent_enabled": {"subagent_enabled", "subagentEnabled"},
		"temperature":      {"temperature", "Temperature"},
		"max_tokens":       {"max_tokens", "maxTokens"},
	} {
		for _, alias := range aliases {
			if value, ok := configurable[alias]; ok {
				normalized[canonicalKey] = value
				break
			}
		}
	}
	out["configurable"] = normalized
	return out
}

func normalizeLoadedThreadHistory(history []ThreadState, rawItems []map[string]any) []ThreadState {
	if len(rawItems) == 0 {
		return history
	}
	if len(history) != len(rawItems) {
		history = make([]ThreadState, len(rawItems))
	}
	for i := range rawItems {
		if history[i].CheckpointID == "" {
			history[i].CheckpointID = firstNonEmpty(stringValue(rawItems[i]["checkpointId"]), stringValue(rawItems[i]["checkpoint_id"]))
		}
		if history[i].ParentCheckpointID == "" {
			history[i].ParentCheckpointID = firstNonEmpty(stringValue(rawItems[i]["parentCheckpointId"]), stringValue(rawItems[i]["parent_checkpoint_id"]))
		}
		if history[i].CreatedAt == "" {
			history[i].CreatedAt = firstNonEmpty(stringValue(rawItems[i]["createdAt"]), stringValue(rawItems[i]["created_at"]))
		}
		if len(history[i].Next) == 0 {
			history[i].Next = stringSliceFromAny(rawItems[i]["next"])
		}
		if len(history[i].Tasks) == 0 {
			history[i].Tasks = anySlice(rawItems[i]["tasks"])
		}
		if len(history[i].Interrupts) == 0 {
			history[i].Interrupts = anySlice(rawItems[i]["interrupts"])
		}
		if len(history[i].Values) == 0 {
			history[i].Values = normalizeThreadValues(mapFromAny(rawItems[i]["values"]))
			if len(history[i].Values) == 0 {
				history[i].Values = normalizeThreadValues(rawItems[i])
			}
		} else {
			history[i].Values = normalizeThreadValues(history[i].Values)
		}
		if len(history[i].Config) == 0 {
			history[i].Config = mapFromAny(rawItems[i]["config"])
		}
		if len(history[i].Config) == 0 {
			if configurable := mapFromAny(rawItems[i]["configurable"]); len(configurable) > 0 {
				history[i].Config = map[string]any{"configurable": configurable}
			}
		}
		if len(history[i].Config) == 0 {
			history[i].Config = normalizeThreadConfig(rawItems[i])
		}
		history[i].Config = normalizeThreadConfig(history[i].Config)
		if len(history[i].Metadata) == 0 {
			history[i].Metadata = mapFromAny(rawItems[i]["metadata"])
		}
		if history[i].Metadata == nil {
			history[i].Metadata = map[string]any{}
		}
		history[i].Metadata = normalizePersistedThreadMetadata(history[i].Metadata)
		normalizedTopLevelMetadata := normalizePersistedThreadMetadata(mapFromAny(rawItems[i]))
		for _, key := range []string{
			"thread_id",
			"assistant_id",
			"graph_id",
			"run_id",
			"checkpoint_id",
			"parent_checkpoint_id",
			"checkpoint_ns",
			"parent_checkpoint_ns",
			"checkpoint_thread_id",
			"parent_checkpoint_thread_id",
			"step",
			"mode",
			"model_name",
			"reasoning_effort",
			"agent_name",
			"agent_type",
			"thinking_enabled",
			"is_plan_mode",
			"subagent_enabled",
			"temperature",
			"max_tokens",
		} {
			if _, ok := history[i].Metadata[key]; !ok {
				if value, ok := normalizedTopLevelMetadata[key]; ok {
					history[i].Metadata[key] = value
				}
			}
		}
		if len(history[i].Checkpoint) == 0 {
			history[i].Checkpoint = checkpointObjectFromMetadata(history[i].Metadata, "")
		}
		if len(history[i].ParentCheckpoint) == 0 {
			history[i].ParentCheckpoint = checkpointObjectFromMetadata(history[i].Metadata, "parent_")
		}
		if checkpoint := mapFromAny(firstNonNil(rawItems[i]["checkpoint"], rawItems[i]["checkpointObject"])); len(checkpoint) > 0 {
			history[i].Checkpoint = normalizeCheckpointObject(checkpoint)
			if history[i].CheckpointID == "" {
				history[i].CheckpointID = stringValue(history[i].Checkpoint["checkpoint_id"])
			}
		}
		if checkpoint := mapFromAny(firstNonNil(rawItems[i]["parent_checkpoint"], rawItems[i]["parentCheckpoint"])); len(checkpoint) > 0 {
			history[i].ParentCheckpoint = normalizeCheckpointObject(checkpoint)
			if history[i].ParentCheckpointID == "" {
				history[i].ParentCheckpointID = stringValue(history[i].ParentCheckpoint["checkpoint_id"])
			}
		}
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
