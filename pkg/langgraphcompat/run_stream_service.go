package langgraphcompat

import (
	"net/http"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
)

type runStreamEmitter struct {
	server  *Server
	w       http.ResponseWriter
	flusher http.Flusher
	run     *Run
	filter  streamModeFilter
}

type runReplayStreamer struct {
	server  *Server
	w       http.ResponseWriter
	flusher http.Flusher
	filter  streamModeFilter
}

func (s *Server) newRunStreamEmitter(w http.ResponseWriter, flusher http.Flusher, run *Run, filter streamModeFilter) runStreamEmitter {
	return runStreamEmitter{
		server:  s,
		w:       w,
		flusher: flusher,
		run:     run,
		filter:  filter,
	}
}

func (s *Server) newRunReplayStreamer(w http.ResponseWriter, flusher http.Flusher, filter streamModeFilter) runReplayStreamer {
	return runReplayStreamer{
		server:  s,
		w:       w,
		flusher: flusher,
		filter:  filter,
	}
}

func (s *Server) recordRunEvent(run *Run, eventType string, data any) StreamEvent {
	if run == nil {
		return StreamEvent{}
	}
	event := harnessruntime.NewEventLogService(s.runtimeEventAdapter()).Record(run.RunID, run.ThreadID, eventType, data)
	return streamEventFromRuntimeEvent(event)
}

func (e runStreamEmitter) Metadata(threadID string, assistantID string) {
	e.server.recordAndSendEventFiltered(e.w, e.flusher, e.run, e.filter, "metadata", map[string]any{
		"run_id":       e.run.RunID,
		"thread_id":    threadID,
		"assistant_id": assistantID,
	})
}

func (e runStreamEmitter) Clarification(item *clarification.Clarification) {
	if item == nil {
		return
	}
	e.server.recordAndSendEventFiltered(e.w, e.flusher, e.run, e.filter, "clarification_request", item)
}

func (e runStreamEmitter) Task(evt subagent.TaskEvent) {
	e.server.forwardTaskEvent(e.w, e.flusher, e.run, e.filter, evt)
}

func (e runStreamEmitter) Agent(evt agent.AgentEvent) {
	e.server.forwardAgentEvent(e.w, e.flusher, e.run, e.filter, evt)
}

func (e runStreamEmitter) FinalMessages(existingMessages []models.Message, resultMessages []models.Message, usage *agent.Usage) {
	e.server.emitFinalMessagesTuple(e.w, e.flusher, e.run, e.filter, existingMessages, resultMessages, usage)
}

func (e runStreamEmitter) Completion(completed *completedRun, usage *agent.Usage) {
	if completed == nil || completed.State == nil {
		return
	}
	e.server.recordAndSendEventFiltered(e.w, e.flusher, e.run, e.filter, "updates", map[string]any{
		"agent": map[string]any{
			"messages":  completed.State.Values["messages"],
			"title":     completed.State.Values["title"],
			"artifacts": completed.State.Values["artifacts"],
		},
	})
	e.server.recordAndSendEventFiltered(e.w, e.flusher, e.run, e.filter, "values", completed.State.Values)
	e.server.recordAndSendEventFiltered(e.w, e.flusher, e.run, e.filter, "end", map[string]any{
		"run_id": e.run.RunID,
		"usage":  usage,
	})
}

func (s runReplayStreamer) Replay(run *Run) bool {
	if run == nil {
		return false
	}
	events, replayedEnd := harnessruntime.NewEventFeedService(s.server.runtimeEventAdapter()).Replay(run.RunID)
	for _, event := range events {
		if !s.filter.allows(event.Event) {
			continue
		}
		s.server.sendSSEEvent(s.w, s.flusher, streamEventFromRuntimeEvent(event))
	}
	s.flusher.Flush()
	return replayedEnd
}

func (s runReplayStreamer) Join(run *Run) {
	if run == nil {
		return
	}
	sub, unsubscribe := harnessruntime.NewEventFeedService(s.server.runtimeEventAdapter()).Subscribe(run.RunID, 16)
	defer unsubscribe()
	for {
		select {
		case event, ok := <-sub:
			if !ok {
				return
			}
			if s.filter.allows(event.Event) {
				s.server.sendSSEEvent(s.w, s.flusher, streamEventFromRuntimeEvent(event))
			}
			if event.Event == "end" {
				return
			}
		case <-time.After(defaultSSEHeartbeatInterval):
			sendSSEHeartbeat(s.w, s.flusher)
		}
	}
}

func (s *Server) recordAndSendEvent(w http.ResponseWriter, flusher http.Flusher, run *Run, eventType string, data any) {
	event := s.recordRunEvent(run, eventType, data)
	s.sendSSEEvent(w, flusher, event)
}

func (s *Server) recordAndSendEventFiltered(w http.ResponseWriter, flusher http.Flusher, run *Run, filter streamModeFilter, eventType string, data any) {
	event := s.recordRunEvent(run, eventType, data)
	if filter.allows(eventType) {
		s.sendSSEEvent(w, flusher, event)
	}
}

func compactToolAliasPayload(run *Run, eventName string, toolEvent *agent.ToolCallEvent, includeRuntimeIDs bool) map[string]any {
	payload := map[string]any{
		"event": eventName,
		"name":  toolEvent.Name,
		"data":  toolEvent,
	}
	if includeRuntimeIDs && run != nil {
		payload["run_id"] = run.RunID
		payload["thread_id"] = run.ThreadID
	}
	return payload
}

func toolUpdatesPayload(state *ThreadState, extra map[string]any) map[string]any {
	agentUpdate := map[string]any{}
	if state != nil {
		if state.Values != nil {
			agentUpdate["artifacts"] = state.Values["artifacts"]
			if messages, ok := state.Values["messages"]; ok {
				agentUpdate["messages"] = messages
			}
			if title, ok := state.Values["title"]; ok {
				agentUpdate["title"] = title
			}
		}
	}
	for key, value := range extra {
		agentUpdate[key] = value
	}
	return map[string]any{"agent": agentUpdate}
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
		s.recordAndSendEventFiltered(w, flusher, run, filter, "events", compactToolAliasPayload(run, "on_tool_start", evt.ToolEvent, true))
	case agent.AgentEventToolCallEnd:
		if evt.ToolEvent == nil {
			return
		}
		s.recordAndSendEventFiltered(w, flusher, run, filter, "tool_call_end", evt.ToolEvent)
		s.recordAndSendEventFiltered(w, flusher, run, filter, "events", compactToolAliasPayload(run, "on_tool_end", evt.ToolEvent, true))
		s.recordAndSendEventFiltered(w, flusher, run, filter, "on_tool_end", compactToolAliasPayload(nil, "on_tool_end", evt.ToolEvent, false))
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
		state := s.getThreadState(run.ThreadID)
		toolName := strings.TrimSpace(strings.ToLower(resolvedToolNameForArtifacts(evt)))
		if toolName == "setup_agent" && evt.Result != nil && evt.Result.Status == models.CallStatusCompleted {
			if state != nil {
				if createdAgent := stringFromAny(state.Values["created_agent_name"]); createdAgent != "" {
					s.recordAndSendEventFiltered(w, flusher, run, filter, "updates", toolUpdatesPayload(state, map[string]any{
						"created_agent_name": createdAgent,
					}))
				}
			}
		}
		if toolName == "present_files" || toolName == "present_file" || toolMayAffectArtifacts(toolName) {
			s.recordAndSendEventFiltered(w, flusher, run, filter, "updates", toolUpdatesPayload(state, nil))
		}
	case agent.AgentEventEnd:
		msg := Message{
			Type:    "ai",
			ID:      evt.MessageID,
			Role:    "assistant",
			Content: rewriteArtifactLinksInText(run.ThreadID, evt.Text),
		}
		if msg.Content == nil {
			msg.Content = ""
		}
		if evt.Usage != nil {
			msg.UsageMetadata = usageMetadataMap(evt.Usage)
		}
		if additionalKwargs := additionalKwargsFromMessageMetadata(evt.Metadata); len(additionalKwargs) > 0 {
			msg.AdditionalKwargs = additionalKwargs
		}
		s.recordAndSendEventFiltered(w, flusher, run, filter, "messages-tuple", msg)
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
		"type":           evt.Type,
		"task_id":        evt.TaskID,
		"request_id":     evt.RequestID,
		"description":    evt.Description,
		"message":        evt.Message,
		"message_index":  evt.MessageIndex,
		"total_messages": evt.TotalMessages,
		"result":         evt.Result,
		"error":          evt.Error,
	}
	s.recordAndSendEventFiltered(w, flusher, run, filter, evt.Type, data)
}
