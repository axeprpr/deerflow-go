package langgraphcompat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

type runConfig struct {
	ModelName       string
	Mode            string
	ExecutionMode   string
	ReasoningEffort string
	AgentType       agent.AgentType
	AgentName       string
	MemoryUserID    string
	MemoryGroupID   string
	MemoryNamespace string
	MaxTurns        *int
	ThinkingEnabled *bool
	IsPlanMode      *bool
	SubagentEnabled *bool
	Temperature     *float64
	MaxTokens       *int
}

type streamModeFilter struct {
	allowAll bool
	allowed  map[string]struct{}
}

const maxUploadedImageParts = 4
const defaultSSEHeartbeatInterval = 15 * time.Second

type synchronizedSSEWriter struct {
	http.ResponseWriter
	flusher http.Flusher
	mu      *sync.Mutex
}

func newSSEWriter(w http.ResponseWriter, flusher http.Flusher) *synchronizedSSEWriter {
	return &synchronizedSSEWriter{
		ResponseWriter: w,
		flusher:        flusher,
		mu:             &sync.Mutex{},
	}
}

func (w *synchronizedSSEWriter) Flush() {
	if w == nil || w.flusher == nil {
		return
	}
	w.flusher.Flush()
}

func (w *synchronizedSSEWriter) lockSSE() {
	if w == nil || w.mu == nil {
		return
	}
	w.mu.Lock()
}

func (w *synchronizedSSEWriter) unlockSSE() {
	if w == nil || w.mu == nil {
		return
	}
	w.mu.Unlock()
}

func clarificationInterruptFromMessages(messages []models.Message) map[string]any {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != models.RoleTool || msg.ToolResult == nil {
			continue
		}
		if strings.TrimSpace(msg.ToolResult.ToolName) != "ask_clarification" || msg.ToolResult.Status != models.CallStatusCompleted {
			continue
		}
		value := strings.TrimSpace(firstNonEmpty(msg.Content, msg.ToolResult.Content))
		if value == "" {
			value = "Clarification requested"
		}
		interrupt := map[string]any{"value": value}
		if len(msg.ToolResult.Data) > 0 {
			if id := stringValue(msg.ToolResult.Data["id"]); id != "" {
				interrupt["id"] = id
			}
			if question := stringValue(msg.ToolResult.Data["question"]); question != "" {
				interrupt["question"] = question
			}
			if clarificationType := stringValue(msg.ToolResult.Data["clarification_type"]); clarificationType != "" {
				interrupt["clarification_type"] = clarificationType
			}
		}
		return interrupt
	}
	return nil
}

func streamResumableRequested(req RunCreateRequest) bool {
	if req.StreamResumable != nil {
		return *req.StreamResumable
	}
	if req.StreamResumableX != nil {
		return *req.StreamResumableX
	}
	return false
}

func requestedOnDisconnect(req RunCreateRequest) string {
	mode := strings.ToLower(strings.TrimSpace(req.OnDisconnect))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(req.OnDisconnectX))
	}
	switch mode {
	case "", "continue":
		return "continue"
	case "cancel":
		return "cancel"
	default:
		return "continue"
	}
}

func isRunCanceledErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (s *Server) convertToMessages(threadID string, input []any, includeUploadedImages ...bool) []models.Message {
	messages := make([]models.Message, 0, len(input))
	msgSeq := uint64(time.Now().UnixNano())
	visionEnabled := len(includeUploadedImages) > 0 && includeUploadedImages[0]

	for _, m := range input {
		msgMap, ok := m.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msgMap["role"].(string)
		if role == "" {
			role, _ = msgMap["type"].(string)
		}

		content := extractMessageContent(msgMap["content"])
		toolCalls := parseLangGraphToolCalls(msgMap["tool_calls"])
		if s.convertRole(role) == models.RoleHuman {
			content = s.injectUploadedFilesContext(threadID, msgMap, content)
		}
		if role == "" || (content == "" && len(toolCalls) == 0) {
			continue
		}

		msgSeq++
		msgID := strings.TrimSpace(stringFromAny(msgMap["id"]))
		if msgID == "" {
			msgID = fmt.Sprintf("msg_%d", msgSeq)
		}
		msg := models.Message{
			ID:        msgID,
			SessionID: threadID,
			Role:      s.convertRole(role),
			Content:   content,
		}
		if metadata := parseLangGraphMessageMetadata(msgMap["additional_kwargs"]); len(metadata) > 0 {
			msg.Metadata = metadata
		}
		if rawAdditionalKwargs, ok := msgMap["additional_kwargs"].(map[string]any); ok && len(rawAdditionalKwargs) > 0 {
			if encoded, err := json.Marshal(rawAdditionalKwargs); err == nil {
				if msg.Metadata == nil {
					msg.Metadata = map[string]string{}
				}
				msg.Metadata["additional_kwargs"] = string(encoded)
			}
		}
		if msg.Role == models.RoleHuman {
			multi := buildMultiContent(content, msgMap["content"], s.uploadedImageParts(threadID, msgMap, visionEnabled))
			if len(multi) > 0 {
				if msg.Metadata == nil {
					msg.Metadata = map[string]string{}
				}
				if encoded, err := json.Marshal(multi); err == nil {
					msg.Metadata["multi_content"] = string(encoded)
				}
			}
		}
		if metadata := parseLangGraphMessageMetadata(msgMap["response_metadata"]); len(metadata) > 0 {
			if msg.Metadata == nil {
				msg.Metadata = metadata
			} else {
				for key, value := range metadata {
					if _, exists := msg.Metadata[key]; !exists {
						msg.Metadata[key] = value
					}
				}
			}
		}
		if metadata := parseLangGraphUsageMetadata(msgMap["usage_metadata"]); len(metadata) > 0 {
			if msg.Metadata == nil {
				msg.Metadata = metadata
			} else {
				for key, value := range metadata {
					msg.Metadata[key] = value
				}
			}
		}
		if status := strings.TrimSpace(stringFromAny(msgMap["status"])); status != "" {
			if msg.Metadata == nil {
				msg.Metadata = map[string]string{}
			}
			msg.Metadata["message_status"] = status
		}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
		}
		if msg.Role == models.RoleTool {
			msg.ToolResult = parseLangGraphToolResult(msgMap)
		}
		messages = append(messages, msg)
	}

	return messages
}

func parseLangGraphMessageMetadata(raw any) map[string]string {
	data, _ := raw.(map[string]any)
	if len(data) == 0 {
		return nil
	}
	out := make(map[string]string)
	for key, value := range data {
		if text := strings.TrimSpace(stringFromAny(value)); text != "" {
			out[key] = text
			continue
		}
		if encoded, err := json.Marshal(value); err == nil && string(encoded) != "null" {
			out[key] = string(encoded)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseLangGraphUsageMetadata(raw any) map[string]string {
	data, _ := raw.(map[string]any)
	if len(data) == 0 {
		return nil
	}
	out := make(map[string]string)
	for _, key := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		if value, ok := data[key]; ok {
			out["usage_"+key] = strconv.FormatInt(toInt64(value), 10)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseLangGraphToolCalls(raw any) []models.ToolCall {
	items, _ := raw.([]any)
	if len(items) == 0 {
		return nil
	}
	out := make([]models.ToolCall, 0, len(items))
	for _, item := range items {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(stringFromAny(call["id"]))
		name := strings.TrimSpace(stringFromAny(call["name"]))
		if id == "" || name == "" {
			continue
		}
		args, _ := call["args"].(map[string]any)
		out = append(out, models.ToolCall{
			ID:        id,
			Name:      name,
			Arguments: args,
			Status:    models.CallStatusCompleted,
		})
	}
	return out
}

func parseLangGraphToolResult(msg map[string]any) *models.ToolResult {
	callID := strings.TrimSpace(stringFromAny(msg["tool_call_id"]))
	toolName := strings.TrimSpace(stringFromAny(msg["name"]))
	if callID == "" || toolName == "" {
		return nil
	}
	result := &models.ToolResult{
		CallID:   callID,
		ToolName: toolName,
		Status:   models.CallStatusCompleted,
		Content:  extractMessageContent(msg["content"]),
	}
	data, _ := msg["data"].(map[string]any)
	if len(data) == 0 {
		return result
	}
	if status := strings.TrimSpace(stringFromAny(data["status"])); status != "" {
		switch models.CallStatus(status) {
		case models.CallStatusPending, models.CallStatusRunning, models.CallStatusCompleted, models.CallStatusFailed:
			result.Status = models.CallStatus(status)
		}
	}
	result.Error = stringFromAny(data["error"])
	if inner, ok := data["data"].(map[string]any); ok && len(inner) > 0 {
		result.Data = inner
	}
	if durationText := strings.TrimSpace(stringFromAny(data["duration"])); durationText != "" {
		if duration, err := time.ParseDuration(durationText); err == nil {
			result.Duration = duration
		}
	}
	return result
}

func (s *Server) injectUploadedFilesContext(threadID string, message map[string]any, content string) string {
	newFiles := s.extractMessageFiles(threadID, message)
	historical := s.uploadedFilesState(threadID)
	if len(newFiles) == 0 && len(historical) == 0 {
		return content
	}

	newNames := make(map[string]struct{}, len(newFiles))
	for _, file := range newFiles {
		if name := stringFromAny(file["filename"]); name != "" {
			newNames[name] = struct{}{}
		}
	}

	lines := []string{
		"<uploaded_files>",
		"The following files were uploaded in this message:",
		"",
	}
	if len(newFiles) == 0 {
		lines = append(lines, "(empty)")
	} else {
		for _, file := range newFiles {
			lines = append(lines, formatUploadedFileBullet(file)...)
		}
	}

	historicalLines := make([]string, 0)
	for _, file := range historical {
		filename := asString(file["filename"])
		if _, exists := newNames[filename]; exists {
			continue
		}
		historicalLines = append(historicalLines, formatUploadedFileBullet(file)...)
	}
	if len(historicalLines) > 0 {
		lines = append(lines, "The following files were uploaded in previous messages and are still available:", "")
		lines = append(lines, historicalLines...)
	}
	lines = append(lines, "You can read these files using the `read_file` tool with the paths shown above.", "</uploaded_files>")

	filesMessage := strings.Join(lines, "\n")
	if strings.TrimSpace(content) == "" {
		return filesMessage
	}
	return filesMessage + "\n\n" + content
}

func buildMultiContent(content string, rawContent any, uploadedImageParts []map[string]any) []map[string]any {
	items, _ := rawContent.([]any)
	if len(items) == 0 && len(uploadedImageParts) == 0 {
		originalContent := strings.TrimSpace(extractMessageContent(rawContent))
		if strings.TrimSpace(content) == "" || strings.TrimSpace(content) == originalContent {
			return nil
		}
		return []map[string]any{{"type": "text", "text": content}}
	}
	multi := []map[string]any{{"type": "text", "text": content}}
	originalTextParts := make([]string, 0, len(items))
	for _, item := range items {
		part, _ := item.(map[string]any)
		if part == nil {
			continue
		}
		switch asString(part["type"]) {
		case "image_url":
			multi = append(multi, part)
		case "text":
			if text := strings.TrimSpace(asString(part["text"])); text != "" {
				originalTextParts = append(originalTextParts, text)
			}
		default:
			continue
		}
	}
	if len(originalTextParts) > 0 {
		originalText := strings.Join(originalTextParts, "\n")
		if strings.TrimSpace(originalText) != "" && strings.TrimSpace(originalText) != strings.TrimSpace(content) {
			multi = append([]map[string]any{
				multi[0],
				map[string]any{"type": "text", "text": originalText},
			}, multi[1:]...)
		}
	}
	if len(uploadedImageParts) > 0 {
		multi = append(multi, uploadedImageParts...)
	}
	return multi
}

func (s *Server) uploadedImageParts(threadID string, message map[string]any, enabled bool) []map[string]any {
	if !enabled {
		return nil
	}
	files := s.extractMessageFiles(threadID, message)
	if len(files) == 0 {
		files = s.uploadedFilesState(threadID)
	}
	parts := make([]map[string]any, 0, len(files))
	for _, file := range files {
		if len(parts) >= maxUploadedImageParts {
			break
		}
		name := asString(file["filename"])
		if !isUploadedImageFile(name) {
			continue
		}
		path := asString(file["path"])
		if path == "" {
			path = filepath.Join(s.uploadsDir(threadID), name)
		}
		if !strings.HasPrefix(path, s.uploadsDir(threadID)) {
			path = filepath.Join(s.uploadsDir(threadID), filepath.Base(path))
		}
		part, ok := imageURLPartFromFile(path)
		if !ok {
			continue
		}
		parts = append(parts, part)
	}
	return parts
}

func isUploadedImageFile(name string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(name))) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func imageURLPartFromFile(path string) (map[string]any, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if strings.TrimSpace(mimeType) == "" {
		mimeType = "application/octet-stream"
	}
	return map[string]any{
		"type": "image_url",
		"image_url": map[string]any{
			"url": "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data),
		},
	}, true
}

func (s *Server) extractMessageFiles(threadID string, message map[string]any) []map[string]any {
	additionalKwargs, _ := message["additional_kwargs"].(map[string]any)
	if len(additionalKwargs) == 0 {
		return nil
	}
	rawFiles, _ := additionalKwargs["files"].([]any)
	if len(rawFiles) == 0 {
		return nil
	}
	files := make([]map[string]any, 0, len(rawFiles))
	for _, raw := range rawFiles {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		filename := stringFromAny(item["filename"])
		if filename == "" {
			continue
		}
		virtualPath := "/mnt/user-data/uploads/" + filename
		sourcePath := filepath.Join(s.uploadsDir(threadID), filename)
		stat, err := os.Stat(sourcePath)
		explicitPath := firstNonEmpty(stringFromAny(item["virtual_path"]), stringFromAny(item["path"]))
		path := virtualPath
		if err == nil && !stat.IsDir() {
			if toInt64(item["size"]) <= 0 {
				item["size"] = stat.Size()
			}
		} else if strings.HasPrefix(explicitPath, "/mnt/user-data/uploads/") {
			path = explicitPath
		} else {
			continue
		}
		if err == nil && !stat.IsDir() && toInt64(item["size"]) <= 0 {
			item["size"] = stat.Size()
		}
		file := map[string]any{
			"filename": filename,
			"size":     toInt64(item["size"]),
			"path":     path,
		}
		if markdownPath := strings.TrimSpace(stringFromAny(firstNonNil(item["markdown_path"], item["markdownPath"]))); markdownPath != "" {
			if !strings.HasPrefix(markdownPath, "/mnt/user-data/uploads/") {
				markdownPath = "/mnt/user-data/uploads/" + filepath.Base(markdownPath)
			}
			file["markdown_path"] = markdownPath
		} else if markdownFile := strings.TrimSpace(stringFromAny(firstNonNil(item["markdown_file"], item["markdownFile"]))); markdownFile != "" {
			file["markdown_path"] = "/mnt/user-data/uploads/" + markdownFile
		} else if mdName := strings.TrimSuffix(filename, filepath.Ext(filename)) + ".md"; mdName != filename {
			if stat, err := os.Stat(filepath.Join(s.uploadsDir(threadID), mdName)); err == nil && !stat.IsDir() {
				file["markdown_path"] = "/mnt/user-data/uploads/" + mdName
			}
		}
		files = append(files, file)
	}
	return files
}

func formatUploadedFileBullet(file map[string]any) []string {
	filename := asString(file["filename"])
	path := firstNonEmpty(asString(file["path"]), asString(file["virtual_path"]))
	size := toInt64(file["size"])
	sizeValue := float64(size) / 1024
	sizeLabel := fmt.Sprintf("%.1f KB", sizeValue)
	if sizeValue >= 1024 {
		sizeLabel = fmt.Sprintf("%.1f MB", sizeValue/1024)
	}
	lines := []string{
		fmt.Sprintf("- %s (%s)", filename, sizeLabel),
		fmt.Sprintf("  Path: %s", path),
	}
	if markdownPath := strings.TrimSpace(asString(file["markdown_path"])); markdownPath != "" {
		lines = append(lines, fmt.Sprintf("  Markdown copy: %s", markdownPath))
	}
	lines = append(lines, "")
	return lines
}

func extractMessageContent(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			part, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if partType, _ := part["type"].(string); partType == "text" {
				if text, _ := part["text"].(string); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func (s *Server) convertRole(langchainRole string) models.Role {
	switch strings.ToLower(langchainRole) {
	case "human", "user":
		return models.RoleHuman
	case "ai", "assistant":
		return models.RoleAI
	case "system":
		return models.RoleSystem
	case "tool":
		return models.RoleTool
	default:
		return models.RoleHuman
	}
}

func (s *Server) sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, event StreamEvent) {
	if w == nil || flusher == nil {
		return
	}
	if locker, ok := w.(interface {
		lockSSE()
		unlockSSE()
	}); ok {
		locker.lockSSE()
		defer locker.unlockSSE()
	}
	jsonData, err := json.Marshal(event.Data)
	if err != nil {
		return
	}

	if event.ID != "" {
		fmt.Fprintf(w, "id: %s\n", event.ID)
	}
	fmt.Fprintf(w, "event: %s\n", event.Event)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	flusher.Flush()
}

func sendSSEHeartbeat(w http.ResponseWriter, flusher http.Flusher) {
	if w == nil || flusher == nil {
		return
	}
	if locker, ok := w.(interface {
		lockSSE()
		unlockSSE()
	}); ok {
		locker.lockSSE()
		defer locker.unlockSSE()
	}
	fmt.Fprint(w, ": heartbeat\n\n")
	flusher.Flush()
}

func streamSSEHeartbeats(ctx context.Context, done <-chan struct{}, w http.ResponseWriter, flusher http.Flusher) {
	ticker := time.NewTicker(defaultSSEHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			sendSSEHeartbeat(w, flusher)
		}
	}
}

func (s *Server) saveSession(threadID string, messages []models.Message) {
	s.sessionsMu.Lock()
	var session *Session
	if session, exists := s.sessions[threadID]; exists {
		session.Messages = append([]models.Message(nil), messages...)
		session.Status = "idle"
		session.UpdatedAt = time.Now().UTC()
	} else {
		session = &Session{
			ThreadID:     threadID,
			Messages:     append([]models.Message(nil), messages...),
			Metadata:     make(map[string]any),
			Status:       "idle",
			PresentFiles: tools.NewPresentFileRegistry(),
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		s.sessions[threadID] = session
	}
	s.sessionsMu.Unlock()
	_ = s.persistSessionFile(session)
	_ = s.appendThreadHistorySnapshot(threadID)
}

func (s *Server) cancelRunOnClientDisconnect(clientCtx context.Context, runDone <-chan struct{}, cancel context.CancelFunc) {
	if clientCtx == nil || cancel == nil {
		return
	}
	select {
	case <-runDone:
		return
	case <-clientCtx.Done():
	}
	cancel()
}

func (s *Server) runHasJoinSubscribers(runID string) bool {
	return s.ensureRunRegistry().hasSubscribers(runID)
}

func newStreamModeFilter(raw any) streamModeFilter {
	filter := streamModeFilter{allowAll: true}
	values := streamModeValues(raw)
	if len(values) == 0 {
		return filter
	}
	filter.allowAll = false
	filter.allowed = make(map[string]struct{}, len(values)+2)
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "events", "debug", "custom":
			filter.allow("metadata")
			filter.allow("clarification_request")
			filter.allow("chunk")
			filter.allow("tool_call")
			filter.allow("tool_call_start")
			filter.allow("tool_call_end")
			filter.allow("on_tool_end")
			filter.allow("error")
			filter.allow("task_started")
			filter.allow("task_running")
			filter.allow("task_completed")
			filter.allow("task_failed")
		case "messages":
			filter.allow("chunk")
			filter.allow("messages-tuple")
		case "messages-tuple":
			filter.allow("messages-tuple")
		case "tasks":
			filter.allow("task_started")
			filter.allow("task_running")
			filter.allow("task_completed")
			filter.allow("task_failed")
		case "values":
			filter.allow("values")
		case "updates":
			filter.allow("updates")
		}
	}
	filter.allow("end")
	filter.allow("error")
	return filter
}

func streamModeValues(raw any) []string {
	switch value := raw.(type) {
	case string:
		if value == "" {
			return nil
		}
		return []string{value}
	case []string:
		return value
	case []any:
		values := make([]string, 0, len(value))
		for _, item := range value {
			if text := stringFromAny(item); text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

func streamModeFromQuery(r *http.Request) any {
	if r == nil || r.URL == nil {
		return nil
	}
	query := r.URL.Query()
	values := make([]string, 0, len(query["stream_mode"])+len(query["streamMode"]))
	values = append(values, splitStreamModeQueryValues(query["stream_mode"])...)
	values = append(values, splitStreamModeQueryValues(query["streamMode"])...)
	if len(values) == 0 {
		return nil
	}
	return values
}

func splitStreamModeQueryValues(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				values = append(values, part)
			}
		}
	}
	return values
}

func (f *streamModeFilter) allow(eventType string) {
	if f.allowed == nil {
		f.allowed = make(map[string]struct{})
	}
	f.allowed[eventType] = struct{}{}
}

func (f streamModeFilter) allows(eventType string) bool {
	if f.allowAll {
		return true
	}
	_, ok := f.allowed[eventType]
	return ok
}

func parseRunConfig(raw map[string]any) runConfig {
	if len(raw) == 0 {
		return runConfig{}
	}

	configurable, _ := raw["configurable"].(map[string]any)
	scope := threadMemoryScopeFromRaw(raw).Merge(threadMemoryScopeFromAny(firstNonNil(configurable["memory_scope"], configurable["memoryScope"])))
	cfg := runConfig{
		ModelName: firstNonEmpty(
			stringFromAny(raw["model_name"]),
			stringFromAny(raw["modelName"]),
			stringFromAny(raw["model"]),
			stringFromAny(configurable["model_name"]),
			stringFromAny(configurable["modelName"]),
			stringFromAny(configurable["model"]),
		),
		Mode: firstNonEmpty(
			stringFromAny(raw["mode"]),
			stringFromAny(configurable["mode"]),
		),
		ReasoningEffort: firstNonEmpty(
			stringFromAny(raw["reasoning_effort"]),
			stringFromAny(raw["reasoningEffort"]),
			stringFromAny(configurable["reasoning_effort"]),
			stringFromAny(configurable["reasoningEffort"]),
		),
		AgentType: agent.AgentType(firstNonEmpty(
			stringFromAny(raw["agent_type"]),
			stringFromAny(raw["agentType"]),
			stringFromAny(configurable["agent_type"]),
			stringFromAny(configurable["agentType"]),
		)),
		AgentName: firstNonEmpty(
			stringFromAny(raw["agent_name"]),
			stringFromAny(raw["agentName"]),
			stringFromAny(configurable["agent_name"]),
			stringFromAny(configurable["agentName"]),
		),
		MemoryUserID: firstNonEmpty(
			stringFromAny(raw["memory_user_id"]),
			stringFromAny(raw["memoryUserId"]),
			stringFromAny(raw["memoryUserID"]),
			stringFromAny(configurable["memory_user_id"]),
			stringFromAny(configurable["memoryUserId"]),
			stringFromAny(configurable["memoryUserID"]),
			scope.UserID,
		),
		MemoryGroupID: firstNonEmpty(
			stringFromAny(raw["memory_group_id"]),
			stringFromAny(raw["memoryGroupId"]),
			stringFromAny(raw["memoryGroupID"]),
			stringFromAny(configurable["memory_group_id"]),
			stringFromAny(configurable["memoryGroupId"]),
			stringFromAny(configurable["memoryGroupID"]),
			scope.GroupID,
		),
		MemoryNamespace: firstNonEmpty(
			stringFromAny(raw["memory_namespace"]),
			stringFromAny(raw["memoryNamespace"]),
			stringFromAny(configurable["memory_namespace"]),
			stringFromAny(configurable["memoryNamespace"]),
			scope.Namespace,
		),
		MaxTurns: intPointerFromAny(firstNonNil(
			raw["recursion_limit"],
			raw["recursionLimit"],
			configurable["recursion_limit"],
			configurable["recursionLimit"],
		)),
		ThinkingEnabled: boolPointerFromAny(firstNonNil(
			raw["thinking_enabled"],
			raw["thinkingEnabled"],
			configurable["thinking_enabled"],
			configurable["thinkingEnabled"],
		)),
		IsPlanMode: boolPointerFromAny(firstNonNil(
			raw["is_plan_mode"],
			raw["isPlanMode"],
			configurable["is_plan_mode"],
			configurable["isPlanMode"],
		)),
		SubagentEnabled: boolPointerFromAny(firstNonNil(
			raw["subagent_enabled"],
			raw["subagentEnabled"],
			configurable["subagent_enabled"],
			configurable["subagentEnabled"],
		)),
		Temperature: floatPointerFromAny(firstNonNil(raw["temperature"], configurable["temperature"])),
		MaxTokens: intPointerFromAny(firstNonNil(
			raw["max_tokens"],
			raw["maxTokens"],
			configurable["max_tokens"],
			configurable["maxTokens"],
		)),
	}
	return cfg
}

func mergeRunConfig(config map[string]any, context map[string]any) map[string]any {
	if len(config) == 0 && len(context) == 0 {
		return nil
	}
	merged := make(map[string]any, len(config)+1)
	for key, value := range config {
		merged[key] = value
	}
	if len(context) == 0 {
		return merged
	}
	configurable, _ := merged["configurable"].(map[string]any)
	if configurable == nil {
		configurable = make(map[string]any, len(context))
	}
	for key, value := range context {
		if _, exists := configurable[key]; exists {
			continue
		}
		configurable[key] = value
	}
	merged["configurable"] = configurable
	return merged
}

func (s *Server) applyRunConfigMetadata(threadID string, cfg runConfig) {
	if cfg.AgentType != "" {
		s.setThreadMetadata(threadID, "agent_type", string(cfg.AgentType))
	}
	if cfg.ModelName != "" {
		s.setThreadMetadata(threadID, "model_name", cfg.ModelName)
	}
	if cfg.Mode != "" {
		s.setThreadMetadata(threadID, "mode", cfg.Mode)
	}
	if cfg.ReasoningEffort != "" {
		s.setThreadMetadata(threadID, "reasoning_effort", cfg.ReasoningEffort)
	}
	if cfg.AgentName != "" {
		s.setThreadMetadata(threadID, "agent_name", cfg.AgentName)
	}
	if cfg.MemoryUserID != "" {
		s.setThreadMetadata(threadID, "memory_user_id", cfg.MemoryUserID)
	}
	if cfg.MemoryGroupID != "" {
		s.setThreadMetadata(threadID, "memory_group_id", cfg.MemoryGroupID)
	}
	if cfg.MemoryNamespace != "" {
		s.setThreadMetadata(threadID, "memory_namespace", cfg.MemoryNamespace)
	}
	if cfg.ThinkingEnabled != nil {
		s.setThreadMetadata(threadID, "thinking_enabled", *cfg.ThinkingEnabled)
	}
	if cfg.IsPlanMode != nil {
		s.setThreadMetadata(threadID, "is_plan_mode", *cfg.IsPlanMode)
	}
	if cfg.SubagentEnabled != nil {
		s.setThreadMetadata(threadID, "subagent_enabled", *cfg.SubagentEnabled)
	}
	if cfg.Temperature != nil {
		s.setThreadMetadata(threadID, "temperature", *cfg.Temperature)
	}
	if cfg.MaxTokens != nil {
		s.setThreadMetadata(threadID, "max_tokens", *cfg.MaxTokens)
	}
	if cfg.MaxTurns != nil {
		s.setThreadMetadata(threadID, "recursion_limit", *cfg.MaxTurns)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func stringFromAny(v any) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func floatPointerFromAny(v any) *float64 {
	switch value := v.(type) {
	case float64:
		out := value
		return &out
	case float32:
		out := float64(value)
		return &out
	case int:
		out := float64(value)
		return &out
	case int64:
		out := float64(value)
		return &out
	case json.Number:
		if parsed, err := value.Float64(); err == nil {
			return &parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			return &parsed
		}
	}
	return nil
}

func intPointerFromAny(v any) *int {
	switch value := v.(type) {
	case int:
		out := value
		return &out
	case int64:
		out := int(value)
		return &out
	case float64:
		out := int(value)
		return &out
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			out := int(parsed)
			return &out
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			return &parsed
		}
	}
	return nil
}

func boolPointerFromAny(v any) *bool {
	switch value := v.(type) {
	case bool:
		out := value
		return &out
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "1", "yes", "on":
			out := true
			return &out
		case "false", "0", "no", "off":
			out := false
			return &out
		}
	}
	return nil
}

func usageMetadataMap(usage *agent.Usage) map[string]any {
	if usage == nil {
		return nil
	}
	return map[string]any{
		"input_tokens":  usage.InputTokens,
		"output_tokens": usage.OutputTokens,
		"total_tokens":  usage.TotalTokens,
	}
}

func toolMessageID(toolCallID string) string {
	if strings.TrimSpace(toolCallID) == "" {
		return ""
	}
	return "tool:" + strings.TrimSpace(toolCallID)
}
