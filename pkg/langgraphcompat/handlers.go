package langgraphcompat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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
	IsBootstrap     bool
	SystemPrompt    string
	Tools           *tools.Registry
	Temperature     *float64
	MaxTokens       *int
}

const planModeTodoPrompt = `When the task is multi-step or likely to take several actions, maintain a concise todo list with the write_todos tool.
Write the initial todo list early, keep exactly one item in_progress when work is active, update statuses immediately after progress, and clear or complete the list when finished.`

const bootstrapAgentPrompt = `You are helping the user create a brand-new custom agent.
Focus on clarifying the agent's purpose, behavior, tool needs, and boundaries.
When you have enough information, call the setup_agent tool exactly once to save the agent's description and full SOUL content.`

const fallbackSkillCreatorPrompt = `You are operating in skill creation mode.
Help the user design a new SKILL.md workflow, ask targeted questions about trigger conditions and outputs, then draft or refine the skill instructions.
If the user is iterating on an existing skill, help compare the current draft against the intended behavior and improve it step by step.`

func (s *Server) handleRunsStream(w http.ResponseWriter, r *http.Request) {
	s.handleStreamRequest(w, r, "")
}

func (s *Server) handleRunsCreate(w http.ResponseWriter, r *http.Request) {
	s.handleRunCreateRequest(w, r, "")
}

func (s *Server) handleThreadRunsStream(w http.ResponseWriter, r *http.Request) {
	s.handleStreamRequest(w, r, r.PathValue("thread_id"))
}

func (s *Server) handleThreadRunsCreate(w http.ResponseWriter, r *http.Request) {
	s.handleRunCreateRequest(w, r, r.PathValue("thread_id"))
}

func (s *Server) handleStreamRequest(w http.ResponseWriter, r *http.Request, routeThreadID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	req, err := parseRunRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	run, _, execErr, statusCode := s.executeRun(r.Context(), req, routeThreadID, w, flusher)
	if run != nil {
		w.Header().Set("Content-Location", fmt.Sprintf("/threads/%s/runs/%s", run.ThreadID, run.RunID))
	}
	if execErr != nil && statusCode != 0 && run == nil {
		http.Error(w, execErr.Error(), statusCode)
	}
}

func (s *Server) handleRunCreateRequest(w http.ResponseWriter, r *http.Request, routeThreadID string) {
	req, err := parseRunRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	run, _, execErr, statusCode := s.executeRun(r.Context(), req, routeThreadID, nil, nil)
	if execErr != nil {
		http.Error(w, execErr.Error(), statusCode)
		return
	}
	writeJSON(w, http.StatusOK, s.runResponse(run))
}

func parseRunRequest(r *http.Request) (RunCreateRequest, error) {
	var req RunCreateRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return RunCreateRequest{}, fmt.Errorf("failed to read body: %v", err)
	}
	defer r.Body.Close()
	if len(body) == 0 {
		return req, nil
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return RunCreateRequest{}, fmt.Errorf("invalid request: %v", err)
	}
	return req, nil
}

func (s *Server) executeRun(ctx context.Context, req RunCreateRequest, routeThreadID string, w http.ResponseWriter, flusher http.Flusher) (*Run, *ThreadState, error, int) {
	threadID := routeThreadID
	if threadID == "" {
		threadID = req.ThreadID
	}
	if threadID == "" {
		threadID = uuid.New().String()
	}
	if err := validateThreadID(threadID); err != nil {
		return nil, nil, err, http.StatusBadRequest
	}

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
	newMessages := s.convertToMessages(threadID, messages)

	runtimeContext := runtimeContextFromRequest(req)
	runCfg := parseRunConfig(req.Config)
	runCfg.IsBootstrap = runCfg.IsBootstrap || boolFromAny(runtimeContext["is_bootstrap"])
	if runCfg.IsBootstrap && strings.TrimSpace(stringFromAny(runtimeContext["agent_name"])) == "" {
		if inferred := inferBootstrapAgentName(newMessages); inferred != "" {
			runtimeContext["agent_name"] = inferred
		}
	}
	resolvedRunCfg, err := s.resolveRunConfig(runCfg, runtimeContext)
	if err != nil {
		return nil, nil, err, http.StatusNotFound
	}
	s.setThreadConfig(threadID, threadConfigFromRuntimeContext(threadID, runtimeContext, resolvedRunCfg))
	resolvedRunCfg.SystemPrompt = joinPromptSections(resolvedRunCfg.SystemPrompt, s.memoryInjectionPrompt(ctx, threadID))

	s.sessionsMu.RLock()
	existingMessages := append([]models.Message(nil), session.Messages...)
	s.sessionsMu.RUnlock()
	historySummary := s.threadHistorySummary(threadID)
	compactedExisting := s.compactConversationHistory(ctx, threadID, resolvedRunCfg.ModelName, historySummary, existingMessages)
	if compactedExisting.Changed {
		existingMessages = compactedExisting.Messages
		historySummary = compactedExisting.Summary
		s.setThreadHistorySummary(threadID, historySummary)
	}
	deerMessages := append(existingMessages, newMessages...)
	if strings.TrimSpace(historySummary) != "" {
		deerMessages = append([]models.Message{conversationSummaryMessage(threadID, historySummary)}, deerMessages...)
	}
	deerMessages = injectTodoReminder(threadID, deerMessages, session.Todos)

	run := &Run{
		RunID:       uuid.New().String(),
		ThreadID:    threadID,
		AssistantID: req.AssistantID,
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)
	if w != nil {
		w.Header().Set("Content-Location", fmt.Sprintf("/threads/%s/runs/%s", threadID, run.RunID))
	}
	s.recordAndSendEvent(w, flusher, run, "metadata", map[string]any{
		"run_id":    run.RunID,
		"thread_id": threadID,
	})

	if resolvedRunCfg.AgentType != "" {
		s.setThreadMetadata(threadID, "agent_type", string(resolvedRunCfg.AgentType))
	}
	runAgent := s.newAgent(agent.AgentConfig{
		Tools:           resolvedRunCfg.Tools,
		PresentFiles:    session.PresentFiles,
		AgentType:       resolvedRunCfg.AgentType,
		Model:           firstNonEmpty(resolvedRunCfg.ModelName, s.defaultModel),
		ReasoningEffort: resolvedRunCfg.ReasoningEffort,
		SystemPrompt:    resolvedRunCfg.SystemPrompt,
		Temperature:     resolvedRunCfg.Temperature,
		MaxTokens:       resolvedRunCfg.MaxTokens,
	})

	ctx = subagent.WithEventSink(ctx, func(evt subagent.TaskEvent) {
		s.forwardTaskEvent(w, flusher, run, evt)
	})
	ctx = tools.WithThreadID(ctx, threadID)
	ctx = tools.WithRuntimeContext(ctx, runtimeContext)
	ctx = clarification.WithThreadID(ctx, threadID)
	ctx = clarification.WithEventSink(ctx, func(item *clarification.Clarification) {
		if item == nil {
			return
		}
		s.recordAndSendEvent(w, flusher, run, "clarification_request", item)
	})
	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		for evt := range runAgent.Events() {
			s.forwardAgentEvent(w, flusher, run, evt)
		}
	}()

	result, err := runAgent.Run(ctx, threadID, deerMessages)
	<-eventsDone
	if err != nil {
		s.recordAndSendEvent(w, flusher, run, "error", map[string]any{
			"error":   "RunError",
			"name":    "RunError",
			"message": err.Error(),
		})
		run.Status = "error"
		run.Error = err.Error()
		run.UpdatedAt = time.Now().UTC()
		s.saveRun(run)
		s.markThreadStatus(threadID, "error")
		return run, nil, err, http.StatusInternalServerError
	}

	storedMessages := filterTransientMessages(result.Messages)
	compactedStored := s.compactConversationHistory(ctx, threadID, resolvedRunCfg.ModelName, historySummary, storedMessages)
	if compactedStored.Changed {
		storedMessages = compactedStored.Messages
		historySummary = compactedStored.Summary
	}
	s.setThreadHistorySummary(threadID, historySummary)
	s.saveSession(threadID, storedMessages)
	s.scheduleMemoryUpdate(threadID, storedMessages)
	s.maybeGenerateThreadTitle(ctx, threadID, resolvedRunCfg.ModelName, storedMessages)
	state := s.getThreadState(threadID)
	if state != nil {
		s.recordAndSendEvent(w, flusher, run, "updates", map[string]any{
			"agent": map[string]any{
				"messages":  state.Values["messages"],
				"title":     state.Values["title"],
				"artifacts": state.Values["artifacts"],
				"todos":     state.Values["todos"],
			},
		})
		s.recordAndSendEvent(w, flusher, run, "values", state.Values)
	}
	s.recordAndSendEvent(w, flusher, run, "end", map[string]any{
		"run_id": run.RunID,
		"usage":  result.Usage,
	})

	run.Status = "success"
	run.UpdatedAt = time.Now().UTC()
	s.saveRun(run)
	s.markThreadStatus(threadID, "idle")
	return run, state, nil, http.StatusOK
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

	s.streamRunEvents(w, r, flusher, run.RunID)
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

	s.streamRunEvents(w, r, flusher, runID)
}

func (s *Server) streamRunEvents(w http.ResponseWriter, r *http.Request, flusher http.Flusher, runID string) {
	run, stream := s.subscribeRun(runID)
	if run == nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	if stream != nil {
		defer s.unsubscribeRun(runID, stream)
	}

	for _, event := range run.Events {
		s.sendSSEEvent(w, flusher, event)
	}
	if stream == nil {
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-stream:
			if !ok {
				return
			}
			s.sendSSEEvent(w, flusher, event)
			if event.Event == "end" || event.Event == "error" {
				return
			}
		}
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

		content, multiContent := extractMessageContent(msgMap["content"])
		if role == "" || content == "" {
			continue
		}

		additionalKwargs, _ := msgMap["additional_kwargs"].(map[string]any)
		files := extractUploadedFiles(additionalKwargs, s.uploadsDir(threadID))
		if len(files) > 0 || hasHistoricalUploads(s.uploadsDir(threadID), files) {
			historical := listHistoricalUploads(s.uploadsDir(threadID), files)
			uploadsBlock := buildUploadedFilesBlock(files, historical)
			content = injectUploadedFilesBlock(content, files, historical)
			multiContent = prependTextPart(multiContent, uploadsBlock)
		}

		msgSeq++
		msg := models.Message{
			ID:        fmt.Sprintf("msg_%d", msgSeq),
			SessionID: threadID,
			Role:      s.convertRole(role),
			Content:   content,
		}
		metadata := map[string]string{}
		if len(additionalKwargs) > 0 {
			if raw, err := json.Marshal(additionalKwargs); err == nil {
				metadata["additional_kwargs"] = string(raw)
			}
		}
		if len(multiContent) > 0 {
			if raw, err := json.Marshal(multiContent); err == nil {
				metadata["multi_content"] = string(raw)
			}
		}
		if len(metadata) > 0 {
			msg.Metadata = metadata
		}
		messages = append(messages, msg)
	}

	return messages
}

type uploadedFile struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Path     string `json:"path"`
}

func extractUploadedFiles(additionalKwargs map[string]any, uploadDir string) []uploadedFile {
	if len(additionalKwargs) == 0 {
		return nil
	}
	rawFiles, _ := additionalKwargs["files"].([]any)
	files := make([]uploadedFile, 0, len(rawFiles))
	for _, raw := range rawFiles {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		filename := sanitizeFilename(stringFromAny(item["filename"]))
		if filename == "" {
			continue
		}
		if strings.TrimSpace(uploadDir) != "" {
			info, err := os.Stat(filepath.Join(uploadDir, filename))
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
		}
		files = append(files, uploadedFile{
			Filename: filename,
			Size:     int64FromAny(item["size"]),
			Path:     "/mnt/user-data/uploads/" + filepath.ToSlash(filename),
		})
	}
	return files
}

func hasHistoricalUploads(uploadDir string, current []uploadedFile) bool {
	return len(listHistoricalUploads(uploadDir, current)) > 0
}

func listHistoricalUploads(uploadDir string, current []uploadedFile) []uploadedFile {
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		return nil
	}
	currentNames := make(map[string]struct{}, len(current))
	for _, file := range current {
		currentNames[file.Filename] = struct{}{}
	}
	files := make([]uploadedFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := sanitizeFilename(entry.Name())
		if name == "" {
			continue
		}
		if _, exists := currentNames[name]; exists {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, uploadedFile{
			Filename: name,
			Size:     info.Size(),
			Path:     "/mnt/user-data/uploads/" + filepath.ToSlash(name),
		})
	}
	return files
}

func injectUploadedFilesBlock(content string, current []uploadedFile, historical []uploadedFile) string {
	block := buildUploadedFilesBlock(current, historical)
	content = strings.TrimSpace(content)
	if content == "" {
		return block
	}
	return block + "\n\n" + content
}

func buildUploadedFilesBlock(current []uploadedFile, historical []uploadedFile) string {
	var b strings.Builder
	b.WriteString("<uploaded_files>\n")
	b.WriteString("The following files were uploaded in this message:\n\n")
	if len(current) == 0 {
		b.WriteString("(empty)\n")
	} else {
		for _, file := range current {
			b.WriteString("- ")
			b.WriteString(file.Filename)
			b.WriteString(" (")
			b.WriteString(formatUploadSize(file.Size))
			b.WriteString(")\n")
			b.WriteString("  Path: ")
			b.WriteString(file.Path)
			b.WriteString("\n\n")
		}
	}
	if len(historical) > 0 {
		b.WriteString("The following files were uploaded in previous messages and are still available:\n\n")
		for _, file := range historical {
			b.WriteString("- ")
			b.WriteString(file.Filename)
			b.WriteString(" (")
			b.WriteString(formatUploadSize(file.Size))
			b.WriteString(")\n")
			b.WriteString("  Path: ")
			b.WriteString(file.Path)
			b.WriteString("\n\n")
		}
	}
	b.WriteString("You can read these files using the `read_file` tool with the paths shown above.\n")
	b.WriteString("</uploaded_files>")
	return b.String()
}

func formatUploadSize(size int64) string {
	if size < 0 {
		size = 0
	}
	kb := float64(size) / 1024
	if kb < 1024 {
		return fmt.Sprintf("%.1f KB", kb)
	}
	return fmt.Sprintf("%.1f MB", kb/1024)
}

func int64FromAny(v any) int64 {
	switch value := v.(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

func extractMessageContent(raw any) (string, []map[string]any) {
	switch v := raw.(type) {
	case string:
		return v, nil
	case []any:
		parts := make([]string, 0, len(v))
		multiContent := make([]map[string]any, 0, len(v))
		for _, item := range v {
			part, ok := item.(map[string]any)
			if !ok {
				continue
			}
			partType, _ := part["type"].(string)
			switch partType {
			case "text":
				if text, _ := part["text"].(string); text != "" {
					parts = append(parts, text)
					multiContent = append(multiContent, map[string]any{
						"type": "text",
						"text": text,
					})
				}
			case "image_url":
				imageURL, _ := part["image_url"].(map[string]any)
				url := stringFromAny(imageURL["url"])
				if strings.TrimSpace(url) == "" {
					continue
				}
				multiContent = append(multiContent, map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url": url,
					},
				})
			}
		}
		return strings.Join(parts, "\n"), multiContent
	default:
		return "", nil
	}
}

func prependTextPart(parts []map[string]any, text string) []map[string]any {
	text = strings.TrimSpace(text)
	if text == "" {
		return parts
	}
	out := make([]map[string]any, 0, len(parts)+1)
	out = append(out, map[string]any{
		"type": "text",
		"text": text,
	})
	out = append(out, parts...)
	return out
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
	if w != nil && flusher != nil {
		s.sendSSEEvent(w, flusher, event)
	}
}

func (s *Server) saveSession(threadID string, messages []models.Message) {
	s.sessionsMu.Lock()
	var snapshot *Session
	if session, exists := s.sessions[threadID]; exists {
		session.Messages = append([]models.Message(nil), messages...)
		session.Status = "idle"
		session.UpdatedAt = time.Now().UTC()
		snapshot = cloneSession(session)
	} else {
		session := &Session{
			ThreadID:     threadID,
			Messages:     append([]models.Message(nil), messages...),
			Todos:        nil,
			Metadata:     make(map[string]any),
			Status:       "idle",
			PresentFiles: tools.NewPresentFileRegistry(),
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		s.sessions[threadID] = session
		snapshot = cloneSession(session)
	}
	s.sessionsMu.Unlock()
	_ = s.persistSessionSnapshot(snapshot)
}

func (s *Server) forwardAgentEvent(w http.ResponseWriter, flusher http.Flusher, run *Run, evt agent.AgentEvent) {
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
		s.recordAndSendEvent(w, flusher, run, "chunk", chunkData)
		s.recordAndSendEvent(w, flusher, run, "messages-tuple", Message{
			Type:    "ai",
			ID:      evt.MessageID,
			Role:    "assistant",
			Content: evt.Text,
		})
	case agent.AgentEventToolCall:
		if evt.ToolEvent == nil || evt.ToolCall == nil {
			return
		}
		s.recordAndSendEvent(w, flusher, run, "tool_call", evt.ToolEvent)
		s.recordAndSendEvent(w, flusher, run, "messages-tuple", Message{
			Type: "ai",
			ID:   evt.MessageID,
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:   evt.ToolCall.ID,
				Name: evt.ToolCall.Name,
				Args: cloneToolArguments(evt.ToolCall.Arguments),
			}},
		})
	case agent.AgentEventToolCallStart:
		if evt.ToolEvent == nil {
			return
		}
		s.recordAndSendEvent(w, flusher, run, "tool_call_start", evt.ToolEvent)
		s.recordAndSendEvent(w, flusher, run, "events", langChainToolEvent("on_tool_start", run, evt))
	case agent.AgentEventToolCallEnd:
		if evt.ToolEvent == nil {
			return
		}
		s.recordAndSendEvent(w, flusher, run, "tool_call_end", evt.ToolEvent)
		s.recordAndSendEvent(w, flusher, run, "events", langChainToolEvent("on_tool_end", run, evt))
		content := ""
		if evt.Result != nil {
			content = evt.Result.Content
			if content == "" {
				content = evt.Result.Error
			}
		}
		s.recordAndSendEvent(w, flusher, run, "messages-tuple", Message{
			Type:       "tool",
			ID:         evt.MessageID,
			Role:       "tool",
			Name:       evt.ToolEvent.Name,
			Content:    content,
			ToolCallID: evt.ToolEvent.ID,
			Data: map[string]any{
				"status":         evt.ToolEvent.Status,
				"arguments":      evt.ToolEvent.Arguments,
				"arguments_text": evt.ToolEvent.ArgumentsText,
				"error":          evt.ToolEvent.Error,
			},
		})
		if evt.Result != nil && evt.Result.ToolName == "write_todos" {
			state := s.getThreadState(run.ThreadID)
			if state != nil {
				s.recordAndSendEvent(w, flusher, run, "updates", map[string]any{
					"agent": map[string]any{
						"todos": state.Values["todos"],
					},
				})
			}
		}
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
		s.recordAndSendEvent(w, flusher, run, "error", errData)
	}
}

func langChainToolEvent(eventName string, run *Run, evt agent.AgentEvent) map[string]any {
	payload := map[string]any{
		"event": eventName,
	}
	if evt.ToolEvent != nil {
		payload["name"] = evt.ToolEvent.Name
		payload["data"] = evt.ToolEvent
	}
	if evt.MessageID != "" {
		payload["message_id"] = evt.MessageID
	}
	if run != nil && run.RunID != "" {
		payload["run_id"] = run.RunID
	}
	if run != nil && run.ThreadID != "" {
		payload["thread_id"] = run.ThreadID
	}
	return payload
}

func (s *Server) forwardTaskEvent(w http.ResponseWriter, flusher http.Flusher, run *Run, evt subagent.TaskEvent) {
	data := map[string]any{
		"type":        evt.Type,
		"task_id":     evt.TaskID,
		"description": evt.Description,
		"message":     evt.Message,
		"result":      evt.Result,
		"error":       evt.Error,
	}
	s.recordAndSendEvent(w, flusher, run, evt.Type, data)
}

func parseRunConfig(raw map[string]any) runConfig {
	if len(raw) == 0 {
		return runConfig{}
	}

	configurable, _ := raw["configurable"].(map[string]any)
	cfg := runConfig{
		ModelName:       firstNonEmpty(stringFromAny(raw["model_name"]), stringFromAny(raw["model"]), stringFromAny(configurable["model_name"]), stringFromAny(configurable["model"])),
		ReasoningEffort: firstNonEmpty(stringFromAny(raw["reasoning_effort"]), stringFromAny(configurable["reasoning_effort"])),
		AgentType:       agent.AgentType(firstNonEmpty(stringFromAny(raw["agent_type"]), stringFromAny(configurable["agent_type"]))),
		AgentName:       firstNonEmpty(stringFromAny(raw["agent_name"]), stringFromAny(configurable["agent_name"])),
		IsBootstrap:     boolFromAny(firstNonNil(raw["is_bootstrap"], configurable["is_bootstrap"])),
		Temperature:     floatPointerFromAny(firstNonNil(raw["temperature"], configurable["temperature"])),
		MaxTokens:       intPointerFromAny(firstNonNil(raw["max_tokens"], configurable["max_tokens"])),
	}
	return cfg
}

func (s *Server) resolveRunConfig(cfg runConfig, runtimeContext map[string]any) (runConfig, error) {
	cfg.IsBootstrap = cfg.IsBootstrap || boolFromAny(runtimeContext["is_bootstrap"])
	cfg.AgentName = firstNonEmpty(stringFromAny(runtimeContext["agent_name"]), cfg.AgentName)
	basePrompt := strings.TrimSpace(cfg.SystemPrompt)
	if basePrompt == "" {
		basePrompt = agent.GetAgentTypeConfig(cfg.AgentType).SystemPrompt
	}
	cfg.SystemPrompt = joinPromptSections(
		basePrompt,
		s.userProfilePromptSection(),
		s.runtimeModePrompt(runtimeContext),
		s.environmentPrompt(),
	)
	if boolFromAny(runtimeContext["is_plan_mode"]) {
		cfg.SystemPrompt = joinPromptSections(cfg.SystemPrompt, planModeTodoPrompt)
	}
	if cfg.IsBootstrap {
		if cfg.AgentName != "" {
			name, ok := normalizeAgentName(cfg.AgentName)
			if !ok {
				return runConfig{}, fmt.Errorf("invalid agent name")
			}
			cfg.AgentName = name
		}
		cfg.SystemPrompt = joinPromptSections(cfg.SystemPrompt, bootstrapAgentPrompt)
		cfg.Tools = resolveRuntimeToolRegistry(s.tools, runtimeContext)
		cfg.Tools = s.resolveModelToolRegistry(cfg.Tools, firstNonEmpty(cfg.ModelName, s.defaultModel))
		return cfg, nil
	}
	if cfg.AgentName == "" {
		if cfg.Tools == nil {
			cfg.Tools = s.tools
		}
		cfg.Tools = resolveRuntimeToolRegistry(cfg.Tools, runtimeContext)
		cfg.Tools = s.resolveModelToolRegistry(cfg.Tools, firstNonEmpty(cfg.ModelName, s.defaultModel))
		return cfg, nil
	}

	name, ok := normalizeAgentName(cfg.AgentName)
	if !ok {
		return runConfig{}, fmt.Errorf("invalid agent name")
	}

	s.uiStateMu.RLock()
	customAgent, exists := s.getAgentsLocked()[name]
	s.uiStateMu.RUnlock()
	if !exists {
		return runConfig{}, fmt.Errorf("agent %q not found", name)
	}

	cfg.AgentName = name
	if cfg.ModelName == "" && customAgent.Model != nil {
		cfg.ModelName = strings.TrimSpace(*customAgent.Model)
	}

	customPrompt := buildCustomAgentPrompt(customAgent)
	cfg.SystemPrompt = joinPromptSections(cfg.SystemPrompt, customPrompt)

	if len(customAgent.ToolGroups) > 0 {
		cfg.Tools = resolveAgentToolRegistry(s.tools, customAgent.ToolGroups)
	}
	if cfg.Tools == nil {
		cfg.Tools = s.tools
	}
	cfg.Tools = resolveRuntimeToolRegistry(cfg.Tools, runtimeContext)
	cfg.Tools = s.resolveModelToolRegistry(cfg.Tools, firstNonEmpty(cfg.ModelName, s.defaultModel))
	return cfg, nil
}

func (s *Server) runtimeModePrompt(runtimeContext map[string]any) string {
	mode := strings.ToLower(strings.TrimSpace(stringFromAny(runtimeContext["mode"])))
	switch mode {
	case "skill":
		if body, ok := s.loadGatewaySkillBody("skill-creator", skillCategoryPublic); ok {
			return "Loaded skill instructions (skill-creator):\n" + body
		}
		if body, ok := s.loadGatewaySkillBody("skill-creator", ""); ok {
			return "Loaded skill instructions (skill-creator):\n" + body
		}
		return fallbackSkillCreatorPrompt
	default:
		return ""
	}
}

func buildCustomAgentPrompt(customAgent gatewayAgent) string {
	parts := make([]string, 0, 2)
	if desc := strings.TrimSpace(customAgent.Description); desc != "" {
		parts = append(parts, "Agent description:\n"+desc)
	}
	if soul := strings.TrimSpace(customAgent.Soul); soul != "" {
		parts = append(parts, "SOUL.md:\n"+soul)
	}
	return strings.Join(parts, "\n\n")
}

func (s *Server) userProfilePrompt() string {
	if s == nil {
		return ""
	}

	s.uiStateMu.RLock()
	defer s.uiStateMu.RUnlock()
	return strings.TrimSpace(s.getUserProfileLocked())
}

func (s *Server) userProfilePromptSection() string {
	if profile := strings.TrimSpace(s.userProfilePrompt()); profile != "" {
		return "USER.md:\n" + profile
	}
	return ""
}

func joinPromptSections(parts ...string) string {
	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			trimmed = append(trimmed, part)
		}
	}
	return strings.Join(trimmed, "\n\n")
}

func (s *Server) memoryInjectionPrompt(ctx context.Context, threadID string) string {
	if s == nil || s.memorySvc == nil || strings.TrimSpace(threadID) == "" {
		return ""
	}
	return strings.TrimSpace(s.memorySvc.Inject(ctx, threadID))
}

func filterTransientMessages(messages []models.Message) []models.Message {
	if len(messages) == 0 {
		return nil
	}
	filtered := make([]models.Message, 0, len(messages))
	for _, msg := range messages {
		if isTransientViewedImagesMessage(msg) || isInjectedSummaryMessage(msg) || isTransientTodoReminderMessage(msg) {
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered
}

func isTransientViewedImagesMessage(msg models.Message) bool {
	if msg.Role != models.RoleHuman || len(msg.Metadata) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(msg.Metadata["transient_viewed_images"]), "true")
}

func isTransientTodoReminderMessage(msg models.Message) bool {
	if msg.Role != models.RoleHuman || len(msg.Metadata) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(msg.Metadata[transientTodoReminderMetadataKey]), "true")
}

func (s *Server) scheduleMemoryUpdate(threadID string, messages []models.Message) {
	if s == nil || s.memorySvc == nil || len(messages) == 0 || strings.TrimSpace(threadID) == "" {
		return
	}
	cloned := append([]models.Message(nil), messages...)
	go func() {
		if err := s.memorySvc.Update(context.Background(), threadID, cloned); err != nil {
			if s.logger != nil {
				s.logger.Printf("memory update failed for %s: %v", threadID, err)
			}
			return
		}
		doc, err := s.memorySvc.Load(context.Background(), threadID)
		if err != nil {
			if s.logger != nil {
				s.logger.Printf("memory load failed for %s: %v", threadID, err)
			}
			return
		}
		if err := s.replaceGatewayMemoryDocument(context.Background(), doc); err != nil && s.logger != nil {
			s.logger.Printf("memory cache refresh failed for %s: %v", threadID, err)
			return
		}
		if err := s.persistGatewayState(); err != nil && s.logger != nil {
			s.logger.Printf("persist gateway state after memory update failed for %s: %v", threadID, err)
		}
	}()
}

func cloneRuntimeContext(runtimeContext map[string]any) map[string]any {
	if len(runtimeContext) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(runtimeContext))
	for key, value := range runtimeContext {
		cloned[key] = value
	}
	return cloned
}

func runtimeContextFromRequest(req RunCreateRequest) map[string]any {
	runtimeContext := cloneRuntimeContext(req.Context)
	configurable, _ := req.Config["configurable"].(map[string]any)
	if len(configurable) == 0 {
		return runtimeContext
	}

	// Match deerflow-ui's request shape, where runtime toggles live under
	// config.configurable while allowing explicit request context to win.
	for _, key := range []string{
		"thread_id",
		"thinking_enabled",
		"is_plan_mode",
		"subagent_enabled",
		"max_concurrent_subagents",
		"agent_name",
		"is_bootstrap",
	} {
		if _, exists := runtimeContext[key]; exists {
			continue
		}
		if value, ok := configurable[key]; ok {
			runtimeContext[key] = value
		}
	}
	return runtimeContext
}

func inferBootstrapAgentName(messages []models.Message) string {
	patterns := []string{
		`(?i)\bnew custom agent name is\s+([A-Za-z0-9-]+)\b`,
		`(?i)\bagent name is\s+([A-Za-z0-9-]+)\b`,
		`名称是\s*([A-Za-z0-9-]+)`,
		`名字是\s*([A-Za-z0-9-]+)`,
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != models.RoleHuman {
			continue
		}
		content := strings.TrimSpace(messages[i].Content)
		if content == "" {
			continue
		}
		for _, pattern := range patterns {
			re := regexp.MustCompile(pattern)
			matches := re.FindStringSubmatch(content)
			if len(matches) < 2 {
				continue
			}
			if name, ok := normalizeAgentName(matches[1]); ok {
				return name
			}
		}
	}
	return ""
}

func resolveAgentToolRegistry(base *tools.Registry, toolGroups []string) *tools.Registry {
	if base == nil || len(toolGroups) == 0 {
		return base
	}

	allowed := make(map[string]struct{})
	addTool := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" {
			allowed[name] = struct{}{}
		}
	}

	for _, group := range toolGroups {
		group = strings.TrimSpace(group)
		switch group {
		case "bash":
			addTool("bash")
		case "file":
			addTool("read_file")
			addTool("write_file")
			addTool("glob")
			addTool("present_files")
		case "file:read":
			addTool("read_file")
			addTool("glob")
		case "file:write":
			addTool("write_file")
			addTool("present_files")
		case "interaction":
			addTool("ask_clarification")
		case "agent", "task":
			addTool("task")
		default:
			if tool := base.Get(group); tool != nil {
				addTool(group)
				continue
			}
			for _, tool := range base.ListByGroup(group) {
				addTool(tool.Name)
			}
		}
	}

	addTool("ask_clarification")

	names := make([]string, 0, len(allowed))
	for name := range allowed {
		names = append(names, name)
	}
	return base.Restrict(names)
}

func resolveRuntimeToolRegistry(base *tools.Registry, runtimeContext map[string]any) *tools.Registry {
	if base == nil {
		return nil
	}
	if len(runtimeContext) == 0 {
		return base
	}

	raw, ok := runtimeContext["subagent_enabled"]
	if !ok || boolFromAny(raw) {
		return base
	}

	allowed := make([]string, 0, len(base.List()))
	for _, tool := range base.List() {
		if tool.Name == "task" {
			continue
		}
		allowed = append(allowed, tool.Name)
	}
	return base.Restrict(allowed)
}

func (s *Server) resolveModelToolRegistry(base *tools.Registry, modelName string) *tools.Registry {
	if base == nil {
		return nil
	}
	supportsVision := agent.ModelLikelySupportsVision(modelName)
	if s != nil {
		if model, ok := findConfiguredGatewayModelByNameOrID(s.defaultModel, modelName); ok {
			supportsVision = model.SupportsVision
		}
	}
	if supportsVision {
		return base
	}

	hasViewImage := false
	allowed := make([]string, 0, len(base.List()))
	for _, tool := range base.List() {
		if tool.Name == "view_image" {
			hasViewImage = true
			continue
		}
		allowed = append(allowed, tool.Name)
	}
	if !hasViewImage {
		return base
	}
	return base.Restrict(allowed)
}

func threadConfigFromRuntimeContext(threadID string, runtimeContext map[string]any, cfg runConfig) map[string]any {
	configurable := defaultThreadConfig(threadID)
	modelName := firstNonEmpty(stringFromAny(runtimeContext["model_name"]), cfg.ModelName)
	if modelName != "" {
		configurable["model_name"] = modelName
	}
	for _, key := range []string{"thinking_enabled", "is_plan_mode", "subagent_enabled", "max_concurrent_subagents", "is_bootstrap"} {
		if value, ok := runtimeContext[key]; ok {
			configurable[key] = value
		}
	}
	if effort := firstNonEmpty(stringFromAny(runtimeContext["reasoning_effort"]), cfg.ReasoningEffort); effort != "" {
		configurable["reasoning_effort"] = effort
	}
	if agentName := firstNonEmpty(stringFromAny(runtimeContext["agent_name"]), cfg.AgentName); agentName != "" {
		configurable["agent_name"] = agentName
	}
	if cfg.AgentType != "" {
		configurable["agent_type"] = string(cfg.AgentType)
	}
	return configurable
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

func boolFromAny(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		return err == nil && parsed
	default:
		return false
	}
}
