package langgraphcompat

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/google/uuid"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

type runConfig struct {
	ModelName       string
	ReasoningEffort string
	AgentType       agent.AgentType
	AgentName       string
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

func (s *Server) handleRunsStream(w http.ResponseWriter, r *http.Request) {
	s.handleStreamRequest(w, r, "")
}

func (s *Server) handleThreadRunsStream(w http.ResponseWriter, r *http.Request) {
	s.handleStreamRequest(w, r, r.PathValue("thread_id"))
}

func (s *Server) handleThreadRunsCreate(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		http.Error(w, "thread ID required", http.StatusBadRequest)
		return
	}

	var req RunCreateRequest
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
			return
		}
	}
	assistantID := firstNonEmpty(req.AssistantID, req.AssistantIDX)

	session := s.ensureSession(threadID, nil)
	if session.PresentFiles != nil {
		session.PresentFiles.Clear()
	}
	s.markThreadStatus(threadID, "busy")

	input := req.Input
	if input == nil {
		input = make(map[string]any)
	}
	messages, _ := input["messages"].([]any)
	if len(messages) == 0 {
		messages = req.Messages
	}
	newMessages := s.convertToMessages(threadID, messages)

	s.sessionsMu.RLock()
	existingMessages := append([]models.Message(nil), session.Messages...)
	s.sessionsMu.RUnlock()
	deerMessages := append(existingMessages, newMessages...)

	runID := uuid.New().String()
	run := &Run{
		RunID:       runID,
		ThreadID:    threadID,
		AssistantID: assistantID,
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)
	s.setThreadMetadata(threadID, "assistant_id", assistantID)
	s.setThreadMetadata(threadID, "graph_id", firstNonEmpty(assistantID, "lead_agent"))
	s.setThreadMetadata(threadID, "run_id", runID)

	runCfg := parseRunConfig(mergeRunConfig(req.Config, req.Context))
	s.applyRunConfigMetadata(threadID, runCfg)
	runAgent := s.newAgent(agent.AgentConfig{
		PresentFiles:    session.PresentFiles,
		AgentType:       runCfg.AgentType,
		Model:           firstNonEmpty(runCfg.ModelName, s.defaultModel),
		ReasoningEffort: runCfg.ReasoningEffort,
		Temperature:     runCfg.Temperature,
		MaxTokens:       runCfg.MaxTokens,
	})

	ctx := subagent.WithEventSink(r.Context(), func(evt subagent.TaskEvent) {})
	ctx = clarification.WithThreadID(ctx, threadID)
	ctx = clarification.WithEventSink(ctx, func(item *clarification.Clarification) {})

	result, err := runAgent.Run(ctx, threadID, deerMessages)
	if err != nil {
		run.Status = "error"
		run.Error = err.Error()
		run.UpdatedAt = time.Now().UTC()
		s.saveRun(run)
		s.markThreadStatus(threadID, "error")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.saveSession(threadID, result.Messages)
	state := s.getThreadState(threadID)
	run.Status = "success"
	run.UpdatedAt = time.Now().UTC()
	s.saveRun(run)
	s.markThreadStatus(threadID, "idle")

	values := map[string]any{}
	if state != nil {
		for k, v := range state.Values {
			values[k] = v
		}
	}
	values["run_id"] = runID
	values["thread_id"] = threadID
	writeJSON(w, http.StatusOK, values)
}

func (s *Server) handleStreamRequest(w http.ResponseWriter, r *http.Request, routeThreadID string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read body: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req RunCreateRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
			return
		}
	}

	threadID := routeThreadID
	if threadID == "" {
		threadID = firstNonEmpty(req.ThreadID, req.ThreadIDX)
	}
	if threadID == "" {
		threadID = uuid.New().String()
	}
	assistantID := firstNonEmpty(req.AssistantID, req.AssistantIDX)

	session := s.ensureSession(threadID, nil)
	if session.PresentFiles != nil {
		session.PresentFiles.Clear()
	}
	s.markThreadStatus(threadID, "busy")

	input := req.Input
	if input == nil {
		input = make(map[string]any)
	}
	messages, _ := input["messages"].([]any)
	if len(messages) == 0 {
		messages = req.Messages
	}
	newMessages := s.convertToMessages(threadID, messages)

	s.sessionsMu.RLock()
	existingMessages := append([]models.Message(nil), session.Messages...)
	s.sessionsMu.RUnlock()
	deerMessages := append(existingMessages, newMessages...)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	runID := uuid.New().String()
	run := &Run{
		RunID:       runID,
		ThreadID:    threadID,
		AssistantID: assistantID,
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)
	s.setThreadMetadata(threadID, "assistant_id", assistantID)
	s.setThreadMetadata(threadID, "graph_id", firstNonEmpty(assistantID, "lead_agent"))
	s.setThreadMetadata(threadID, "run_id", runID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Content-Location", fmt.Sprintf("/threads/%s/runs/%s", threadID, runID))

	filter := newStreamModeFilter(firstNonNil(req.StreamMode, req.StreamModeX))
	s.recordAndSendEventFiltered(w, flusher, run, filter, "metadata", map[string]any{
		"run_id":    runID,
		"thread_id": threadID,
	})

	runCfg := parseRunConfig(mergeRunConfig(req.Config, req.Context))
	s.applyRunConfigMetadata(threadID, runCfg)
	runAgent := s.newAgent(agent.AgentConfig{
		PresentFiles:    session.PresentFiles,
		AgentType:       runCfg.AgentType,
		Model:           firstNonEmpty(runCfg.ModelName, s.defaultModel),
		ReasoningEffort: runCfg.ReasoningEffort,
		Temperature:     runCfg.Temperature,
		MaxTokens:       runCfg.MaxTokens,
	})

	ctx := subagent.WithEventSink(r.Context(), func(evt subagent.TaskEvent) {
		s.forwardTaskEvent(w, flusher, run, filter, evt)
	})
	ctx = clarification.WithThreadID(ctx, threadID)
	ctx = clarification.WithEventSink(ctx, func(item *clarification.Clarification) {
		if item == nil {
			return
		}
		s.recordAndSendEventFiltered(w, flusher, run, filter, "clarification_request", item)
	})
	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		for evt := range runAgent.Events() {
			s.forwardAgentEvent(w, flusher, run, filter, evt)
		}
	}()

	result, err := runAgent.Run(ctx, threadID, deerMessages)
	<-eventsDone
	if err != nil {
		run.Status = "error"
		run.Error = err.Error()
		run.UpdatedAt = time.Now().UTC()
		s.saveRun(run)
		s.markThreadStatus(threadID, "error")
		return
	}

	s.saveSession(threadID, result.Messages)
	state := s.getThreadState(threadID)
	s.emitFinalMessagesTuple(w, flusher, run, filter, existingMessages, result.Messages, result.Usage)

	s.recordAndSendEventFiltered(w, flusher, run, filter, "updates", map[string]any{
		"agent": map[string]any{
			"messages":  state.Values["messages"],
			"title":     state.Values["title"],
			"artifacts": state.Values["artifacts"],
		},
	})
	s.recordAndSendEventFiltered(w, flusher, run, filter, "values", state.Values)
	s.recordAndSendEventFiltered(w, flusher, run, filter, "end", map[string]any{
		"run_id": runID,
		"usage":  result.Usage,
	})

	run.Status = "success"
	run.UpdatedAt = time.Now().UTC()
	s.saveRun(run)
	s.markThreadStatus(threadID, "idle")
}

func (s *Server) handleRunStream(w http.ResponseWriter, r *http.Request) {
	s.streamRecordedRun(w, r, "", r.PathValue("run_id"))
}

func (s *Server) handleThreadRunStream(w http.ResponseWriter, r *http.Request) {
	s.streamRecordedRun(w, r, r.PathValue("thread_id"), r.PathValue("run_id"))
}

func (s *Server) handleThreadJoinStream(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	run := s.getLatestRunForThread(threadID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	if run == nil {
		fmt.Fprint(w, ": no active run\n\n")
		flusher.Flush()
		return
	}

	filter := newStreamModeFilter(streamModeFromQuery(r))
	for _, event := range run.Events {
		if !filter.allows(event.Event) {
			continue
		}
		s.sendSSEEvent(w, flusher, event)
	}
	flusher.Flush()
}

func (s *Server) streamRecordedRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	run := s.getRun(runID)
	if run == nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	if threadID != "" && run.ThreadID != threadID {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Content-Location", fmt.Sprintf("/threads/%s/runs/%s", run.ThreadID, run.RunID))

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	filter := newStreamModeFilter(streamModeFromQuery(r))
	for _, event := range run.Events {
		if !filter.allows(event.Event) {
			continue
		}
		s.sendSSEEvent(w, flusher, event)
	}
}

func (s *Server) convertToMessages(threadID string, input []any) []models.Message {
	messages := make([]models.Message, 0, len(input))
	msgSeq := uint64(time.Now().UnixNano())

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
		if s.convertRole(role) == models.RoleHuman {
			content = s.injectUploadedFilesContext(threadID, msgMap, content)
		}
		if role == "" || content == "" {
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
		messages = append(messages, msg)
	}

	return messages
}

func (s *Server) injectUploadedFilesContext(threadID string, message map[string]any, content string) string {
	newFiles := extractMessageFiles(message)
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

func extractMessageFiles(message map[string]any) []map[string]any {
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
		path := firstNonEmpty(stringFromAny(item["path"]), "/mnt/user-data/uploads/"+filename)
		files = append(files, map[string]any{
			"filename": filename,
			"size":     toInt64(item["size"]),
			"path":     path,
		})
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
	return []string{
		fmt.Sprintf("- %s (%s)", filename, sizeLabel),
		fmt.Sprintf("  Path: %s", path),
		"",
	}
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

func (s *Server) recordAndSendEvent(w http.ResponseWriter, flusher http.Flusher, run *Run, eventType string, data any) {
	event := StreamEvent{
		ID:       fmt.Sprintf("%s:%d", run.RunID, s.nextRunEventIndex(run.RunID)),
		Event:    eventType,
		Data:     data,
		RunID:    run.RunID,
		ThreadID: run.ThreadID,
	}
	s.appendRunEvent(run.RunID, event)
	s.sendSSEEvent(w, flusher, event)
}

func (s *Server) recordAndSendEventFiltered(w http.ResponseWriter, flusher http.Flusher, run *Run, filter streamModeFilter, eventType string, data any) {
	event := StreamEvent{
		ID:       fmt.Sprintf("%s:%d", run.RunID, s.nextRunEventIndex(run.RunID)),
		Event:    eventType,
		Data:     data,
		RunID:    run.RunID,
		ThreadID: run.ThreadID,
	}
	s.appendRunEvent(run.RunID, event)
	if filter.allows(eventType) {
		s.sendSSEEvent(w, flusher, event)
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

func (s *Server) forwardAgentEvent(w http.ResponseWriter, flusher http.Flusher, run *Run, filter streamModeFilter, evt agent.AgentEvent) {
	switch evt.Type {
	case agent.AgentEventChunk:
		chunkData := map[string]any{
			"run_id":    run.RunID,
			"thread_id": run.ThreadID,
			"type":      "ai",
			"role":      "assistant",
			"delta":     evt.Text,
			"content":   evt.Text,
		}
		s.recordAndSendEventFiltered(w, flusher, run, filter, "chunk", chunkData)
		s.recordAndSendEventFiltered(w, flusher, run, filter, "messages-tuple", Message{
			Type:    "ai",
			Role:    "assistant",
			Content: evt.Text,
		})
	case agent.AgentEventToolCall:
		if evt.ToolEvent == nil {
			return
		}
		s.recordAndSendEventFiltered(w, flusher, run, filter, "tool_call", evt.ToolEvent)
	case agent.AgentEventToolCallStart:
		if evt.ToolEvent == nil {
			return
		}
		s.recordAndSendEventFiltered(w, flusher, run, filter, "tool_call_start", evt.ToolEvent)
	case agent.AgentEventToolCallEnd:
		if evt.ToolEvent == nil {
			return
		}
		s.recordAndSendEventFiltered(w, flusher, run, filter, "tool_call_end", evt.ToolEvent)
		s.recordAndSendEventFiltered(w, flusher, run, filter, "on_tool_end", map[string]any{
			"event": "on_tool_end",
			"name":  evt.ToolEvent.Name,
			"data":  evt.ToolEvent,
		})
		s.recordAndSendEventFiltered(w, flusher, run, filter, "messages-tuple", Message{
			Type:       "tool",
			ID:         toolMessageID(evt.ToolEvent.ID),
			Role:       "tool",
			Name:       evt.ToolEvent.Name,
			Content:    evt.ToolEvent.ResultPreview,
			ToolCallID: evt.ToolEvent.ID,
			Data: map[string]any{
				"status":         evt.ToolEvent.Status,
				"arguments":      evt.ToolEvent.Arguments,
				"arguments_text": evt.ToolEvent.ArgumentsText,
				"error":          evt.ToolEvent.Error,
			},
		})
	case agent.AgentEventError:
		errData := map[string]any{
			"error":   "RunError",
			"name":    "RunError",
			"message": evt.Err,
		}
		if evt.Error != nil {
			errData["code"] = evt.Error.Code
			errData["suggestion"] = evt.Error.Suggestion
			errData["retryable"] = evt.Error.Retryable
		}
		s.recordAndSendEventFiltered(w, flusher, run, filter, "error", errData)
	}
}

func (s *Server) emitFinalMessagesTuple(w http.ResponseWriter, flusher http.Flusher, run *Run, filter streamModeFilter, existingMessages []models.Message, resultMessages []models.Message, usage *agent.Usage) {
	start := len(existingMessages)
	if start > len(resultMessages) {
		start = 0
	}
	finalMessages := s.messagesToLangChain(resultMessages[start:])
	for i := range finalMessages {
		if finalMessages[i].Type == "ai" && usage != nil {
			finalMessages[i].UsageMetadata = usageMetadataMap(usage)
		}
		s.recordAndSendEventFiltered(w, flusher, run, filter, "messages-tuple", finalMessages[i])
	}
}

func (s *Server) forwardTaskEvent(w http.ResponseWriter, flusher http.Flusher, run *Run, filter streamModeFilter, evt subagent.TaskEvent) {
	data := map[string]any{
		"type":        evt.Type,
		"task_id":     evt.TaskID,
		"description": evt.Description,
		"message":     evt.Message,
		"result":      evt.Result,
		"error":       evt.Error,
	}
	s.recordAndSendEventFiltered(w, flusher, run, filter, evt.Type, data)
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
	cfg := runConfig{
		ModelName: firstNonEmpty(
			stringFromAny(raw["model_name"]),
			stringFromAny(raw["modelName"]),
			stringFromAny(raw["model"]),
			stringFromAny(configurable["model_name"]),
			stringFromAny(configurable["modelName"]),
			stringFromAny(configurable["model"]),
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
	if cfg.ReasoningEffort != "" {
		s.setThreadMetadata(threadID, "reasoning_effort", cfg.ReasoningEffort)
	}
	if cfg.AgentName != "" {
		s.setThreadMetadata(threadID, "agent_name", cfg.AgentName)
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
