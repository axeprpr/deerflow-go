package harnessruntime

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
)

type WorkerRunEventRecorder struct {
	store RunEventRecorder
}

func NewWorkerRunEventRecorder(store RunEventRecorder) WorkerRunEventRecorder {
	return WorkerRunEventRecorder{store: store}
}

func (r WorkerRunEventRecorder) RecordAgentEvent(plan WorkerExecutionPlan, evt agent.AgentEvent) {
	if r.store == nil {
		return
	}
	record := EventLogService{store: r.store}
	ctx := RunEventContext{
		Attempt:         plan.Attempt,
		ResumeFromEvent: plan.ResumeFromEvent,
		ResumeReason:    plan.ResumeReason,
		Outcome: RunOutcomeDescriptor{
			RunStatus:       "running",
			Attempt:         plan.Attempt,
			ResumeFromEvent: plan.ResumeFromEvent,
			ResumeReason:    plan.ResumeReason,
		},
	}
	recordEvent := func(eventType string, data any) {
		record.RecordWithContext(ctx, plan.RunID, plan.ThreadID, eventType, data)
	}

	switch evt.Type {
	case agent.AgentEventChunk:
		recordEvent("chunk", map[string]any{
			"run_id":    plan.RunID,
			"thread_id": plan.ThreadID,
			"type":      "ai",
			"role":      "assistant",
			"delta":     evt.Text,
			"content":   evt.Text,
		})
	case agent.AgentEventToolCall:
		if evt.ToolEvent != nil {
			recordEvent("tool_call", evt.ToolEvent)
		}
	case agent.AgentEventToolCallStart:
		if evt.ToolEvent == nil {
			return
		}
		recordEvent("tool_call_start", evt.ToolEvent)
		recordEvent("events", map[string]any{
			"event":     "on_tool_start",
			"name":      evt.ToolEvent.Name,
			"data":      evt.ToolEvent,
			"run_id":    plan.RunID,
			"thread_id": plan.ThreadID,
		})
	case agent.AgentEventToolCallEnd:
		if evt.ToolEvent == nil {
			return
		}
		recordEvent("tool_call_end", evt.ToolEvent)
		alias := map[string]any{
			"event":     "on_tool_end",
			"name":      evt.ToolEvent.Name,
			"data":      evt.ToolEvent,
			"run_id":    plan.RunID,
			"thread_id": plan.ThreadID,
		}
		recordEvent("events", alias)
		recordEvent("on_tool_end", map[string]any{
			"event": "on_tool_end",
			"name":  evt.ToolEvent.Name,
			"data":  evt.ToolEvent,
		})
		recordEvent("messages-tuple", map[string]any{
			"type":         "tool",
			"id":           workerToolMessageID(evt.ToolEvent.ID),
			"role":         "tool",
			"name":         evt.ToolEvent.Name,
			"content":      evt.ToolEvent.ResultPreview,
			"tool_call_id": evt.ToolEvent.ID,
			"data": map[string]any{
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
		recordEvent("error", errData)
	}
}

func workerToolMessageID(toolCallID string) string {
	if strings.TrimSpace(toolCallID) == "" {
		return ""
	}
	return "tool:" + strings.TrimSpace(toolCallID)
}
