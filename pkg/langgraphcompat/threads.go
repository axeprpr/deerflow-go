package langgraphcompat

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

type threadSearchRequest struct {
	Limit     *int           `json:"limit"`
	Offset    *int           `json:"offset"`
	SortBy    string         `json:"-"`
	SortOrder string         `json:"-"`
	Query     string         `json:"query"`
	Status    string         `json:"status"`
	Metadata  map[string]any `json:"metadata"`
	Values    map[string]any `json:"values"`
	Select    []string       `json:"select"`
}

func (s *Server) handleThreadsList(w http.ResponseWriter, r *http.Request) {
	req := parseThreadSearchRequest(r)
	req.Select = preferThreadSearchSelect(req.Select, []string{"thread_id", "created_at", "updated_at", "metadata", "status", "config", "values"})
	s.writeThreadSearchResponse(w, req)
}

func (s *Server) handleThreadGet(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	} else if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	metadata, _ := req["metadata"].(map[string]any)

	session := s.ensureSession(threadID, metadata)
	writeJSON(w, http.StatusCreated, s.threadResponse(session))
}

func (s *Server) handleThreadUpdate(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		for k, v := range metadata {
			session.Metadata[k] = v
		}
	}
	session.UpdatedAt = time.Now().UTC()
	snapshot := cloneSession(session)
	s.sessionsMu.Unlock()
	_ = s.persistSessionSnapshot(snapshot)

	writeJSON(w, http.StatusOK, s.threadResponse(session))
}

func (s *Server) handleThreadDelete(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.deleteThreadResources(threadID, true); err != nil {
		http.Error(w, "failed to delete thread", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleThreadSearch(w http.ResponseWriter, r *http.Request) {
	req := parseThreadSearchRequest(r)
	s.writeThreadSearchResponse(w, req)
}

func (s *Server) writeThreadSearchResponse(w http.ResponseWriter, req threadSearchRequest) {
	limit := 50
	if req.Limit != nil {
		limit = *req.Limit
		if limit < 0 {
			limit = 0
		}
	}
	offset := 0
	if req.Offset != nil {
		offset = *req.Offset
		if offset < 0 {
			offset = 0
		}
	}
	sortBy := normalizeThreadSortBy(req.SortBy)
	sortOrder := normalizeThreadSortOrder(req.SortOrder)

	s.sessionsMu.RLock()
	threads := make([]map[string]any, 0, len(s.sessions))
	for _, session := range s.sessions {
		thread := s.threadResponse(session)
		if !threadMatchesSearch(session, thread, req) {
			continue
		}
		threads = append(threads, thread)
	}
	s.sessionsMu.RUnlock()

	sort.Slice(threads, func(i, j int) bool {
		left := threads[i]
		right := threads[j]
		cmp := compareThreadForSort(left, right, sortBy)
		if sortOrder == "asc" {
			return cmp < 0
		}
		return cmp > 0
	})

	start := offset
	if start > len(threads) {
		start = len(threads)
	}
	end := start + limit
	if end > len(threads) {
		end = len(threads)
	}
	if limit == 0 {
		end = start
	}

	selected := make([]map[string]any, 0, end-start)
	for _, thread := range threads[start:end] {
		selected = append(selected, selectThreadFields(thread, req.Select))
	}
	writeJSON(w, http.StatusOK, selected)
}

func parseThreadSearchRequest(r *http.Request) threadSearchRequest {
	req := threadSearchRequest{}
	if r == nil {
		return req
	}

	req.Limit = intPointerFromString(r.URL.Query().Get("limit"))
	req.Offset = intPointerFromString(r.URL.Query().Get("offset"))
	req.SortBy = firstNonEmpty(r.URL.Query().Get("sort_by"), r.URL.Query().Get("sortBy"))
	req.SortOrder = firstNonEmpty(r.URL.Query().Get("sort_order"), r.URL.Query().Get("sortOrder"))
	req.Query = r.URL.Query().Get("query")
	req.Status = r.URL.Query().Get("status")
	req.Select = csvStringSlice(r.URL.Query()["select"])

	if r.Body == nil {
		return req
	}
	defer r.Body.Close()

	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return req
	}

	req.Limit = intPointerFromAny(raw["limit"])
	req.Offset = intPointerFromAny(raw["offset"])
	req.SortBy = firstNonEmpty(stringFromAny(raw["sort_by"]), stringFromAny(raw["sortBy"]))
	req.SortOrder = firstNonEmpty(stringFromAny(raw["sort_order"]), stringFromAny(raw["sortOrder"]))
	req.Query = stringFromAny(raw["query"])
	req.Status = stringFromAny(raw["status"])
	req.Metadata, _ = raw["metadata"].(map[string]any)
	req.Values, _ = raw["values"].(map[string]any)
	req.Select = stringSliceFromAny(raw["select"])
	return req
}

func preferThreadSearchSelect(current []string, fallback []string) []string {
	if len(current) > 0 {
		return current
	}
	return append([]string(nil), fallback...)
}

func intPointerFromString(raw string) *int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &value
}

func csvStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func normalizeThreadSortBy(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "created_at", "createdat":
		return "created_at"
	case "thread_id", "threadid":
		return "thread_id"
	default:
		return "updated_at"
	}
}

func normalizeThreadSortOrder(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "asc") {
		return "asc"
	}
	return "desc"
}

func compareThreadForSort(left, right map[string]any, sortBy string) int {
	for _, field := range threadSortFields(sortBy) {
		cmp := compareThreadSortValue(left[field], right[field])
		if cmp != 0 {
			return cmp
		}
	}
	return 0
}

func threadSortFields(sortBy string) []string {
	switch sortBy {
	case "created_at":
		return []string{"created_at", "updated_at", "thread_id"}
	case "thread_id":
		return []string{"thread_id", "updated_at", "created_at"}
	default:
		return []string{"updated_at", "created_at", "thread_id"}
	}
}

func compareThreadSortValue(left, right any) int {
	leftValue := stringFromAnyValue(left)
	rightValue := stringFromAnyValue(right)
	switch {
	case leftValue < rightValue:
		return -1
	case leftValue > rightValue:
		return 1
	default:
		return 0
	}
}

func stringFromAnyValue(v any) string {
	switch value := v.(type) {
	case string:
		return value
	default:
		return ""
	}
}

func threadMatchesSearch(session *Session, thread map[string]any, req threadSearchRequest) bool {
	if len(req.Metadata) > 0 {
		threadMetadata, _ := thread["metadata"].(map[string]any)
		if !mapContainsSubset(threadMetadata, req.Metadata) {
			return false
		}
	}
	if len(req.Values) > 0 {
		threadValues, _ := thread["values"].(map[string]any)
		if !mapContainsSubset(threadValues, req.Values) {
			return false
		}
	}
	if req.Status != "" && !strings.EqualFold(stringFromAnyValue(thread["status"]), req.Status) {
		return false
	}
	query := strings.ToLower(strings.TrimSpace(req.Query))
	if query == "" {
		return true
	}
	if threadValues, _ := thread["values"].(map[string]any); anyContainsQuery(threadValues, query) {
		return true
	}
	title := ""
	if values, _ := thread["values"].(map[string]any); values != nil {
		title = strings.ToLower(stringFromAnyValue(values["title"]))
	}
	threadID := strings.ToLower(stringFromAnyValue(thread["thread_id"]))
	if strings.Contains(threadID, query) || strings.Contains(title, query) {
		return true
	}
	if sessionContainsQuery(session, query) {
		return true
	}
	return false
}

func sessionContainsQuery(session *Session, query string) bool {
	if session == nil || query == "" {
		return false
	}
	if anyContainsQuery(session.Metadata, query) || anyContainsQuery(session.Configurable, query) || anyContainsQuery(session.Todos, query) {
		return true
	}
	for _, msg := range session.Messages {
		if messageContainsQuery(msg, query) {
			return true
		}
	}
	return false
}

func messageContainsQuery(msg models.Message, query string) bool {
	if containsQueryString(msg.Content, query) {
		return true
	}
	if anyContainsQuery(msg.Metadata, query) {
		return true
	}
	for _, call := range msg.ToolCalls {
		if containsQueryString(call.Name, query) || anyContainsQuery(call.Arguments, query) {
			return true
		}
	}
	if msg.ToolResult == nil {
		return false
	}
	return containsQueryString(msg.ToolResult.ToolName, query) ||
		containsQueryString(msg.ToolResult.Content, query) ||
		containsQueryString(msg.ToolResult.Error, query) ||
		anyContainsQuery(msg.ToolResult.Data, query)
}

func anyContainsQuery(value any, query string) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return containsQueryString(v, query)
	case []string:
		for _, item := range v {
			if containsQueryString(item, query) {
				return true
			}
		}
	case map[string]string:
		for key, item := range v {
			if containsQueryString(key, query) || containsQueryString(item, query) {
				return true
			}
		}
	case map[string]any:
		for key, item := range v {
			if containsQueryString(key, query) || anyContainsQuery(item, query) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if anyContainsQuery(item, query) {
				return true
			}
		}
	case []Todo:
		for _, item := range v {
			if containsQueryString(item.Content, query) || containsQueryString(item.Status, query) {
				return true
			}
		}
	}
	return false
}

func containsQueryString(value string, query string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(value)), query)
}

func mapContainsSubset(target map[string]any, subset map[string]any) bool {
	if len(subset) == 0 {
		return true
	}
	if len(target) == 0 {
		return false
	}
	for key, want := range subset {
		got, exists := target[key]
		if !exists {
			return false
		}
		wantMap, wantIsMap := want.(map[string]any)
		if wantIsMap {
			gotMap, _ := got.(map[string]any)
			if !mapContainsSubset(gotMap, wantMap) {
				return false
			}
			continue
		}
		if !valuesEqual(got, want) {
			return false
		}
	}
	return true
}

func valuesEqual(left, right any) bool {
	switch l := left.(type) {
	case string:
		return l == stringFromAnyValue(right)
	case bool:
		r, ok := right.(bool)
		return ok && l == r
	case float64:
		r, ok := right.(float64)
		return ok && l == r
	case int:
		r, ok := right.(int)
		return ok && l == r
	case nil:
		return right == nil
	default:
		return left == right
	}
}

func selectThreadFields(thread map[string]any, fields []string) map[string]any {
	if len(fields) == 0 {
		return thread
	}

	selected := make(map[string]any, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if value, ok := thread[field]; ok {
			selected[field] = value
		}
	}
	if _, ok := selected["thread_id"]; !ok {
		selected["thread_id"] = thread["thread_id"]
	}
	return selected
}

func stringSliceFromAny(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func (s *Server) handleThreadFiles(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req struct {
		Values   map[string]any `json:"values"`
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
	applyThreadStateUpdate(session, req.Values, req.Metadata)
	session.UpdatedAt = time.Now().UTC()
	snapshot := cloneSession(session)
	s.sessionsMu.Unlock()
	_ = s.persistSessionSnapshot(snapshot)

	writeJSON(w, http.StatusOK, s.getThreadState(threadID))
}

func (s *Server) handleThreadStatePatch(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req struct {
		Values   map[string]any `json:"values"`
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
	applyThreadStateUpdate(session, req.Values, req.Metadata)
	session.UpdatedAt = time.Now().UTC()
	snapshot := cloneSession(session)
	s.sessionsMu.Unlock()
	_ = s.persistSessionSnapshot(snapshot)

	writeJSON(w, http.StatusOK, s.getThreadState(threadID))
}

func applyThreadStateUpdate(session *Session, values map[string]any, metadata map[string]any) {
	if session == nil {
		return
	}
	if session.Values == nil {
		session.Values = make(map[string]any)
	}
	if session.Metadata == nil {
		session.Metadata = make(map[string]any)
	}
	for key, value := range values {
		switch key {
		case "title":
			if title, ok := value.(string); ok {
				session.Metadata["title"] = title
			}
		case "todos":
			if todos, err := decodeTodos(value); err == nil {
				session.Todos = append([]Todo(nil), todos...)
			}
		case "messages", "artifacts", "thread_data", "uploaded_files":
			// These values are derived from the session and filesystem state.
		default:
			session.Values[key] = value
		}
	}
	for k, v := range metadata {
		session.Metadata[k] = v
	}
}

func (s *Server) handleThreadHistory(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req struct {
		Limit int `json:"limit"`
	}
	if r.Method == http.MethodGet {
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			limit, err := strconv.Atoi(raw)
			if err != nil {
				http.Error(w, "invalid limit", http.StatusBadRequest)
				return
			}
			req.Limit = limit
		}
	} else if r.Body != nil {
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	state := s.getThreadState(threadID)
	if state == nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}

	history := s.threadHistory(threadID)
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

	writeJSON(w, http.StatusOK, s.runResponse(run))
}

func (s *Server) handleThreadRunGet(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.getThreadState(threadID) == nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}

	runID := r.PathValue("run_id")
	run := s.getRun(runID)
	if run == nil || run.ThreadID != threadID {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, s.runResponse(run))
}

func (s *Server) handleThreadRunsList(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.getThreadState(threadID) == nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}

	s.runsMu.RLock()
	runs := make([]*Run, 0)
	for _, run := range s.runs {
		if run.ThreadID != threadID {
			continue
		}
		copyRun := *run
		copyRun.Events = append([]StreamEvent(nil), run.Events...)
		runs = append(runs, &copyRun)
	}
	s.runsMu.RUnlock()

	sort.Slice(runs, func(i, j int) bool {
		return runs[i].CreatedAt.After(runs[j].CreatedAt)
	})

	items := make([]map[string]any, 0, len(runs))
	for _, run := range runs {
		items = append(items, s.runResponse(run))
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": items})
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
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.clarifyAPI.HandleGet(w, r, threadID, r.PathValue("id"))
}

func (s *Server) handleThreadClarificationResolve(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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

		item := Message{
			Type:             msgType,
			ID:               msg.ID,
			Role:             role,
			Content:          messageContent(msg.Content, msg.Metadata),
			AdditionalKwargs: decodeAdditionalKwargs(msg.Metadata),
			UsageMetadata:    decodeUsageMetadata(msg.Metadata),
		}
		if msg.Role == models.RoleAI && len(msg.ToolCalls) > 0 {
			item.ToolCalls = toLangChainToolCalls(msg.ToolCalls)
		}
		if msg.Role == models.RoleTool && msg.ToolResult != nil {
			item.Name = msg.ToolResult.ToolName
			item.ToolCallID = msg.ToolResult.CallID
			item.Data = map[string]any{
				"status": msg.ToolResult.Status,
			}
			if msg.ToolResult.Error != "" {
				item.Data["error"] = msg.ToolResult.Error
			}
		}
		result = append(result, item)
	}
	return result
}

func decodeAdditionalKwargs(metadata map[string]string) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	raw := strings.TrimSpace(metadata["additional_kwargs"])
	if raw == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func decodeMultiContent(metadata map[string]string) []map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	raw := strings.TrimSpace(metadata["multi_content"])
	if raw == "" {
		return nil
	}
	var out []map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func messageContent(content string, metadata map[string]string) any {
	if multi := decodeMultiContent(metadata); len(multi) > 0 {
		return multi
	}
	return content
}

func decodeUsageMetadata(metadata map[string]string) map[string]int {
	if len(metadata) == 0 {
		return nil
	}
	raw := strings.TrimSpace(metadata["usage_metadata"])
	if raw == "" {
		return nil
	}
	var usage map[string]int
	if err := json.Unmarshal([]byte(raw), &usage); err != nil {
		return nil
	}
	if len(usage) == 0 {
		return nil
	}
	return usage
}

func toLangChainToolCalls(calls []models.ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, ToolCall{
			ID:   call.ID,
			Name: call.Name,
			Args: cloneToolArguments(call.Arguments),
		})
	}
	return out
}

func cloneToolArguments(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	return out
}

func (s *Server) threadResponse(session *Session) map[string]any {
	configurable := copyMetadataMap(session.Configurable)
	if configurable == nil {
		configurable = map[string]any{}
	}
	if _, ok := configurable["agent_name"]; !ok {
		configurable["agent_name"] = stringValue(session.Metadata["agent_name"])
	}
	if _, ok := configurable["agent_type"]; !ok {
		configurable["agent_type"] = stringValue(session.Metadata["agent_type"])
	}
	return map[string]any{
		"thread_id":  session.ThreadID,
		"created_at": session.CreatedAt.Format(time.RFC3339Nano),
		"updated_at": session.UpdatedAt.Format(time.RFC3339Nano),
		"metadata":   session.Metadata,
		"status":     session.Status,
		"config": map[string]any{
			"configurable": configurable,
		},
		"values": s.threadValues(session),
	}
}

func (s *Server) runResponse(run *Run) map[string]any {
	if run == nil {
		return map[string]any{}
	}
	resp := map[string]any{
		"run_id":       run.RunID,
		"thread_id":    run.ThreadID,
		"assistant_id": run.AssistantID,
		"status":       run.Status,
		"created_at":   run.CreatedAt.Format(time.RFC3339Nano),
		"updated_at":   run.UpdatedAt.Format(time.RFC3339Nano),
	}
	if run.Error != "" {
		resp["error"] = run.Error
	}
	return resp
}

func (s *Server) getThreadState(threadID string) *ThreadState {
	s.sessionsMu.RLock()
	session, exists := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if !exists {
		return nil
	}

	return s.threadStateFromSession(session, uuid.New().String(), session.UpdatedAt)
}

func (s *Server) threadHistory(threadID string) []ThreadState {
	entries, err := s.readPersistedHistory(threadID)
	if err != nil {
		return nil
	}
	history := make([]ThreadState, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		session := &Session{
			ThreadID:     entry.ThreadID,
			Messages:     append([]models.Message(nil), entry.Messages...),
			Todos:        append([]Todo(nil), entry.Todos...),
			Values:       copyMetadataMap(entry.Values),
			Metadata:     copyMetadataMap(entry.Metadata),
			Configurable: copyMetadataMap(entry.Config),
			Status:       entry.Status,
			CreatedAt:    entry.CreatedAt,
			UpdatedAt:    entry.UpdatedAt,
		}
		state := s.threadStateFromSession(session, entry.CheckpointID, entry.UpdatedAt)
		if state != nil {
			history = append(history, *state)
		}
	}
	return history
}

func (s *Server) threadStateFromSession(session *Session, checkpointID string, createdAt time.Time) *ThreadState {
	if session == nil {
		return nil
	}
	values := s.threadValues(session)
	values["messages"] = s.messagesToLangChain(session.Messages)
	configurable := copyMetadataMap(session.Configurable)
	if configurable == nil {
		configurable = map[string]any{}
	}
	if _, ok := configurable["agent_name"]; !ok {
		configurable["agent_name"] = stringValue(session.Metadata["agent_name"])
	}
	if _, ok := configurable["agent_type"]; !ok {
		configurable["agent_type"] = stringValue(session.Metadata["agent_type"])
	}

	return &ThreadState{
		CheckpointID: checkpointID,
		Values:       values,
		Next:         []string{},
		Tasks:        []any{},
		Metadata: map[string]any{
			"thread_id": session.ThreadID,
			"step":      0,
		},
		Config: map[string]any{
			"configurable": configurable,
		},
		CreatedAt: createdAt.Format(time.RFC3339Nano),
	}
}

func (s *Server) ensureSession(threadID string, metadata map[string]any) *Session {
	s.sessionsMu.Lock()
	var snapshot *Session
	if session, exists := s.sessions[threadID]; exists {
		if metadata != nil {
			for k, v := range metadata {
				session.Metadata[k] = v
			}
			session.UpdatedAt = time.Now().UTC()
			snapshot = cloneSession(session)
		}
		s.sessionsMu.Unlock()
		if snapshot != nil {
			_ = s.persistSessionSnapshot(snapshot)
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
		Todos:        nil,
		Values:       map[string]any{},
		Metadata:     metadata,
		Configurable: defaultThreadConfig(threadID),
		Status:       "idle",
		PresentFiles: tools.NewPresentFileRegistry(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.sessions[threadID] = session
	snapshot = cloneSession(session)
	s.sessionsMu.Unlock()
	_ = s.persistSessionSnapshot(snapshot)
	return session
}

func (s *Server) markThreadStatus(threadID string, status string) {
	s.sessionsMu.Lock()
	var snapshot *Session
	if session, exists := s.sessions[threadID]; exists {
		session.Status = status
		session.UpdatedAt = time.Now().UTC()
		snapshot = cloneSession(session)
	}
	s.sessionsMu.Unlock()
	_ = s.persistSessionSnapshot(snapshot)
}

func (s *Server) setThreadMetadata(threadID string, key string, value any) {
	s.sessionsMu.Lock()
	var snapshot *Session
	if session, exists := s.sessions[threadID]; exists {
		if session.Metadata == nil {
			session.Metadata = make(map[string]any)
		}
		session.Metadata[key] = value
		session.UpdatedAt = time.Now().UTC()
		snapshot = cloneSession(session)
	}
	s.sessionsMu.Unlock()
	_ = s.persistSessionSnapshot(snapshot)
}

func (s *Server) setThreadConfig(threadID string, values map[string]any) {
	if len(values) == 0 {
		return
	}
	s.sessionsMu.Lock()
	var snapshot *Session
	if session, exists := s.sessions[threadID]; exists {
		if session.Configurable == nil {
			session.Configurable = defaultThreadConfig(threadID)
		}
		for key, value := range values {
			session.Configurable[key] = value
		}
		session.UpdatedAt = time.Now().UTC()
		snapshot = cloneSession(session)
	}
	s.sessionsMu.Unlock()
	_ = s.persistSessionSnapshot(snapshot)
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
	subscribers := make([]chan StreamEvent, 0)
	if run, exists := s.runs[runID]; exists {
		run.Events = append(run.Events, event)
		run.UpdatedAt = time.Now().UTC()
		for _, ch := range s.runStreams[runID] {
			subscribers = append(subscribers, ch)
		}
	}
	s.runsMu.Unlock()

	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
		}
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

func (s *Server) getLatestActiveRunForThread(threadID string) *Run {
	s.runsMu.RLock()
	defer s.runsMu.RUnlock()

	var latest *Run
	for _, run := range s.runs {
		if run.ThreadID != threadID {
			continue
		}
		if run.Status != "running" {
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

func (s *Server) subscribeRun(runID string) (*Run, <-chan StreamEvent) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()

	run, exists := s.runs[runID]
	if !exists {
		return nil, nil
	}

	copyRun := *run
	copyRun.Events = append([]StreamEvent(nil), run.Events...)
	if run.Status != "running" {
		return &copyRun, nil
	}
	if run.abandonTimer != nil {
		run.abandonTimer.Stop()
		run.abandonTimer = nil
	}

	streamID := atomic.AddUint64(&s.runStreamSeq, 1)
	ch := make(chan StreamEvent, 64)
	if s.runStreams[runID] == nil {
		s.runStreams[runID] = make(map[uint64]chan StreamEvent)
	}
	s.runStreams[runID][streamID] = ch
	return &copyRun, ch
}

func (s *Server) unsubscribeRun(runID string, stream <-chan StreamEvent) {
	if stream == nil {
		return
	}

	s.runsMu.Lock()
	defer s.runsMu.Unlock()

	run := s.runs[runID]
	subscribers := s.runStreams[runID]
	for id, ch := range subscribers {
		if (<-chan StreamEvent)(ch) == stream {
			delete(subscribers, id)
			break
		}
	}
	if len(subscribers) == 0 {
		delete(s.runStreams, runID)
		s.armRunAbandonTimerLocked(run)
	}
}

func (s *Server) armRunAbandonTimer(runID string) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	s.armRunAbandonTimerLocked(s.runs[runID])
}

func (s *Server) clearRunLifecycle(runID string) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()

	run, ok := s.runs[runID]
	if !ok {
		return
	}
	if run.abandonTimer != nil {
		run.abandonTimer.Stop()
		run.abandonTimer = nil
	}
	run.cancel = nil
}

func (s *Server) armRunAbandonTimerLocked(run *Run) {
	if run == nil || run.Status != "running" || run.cancel == nil {
		return
	}
	if run.abandonTimer != nil {
		run.abandonTimer.Stop()
	}

	runID := run.RunID
	cancel := run.cancel
	run.abandonTimer = time.AfterFunc(runReconnectGracePeriod(), func() {
		s.cancelAbandonedRun(runID, cancel)
	})
}

func (s *Server) cancelAbandonedRun(runID string, cancel context.CancelFunc) {
	s.runsMu.Lock()
	run, ok := s.runs[runID]
	if !ok || run.Status != "running" || run.cancel == nil {
		s.runsMu.Unlock()
		return
	}
	if len(s.runStreams[runID]) > 0 {
		run.abandonTimer = nil
		s.runsMu.Unlock()
		return
	}
	run.abandonTimer = nil
	run.cancel = nil
	s.runsMu.Unlock()

	cancel()
}

func runReconnectGracePeriod() time.Duration {
	const fallback = 2 * time.Second
	raw := strings.TrimSpace(os.Getenv("DEERFLOW_RUN_RECONNECT_GRACE"))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func (s *Server) sessionArtifactPaths(session *Session) []string {
	if session == nil {
		return []string{}
	}

	seen := make(map[string]struct{})
	paths := make([]string, 0)
	if session.PresentFiles != nil {
		files := session.PresentFiles.List()
		for _, file := range files {
			if file.Path == "" {
				continue
			}
			if _, ok := seen[file.Path]; ok {
				continue
			}
			seen[file.Path] = struct{}{}
			paths = append(paths, file.Path)
		}
	}
	for _, path := range collectArtifactPaths(
		filepath.Join(s.threadRoot(session.ThreadID), "outputs"),
		"/mnt/user-data/outputs",
	) {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	for _, path := range collectArtifactPaths(
		filepath.Join(s.threadRoot(session.ThreadID), "workspace"),
		"/mnt/user-data/workspace",
	) {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	for _, path := range s.uploadArtifactPaths(session.ThreadID) {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

func (s *Server) uploadArtifactPaths(threadID string) []string {
	files := s.listUploadedFiles(threadID)
	if len(files) == 0 {
		return nil
	}

	paths := make([]string, 0, len(files)*2)
	for _, info := range files {
		if markdownPath := strings.TrimSpace(asString(info["markdown_virtual_path"])); markdownPath != "" {
			paths = append(paths, markdownPath)
		}
		if path := strings.TrimSpace(asString(info["path"])); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func cloneSession(session *Session) *Session {
	if session == nil {
		return nil
	}
	return &Session{
		ThreadID:     session.ThreadID,
		Messages:     append([]models.Message(nil), session.Messages...),
		Todos:        append([]Todo(nil), session.Todos...),
		Values:       copyMetadataMap(session.Values),
		Metadata:     copyMetadataMap(session.Metadata),
		Configurable: copyMetadataMap(session.Configurable),
		Status:       session.Status,
		CreatedAt:    session.CreatedAt,
		UpdatedAt:    session.UpdatedAt,
	}
}

func defaultThreadConfig(threadID string) map[string]any {
	cfg := map[string]any{}
	if strings.TrimSpace(threadID) != "" {
		cfg["thread_id"] = threadID
	}
	return cfg
}

func (s *Server) threadValues(session *Session) map[string]any {
	values := copyMetadataMap(session.Values)
	if values == nil {
		values = map[string]any{}
	}
	values["title"] = stringValue(session.Metadata["title"])
	values["artifacts"] = s.sessionArtifactPaths(session)
	values["todos"] = todosToAny(session.Todos)
	values["thread_data"] = s.threadDataState(session.ThreadID)
	values["uploaded_files"] = s.listUploadedFiles(session.ThreadID)
	return values
}

func (s *Server) deleteGatewayThreadData(threadID string) error {
	if strings.TrimSpace(threadID) == "" {
		return nil
	}

	var snapshot *Session
	s.sessionsMu.Lock()
	if session := s.sessions[threadID]; session != nil {
		if session.PresentFiles != nil {
			session.PresentFiles.Clear()
		}
		session.UpdatedAt = time.Now().UTC()
		snapshot = cloneSession(session)
	}
	s.sessionsMu.Unlock()

	if snapshot != nil {
		if err := s.persistSessionSnapshot(snapshot); err != nil {
			return err
		}
	}

	if err := os.RemoveAll(s.threadRoot(threadID)); err != nil {
		return err
	}

	acpWorkspace, err := tools.ACPWorkspaceDir(threadID)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(acpWorkspace); err != nil {
		return err
	}
	return nil
}

func (s *Server) deleteThreadResources(threadID string, removeLocalData bool) error {
	s.sessionsMu.Lock()
	delete(s.sessions, threadID)
	s.sessionsMu.Unlock()

	s.runsMu.Lock()
	for runID, run := range s.runs {
		if run == nil || run.ThreadID != threadID {
			continue
		}
		delete(s.runs, runID)
		delete(s.runStreams, runID)
	}
	s.runsMu.Unlock()

	if err := s.deletePersistedSession(threadID); err != nil {
		return err
	}
	if !removeLocalData {
		return nil
	}
	if err := os.RemoveAll(s.threadDir(threadID)); err != nil {
		return err
	}
	return nil
}

func (s *Server) threadDataState(threadID string) map[string]any {
	acpWorkspace, _ := tools.ACPWorkspaceDir(threadID)
	return map[string]any{
		"workspace_path":     filepath.Join(s.threadRoot(threadID), "workspace"),
		"uploads_path":       s.uploadsDir(threadID),
		"outputs_path":       filepath.Join(s.threadRoot(threadID), "outputs"),
		"acp_workspace_path": acpWorkspace,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
