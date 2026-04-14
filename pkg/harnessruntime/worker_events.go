package harnessruntime

import (
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
)

type WorkerRunEventRecorder struct {
	store     RunEventRecorder
	threads   ThreadStateStore
	snapshots RunSnapshotStore
}

func NewWorkerRunEventRecorder(store RunEventRecorder, threads ...ThreadStateStore) WorkerRunEventRecorder {
	recorder := WorkerRunEventRecorder{store: store}
	if len(threads) > 0 {
		recorder.threads = threads[0]
	}
	return recorder
}

func NewWorkerRunEventRecorderWithRuntime(store RunEventRecorder, threads ThreadStateStore, snapshots RunSnapshotStore) WorkerRunEventRecorder {
	return WorkerRunEventRecorder{
		store:     store,
		threads:   threads,
		snapshots: snapshots,
	}
}

func (r WorkerRunEventRecorder) RecordAgentEvent(plan WorkerExecutionPlan, evt agent.AgentEvent) {
	if r.store == nil {
		return
	}
	record := EventLogService{store: r.store}
	ctx := workerRunEventContext(plan, r.threads)
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
		r.syncRunningRecord(plan)
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

func (r WorkerRunEventRecorder) RecordTaskEvent(plan WorkerExecutionPlan, evt subagent.TaskEvent) {
	if r.store == nil {
		return
	}
	EventLogService{store: r.store}.RecordWithContext(workerRunEventContext(plan, r.threads), plan.RunID, plan.ThreadID, evt.Type, map[string]any{
		"type":           evt.Type,
		"task_id":        evt.TaskID,
		"request_id":     evt.RequestID,
		"description":    evt.Description,
		"message":        evt.Message,
		"message_index":  evt.MessageIndex,
		"total_messages": evt.TotalMessages,
		"result":         evt.Result,
		"error":          evt.Error,
	})
	r.syncRunningRecord(plan)
}

func (r WorkerRunEventRecorder) RecordClarification(plan WorkerExecutionPlan, item *clarification.Clarification) {
	if r.store == nil || item == nil {
		return
	}
	EventLogService{store: r.store}.RecordWithContext(workerRunEventContext(plan, r.threads), plan.RunID, plan.ThreadID, "clarification_request", item)
}

func (r WorkerRunEventRecorder) RecordCompletion(plan WorkerExecutionPlan, result *agent.RunResult, outcome RunOutcomeDescriptor) {
	if r.store == nil {
		return
	}
	payload := map[string]any{"run_id": plan.RunID}
	if result != nil && result.Usage != nil {
		payload["usage"] = result.Usage
	}
	ctx := workerRunEventContext(plan, r.threads)
	if outcome.RunStatus == "" {
		outcome = RunOutcomeDescriptor{
			RunStatus: "success",
		}
	}
	ctx.Outcome = NewOutcomeService().BindRecord(RunRecord{
		Attempt:         plan.Attempt,
		ResumeFromEvent: plan.ResumeFromEvent,
		ResumeReason:    plan.ResumeReason,
	}, outcome)
	EventLogService{store: r.store}.RecordWithContext(ctx, plan.RunID, plan.ThreadID, "end", payload)
}

func workerRunEventContext(plan WorkerExecutionPlan, threads ThreadStateStore) RunEventContext {
	return RunEventContext{
		Attempt:         plan.Attempt,
		ResumeFromEvent: plan.ResumeFromEvent,
		ResumeReason:    plan.ResumeReason,
		Outcome: NewOutcomeService().DescribeLiveRunning(RunRecord{
			ThreadID:        plan.ThreadID,
			Attempt:         plan.Attempt,
			ResumeFromEvent: plan.ResumeFromEvent,
			ResumeReason:    plan.ResumeReason,
		}, threads),
	}
}

func workerToolMessageID(toolCallID string) string {
	if strings.TrimSpace(toolCallID) == "" {
		return ""
	}
	return "tool:" + strings.TrimSpace(toolCallID)
}

func runningOutcomeDescriptor(plan WorkerExecutionPlan, threads ThreadStateStore) RunOutcomeDescriptor {
	return NewOutcomeService().DescribeLiveRunning(RunRecord{
		ThreadID:        plan.ThreadID,
		Attempt:         plan.Attempt,
		ResumeFromEvent: plan.ResumeFromEvent,
		ResumeReason:    plan.ResumeReason,
	}, threads)
}

func (r WorkerRunEventRecorder) syncRunningRecord(plan WorkerExecutionPlan) {
	if r.snapshots == nil {
		return
	}
	record := loadWorkerRunRecord(plan, r.snapshots)
	record.Status = "running"
	record.Error = ""
	record.Outcome = runningOutcomeDescriptor(plan, r.threads)
	record.UpdatedAt = time.Now().UTC()
	NewSnapshotStoreService(r.snapshots).SaveRecord(record)
}
