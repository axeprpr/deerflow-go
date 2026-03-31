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

	runtimeContext := cloneRuntimeContext(req.Context)
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

	s.sessionsMu.RLock()
	existingMessages := append([]models.Message(nil), session.Messages...)
	s.sessionsMu.RUnlock()
	deerMessages := append(existingMessages, newMessages...)

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

	s.saveSession(threadID, result.Messages)
	s.maybeGenerateThreadTitle(ctx, threadID, resolvedRunCfg.ModelName, result.Messages)
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

		content := extractMessageContent(msgMap["content"])
		if role == "" || content == "" {
			continue
		}

		additionalKwargs, _ := msgMap["additional_kwargs"].(map[string]any)
		files := extractUploadedFiles(additionalKwargs)
		if len(files) > 0 || hasHistoricalUploads(s.uploadsDir(threadID), files) {
			content = injectUploadedFilesBlock(content, files, listHistoricalUploads(s.uploadsDir(threadID), files))
		}

		msgSeq++
		msg := models.Message{
			ID:        fmt.Sprintf("msg_%d", msgSeq),
			SessionID: threadID,
			Role:      s.convertRole(role),
			Content:   content,
		}
		if len(additionalKwargs) > 0 {
			if raw, err := json.Marshal(additionalKwargs); err == nil {
				msg.Metadata = map[string]string{"additional_kwargs": string(raw)}
			}
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

func extractUploadedFiles(additionalKwargs map[string]any) []uploadedFile {
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
		path := strings.TrimSpace(firstNonEmpty(stringFromAny(item["virtual_path"]), stringFromAny(item["path"])))
		if filename == "" || path == "" {
			continue
		}
		files = append(files, uploadedFile{
			Filename: filename,
			Size:     int64FromAny(item["size"]),
			Path:     path,
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
	content = strings.TrimSpace(content)
	if content == "" {
		return b.String()
	}
	return b.String() + "\n\n" + content
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
	}
	return 0
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
	case agent.AgentEventToolCallEnd:
		if evt.ToolEvent == nil {
			return
		}
		s.recordAndSendEvent(w, flusher, run, "tool_call_end", evt.ToolEvent)
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
	if boolFromAny(runtimeContext["is_plan_mode"]) {
		cfg.SystemPrompt = joinPromptSections(cfg.SystemPrompt, planModeTodoPrompt)
	}
	cfg.IsBootstrap = cfg.IsBootstrap || boolFromAny(runtimeContext["is_bootstrap"])
	cfg.AgentName = firstNonEmpty(stringFromAny(runtimeContext["agent_name"]), cfg.AgentName)
	if cfg.IsBootstrap {
		if cfg.AgentName != "" {
			name, ok := normalizeAgentName(cfg.AgentName)
			if !ok {
				return runConfig{}, fmt.Errorf("invalid agent name")
			}
			cfg.AgentName = name
		}
		basePrompt := strings.TrimSpace(cfg.SystemPrompt)
		if basePrompt == "" {
			basePrompt = agent.GetAgentTypeConfig(cfg.AgentType).SystemPrompt
		}
		cfg.SystemPrompt = joinPromptSections(basePrompt, bootstrapAgentPrompt)
		cfg.Tools = s.tools
		return cfg, nil
	}
	if cfg.AgentName == "" {
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
	basePrompt := strings.TrimSpace(cfg.SystemPrompt)
	if basePrompt == "" {
		basePrompt = agent.GetAgentTypeConfig(cfg.AgentType).SystemPrompt
	}
	cfg.SystemPrompt = joinPromptSections(basePrompt, customPrompt)

	if len(customAgent.ToolGroups) > 0 {
		cfg.Tools = resolveAgentToolRegistry(s.tools, customAgent.ToolGroups)
	}
	return cfg, nil
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
			addTool("present_file")
		case "file:read":
			addTool("read_file")
			addTool("glob")
		case "file:write":
			addTool("write_file")
			addTool("present_file")
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
