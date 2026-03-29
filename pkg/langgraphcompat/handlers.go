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
	"github.com/google/uuid"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

type runConfig struct {
	ModelName       string
	ReasoningEffort string
	Temperature     *float64
	MaxTokens       *int
}

func (s *Server) handleRunsStream(w http.ResponseWriter, r *http.Request) {
	s.handleStreamRequest(w, r, "")
}

func (s *Server) handleThreadRunsStream(w http.ResponseWriter, r *http.Request) {
	s.handleStreamRequest(w, r, r.PathValue("thread_id"))
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
		AssistantID: req.AssistantID,
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Content-Location", fmt.Sprintf("/threads/%s/runs/%s", threadID, runID))

	s.recordAndSendEvent(w, flusher, run, "metadata", map[string]any{
		"run_id":    runID,
		"thread_id": threadID,
	})

	runCfg := parseRunConfig(req.Config)
	runAgent := s.newAgent(agent.AgentConfig{
		PresentFiles:    session.PresentFiles,
		Model:           firstNonEmpty(runCfg.ModelName, s.defaultModel),
		ReasoningEffort: runCfg.ReasoningEffort,
		Temperature:     runCfg.Temperature,
		MaxTokens:       runCfg.MaxTokens,
	})

	ctx := subagent.WithEventSink(r.Context(), func(evt subagent.TaskEvent) {
		s.forwardTaskEvent(w, flusher, run, evt)
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
		run.Status = "error"
		run.Error = err.Error()
		run.UpdatedAt = time.Now().UTC()
		s.saveRun(run)
		s.markThreadStatus(threadID, "error")
		return
	}

	s.saveSession(threadID, result.Messages)
	state := s.getThreadState(threadID)

	s.recordAndSendEvent(w, flusher, run, "updates", map[string]any{
		"agent": map[string]any{
			"messages":  state.Values["messages"],
			"title":     state.Values["title"],
			"artifacts": state.Values["artifacts"],
		},
	})
	s.recordAndSendEvent(w, flusher, run, "values", state.Values)
	s.recordAndSendEvent(w, flusher, run, "end", map[string]any{
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

	for _, event := range run.Events {
		if event.ID != "" {
			fmt.Fprintf(w, "id: %s\n", event.ID)
		}
		jsonData, err := json.Marshal(event.Data)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "event: %s\n", event.Event)
		fmt.Fprintf(w, "data: %s\n\n", jsonData)
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

	for _, event := range run.Events {
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
		if role == "" || content == "" {
			continue
		}

		msgSeq++
		msg := models.Message{
			ID:        fmt.Sprintf("msg_%d", msgSeq),
			SessionID: threadID,
			Role:      s.convertRole(role),
			Content:   content,
		}
		messages = append(messages, msg)
	}

	return messages
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

func (s *Server) saveSession(threadID string, messages []models.Message) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	if session, exists := s.sessions[threadID]; exists {
		session.Messages = append([]models.Message(nil), messages...)
		session.Status = "idle"
		session.UpdatedAt = time.Now().UTC()
	} else {
		s.sessions[threadID] = &Session{
			ThreadID:     threadID,
			Messages:     append([]models.Message(nil), messages...),
			Metadata:     make(map[string]any),
			Status:       "idle",
			PresentFiles: tools.NewPresentFileRegistry(),
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
	}
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
			Role:    "assistant",
			Content: evt.Text,
		})
	case agent.AgentEventToolCall:
		if evt.ToolEvent == nil {
			return
		}
		s.recordAndSendEvent(w, flusher, run, "tool_call", evt.ToolEvent)
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
		s.recordAndSendEvent(w, flusher, run, "messages-tuple", Message{
			Type:       "tool",
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
		Temperature:     floatPointerFromAny(firstNonNil(raw["temperature"], configurable["temperature"])),
		MaxTokens:       intPointerFromAny(firstNonNil(raw["max_tokens"], configurable["max_tokens"])),
	}
	return cfg
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
