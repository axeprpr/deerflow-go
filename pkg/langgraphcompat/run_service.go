package langgraphcompat

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

type preparedRunRequest struct {
	ThreadID         string
	AssistantID      string
	PresentFiles     *tools.PresentFileRegistry
	ExistingMessages []models.Message
	Messages         []models.Message
	Lifecycle        *harness.RunState
	Run              *Run
}

type completedRun struct {
	Result      *agent.RunResult
	State       *ThreadState
	Interrupted bool
}

func (s *Server) prepareRunRequest(routeThreadID string, req RunCreateRequest) *preparedRunRequest {
	preflightService := harnessruntime.NewPreflightService(s.runtimePreflightAdapter())
	threadID, _ := preflightService.Resolve(harnessruntime.PreflightInput{
		RouteThreadID:      routeThreadID,
		RequestedThreadID:  req.ThreadID,
		RequestedThreadIDX: req.ThreadIDX,
		AssistantID:        req.AssistantID,
		AssistantIDX:       req.AssistantIDX,
	})

	input := req.Input
	if input == nil {
		input = make(map[string]any)
	}
	rawMessages, _ := input["messages"].([]any)
	if len(rawMessages) == 0 {
		rawMessages = req.Messages
	}
	newMessages := s.convertToMessages(threadID, rawMessages)
	preflight := preflightService.Prepare(harnessruntime.PreflightInput{
		RouteThreadID:      routeThreadID,
		RequestedThreadID:  req.ThreadID,
		RequestedThreadIDX: req.ThreadIDX,
		AssistantID:        req.AssistantID,
		AssistantIDX:       req.AssistantIDX,
		NewMessages:        newMessages,
	})

	return &preparedRunRequest{
		ThreadID:         preflight.ThreadID,
		AssistantID:      preflight.AssistantID,
		PresentFiles:     preflight.PresentFiles,
		ExistingMessages: preflight.ExistingMessages,
		Messages:         preflight.Messages,
		Run:              runFromRecord(preflight.Run),
	}
}

func (s *Server) buildRunExecution(ctx context.Context, prepared *preparedRunRequest, req RunCreateRequest) (*harness.Execution, error) {
	runCfg := parseRunConfig(mergeRunConfig(req.Config, req.Context))
	s.applyRunConfigMetadata(prepared.ThreadID, runCfg)
	agentSpec, err := s.resolveRunConfig(runCfg, nil)
	if err != nil {
		return nil, err
	}
	agentSpec.PresentFiles = prepared.PresentFiles
	orchestrated, err := harnessruntime.NewOrchestrator(s.runtimeView()).Prepare(ctx, harnessruntime.RunPlan{
		ThreadID:         prepared.ThreadID,
		AssistantID:      prepared.AssistantID,
		Model:            agentSpec.Model,
		AgentName:        runCfg.AgentName,
		Spec:             agentSpec,
		ExistingMessages: prepared.ExistingMessages,
		Messages:         prepared.Messages,
	})
	if err != nil {
		return nil, err
	}
	prepared.Lifecycle = orchestrated.Lifecycle
	prepared.Messages = append([]models.Message(nil), orchestrated.Lifecycle.Messages...)
	return orchestrated.Execution, nil
}

func runFromRecord(record harnessruntime.RunRecord) *Run {
	return &Run{
		RunID:       record.RunID,
		ThreadID:    record.ThreadID,
		AssistantID: record.AssistantID,
		Status:      record.Status,
		Error:       record.Error,
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
	}
}

func runRecordFromRun(run *Run) harnessruntime.RunRecord {
	if run == nil {
		return harnessruntime.RunRecord{}
	}
	return harnessruntime.RunRecord{
		RunID:       run.RunID,
		ThreadID:    run.ThreadID,
		AssistantID: run.AssistantID,
		Status:      run.Status,
		Error:       run.Error,
		CreatedAt:   run.CreatedAt,
		UpdatedAt:   run.UpdatedAt,
	}
}

func applyRunRecord(run *Run, record harnessruntime.RunRecord) {
	if run == nil {
		return
	}
	run.RunID = record.RunID
	run.ThreadID = record.ThreadID
	run.AssistantID = record.AssistantID
	run.Status = record.Status
	run.Error = record.Error
	run.CreatedAt = record.CreatedAt
	run.UpdatedAt = record.UpdatedAt
}

func (s *Server) bindRunContext(ctx context.Context, threadID string, taskSink func(subagent.TaskEvent), clarificationSink func(*clarification.Clarification)) context.Context {
	return harnessruntime.NewContextService(s.runtimeContextAdapter(), s.runtimeView()).Bind(ctx, threadID, harness.RunHooks{
		TaskSink:          taskSink,
		ClarificationSink: clarificationSink,
	})
}

func (s *Server) markRunError(run *Run, threadID string, err error) {
	record := harnessruntime.NewRunStateService(s.runtimeRunStateAdapter()).MarkError(runRecordFromRun(run), err)
	applyRunRecord(run, record)
}

func (s *Server) markRunCanceled(run *Run, threadID string) {
	record := harnessruntime.NewRunStateService(s.runtimeRunStateAdapter()).MarkCanceled(runRecordFromRun(run))
	applyRunRecord(run, record)
}

func (s *Server) finalizeCompletedRun(ctx context.Context, prepared *preparedRunRequest, result *agent.RunResult) *completedRun {
	if prepared.Lifecycle != nil {
		prepared.Lifecycle.Messages = append([]models.Message(nil), result.Messages...)
		_ = s.runtimeView().AfterRun(ctx, prepared.Lifecycle, result)
	}
	s.saveSession(prepared.ThreadID, result.Messages)

	outcome := harnessruntime.NewCompletionService(s.runtimeCompletionAdapter(), "generated_title", "clarification_interrupt").Apply(prepared.ThreadID, prepared.Lifecycle, result)
	record := harnessruntime.NewRunStateService(s.runtimeRunStateAdapter()).Finalize(runRecordFromRun(prepared.Run), outcome)
	applyRunRecord(prepared.Run, record)

	state := s.getThreadState(prepared.ThreadID)

	return &completedRun{
		Result:      result,
		State:       state,
		Interrupted: outcome.Interrupted,
	}
}

func (s *Server) threadRun(threadID, runID string) *Run {
	run := s.getRun(strings.TrimSpace(runID))
	if run == nil {
		return nil
	}
	if threadID != "" && run.ThreadID != strings.TrimSpace(threadID) {
		return nil
	}
	return run
}

func (s *Server) waitForThreadRun(ctx context.Context, threadID, runID string, cancelOnDisconnect bool) (*Run, bool) {
	for {
		run := s.threadRun(threadID, runID)
		if run == nil {
			return nil, false
		}
		switch strings.ToLower(strings.TrimSpace(run.Status)) {
		case "", "running", "queued", "busy":
		default:
			return run, true
		}

		select {
		case <-ctx.Done():
			if cancelOnDisconnect {
				s.cancelActiveRun(runID)
			}
			return nil, true
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *Server) cancelThreadRun(threadID, runID string) (map[string]any, bool, bool) {
	run := s.threadRun(threadID, runID)
	if run == nil {
		return nil, false, false
	}
	switch strings.ToLower(strings.TrimSpace(run.Status)) {
	case "", "running", "queued", "busy":
	default:
		return nil, true, false
	}
	if !s.cancelActiveRun(runID) {
		return nil, true, false
	}
	return map[string]any{
		"run_id":    runID,
		"thread_id": threadID,
		"status":    "interrupted",
	}, true, true
}

func (s *Server) listThreadRunResponses(threadID string) []map[string]any {
	s.runsMu.RLock()
	runs := make([]map[string]any, 0)
	for _, run := range s.runs {
		if run.ThreadID != threadID {
			continue
		}
		runs = append(runs, runResponse(run))
	}
	s.runsMu.RUnlock()
	sort.Slice(runs, func(i, j int) bool {
		return runs[i]["created_at"].(string) > runs[j]["created_at"].(string)
	})
	return runs
}

func runResponse(run *Run) map[string]any {
	if run == nil {
		return nil
	}
	out := map[string]any{
		"run_id":       run.RunID,
		"thread_id":    run.ThreadID,
		"assistant_id": run.AssistantID,
		"status":       run.Status,
		"created_at":   run.CreatedAt.Format(time.RFC3339Nano),
		"updated_at":   run.UpdatedAt.Format(time.RFC3339Nano),
	}
	if strings.TrimSpace(run.Error) != "" {
		out["error"] = run.Error
	}
	return out
}
