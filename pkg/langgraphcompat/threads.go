package langgraphcompat

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

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

func stringValue(v any) string {
	s, _ := v.(string)
	return s
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
