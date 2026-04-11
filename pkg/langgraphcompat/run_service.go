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
	"github.com/google/uuid"
)

type preparedRunRequest struct {
	ThreadID         string
	AssistantID      string
	Session          *Session
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
	rawMessages, _ := input["messages"].([]any)
	if len(rawMessages) == 0 {
		rawMessages = req.Messages
	}
	newMessages := s.convertToMessages(threadID, rawMessages)

	s.sessionsMu.RLock()
	existingMessages := append([]models.Message(nil), session.Messages...)
	s.sessionsMu.RUnlock()

	run := &Run{
		RunID:       uuid.New().String(),
		ThreadID:    threadID,
		AssistantID: assistantID,
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)
	s.setThreadMetadata(threadID, "assistant_id", assistantID)
	s.setThreadMetadata(threadID, "graph_id", firstNonEmpty(assistantID, "lead_agent"))
	s.setThreadMetadata(threadID, "run_id", run.RunID)
	s.deleteThreadMetadata(threadID, "interrupts")

	return &preparedRunRequest{
		ThreadID:         threadID,
		AssistantID:      assistantID,
		Session:          session,
		ExistingMessages: existingMessages,
		Messages:         append(existingMessages, newMessages...),
		Run:              run,
	}
}

func (s *Server) buildRunExecution(ctx context.Context, prepared *preparedRunRequest, req RunCreateRequest) (*harness.Execution, error) {
	runCfg := parseRunConfig(mergeRunConfig(req.Config, req.Context))
	s.applyRunConfigMetadata(prepared.ThreadID, runCfg)
	agentSpec, err := s.resolveRunConfig(runCfg, nil)
	if err != nil {
		return nil, err
	}
	agentSpec.PresentFiles = prepared.Session.PresentFiles
	lifecycle := &harness.RunState{
		ThreadID:         prepared.ThreadID,
		AssistantID:      prepared.AssistantID,
		Model:            agentSpec.Model,
		AgentName:        runCfg.AgentName,
		Spec:             agentSpec,
		ExistingMessages: append([]models.Message(nil), prepared.ExistingMessages...),
		Messages:         append([]models.Message(nil), prepared.Messages...),
		Metadata:         map[string]any{},
	}
	if err := s.runtimeView().BeforeRun(ctx, lifecycle); err != nil {
		return nil, err
	}
	prepared.Lifecycle = lifecycle
	prepared.Messages = append([]models.Message(nil), lifecycle.Messages...)
	return s.runtimeView().PrepareRun(harness.RunRequest{
		Agent: harness.AgentRequest{
			Spec:     lifecycle.Spec,
			Features: harness.FeatureSet{Sandbox: true},
		},
		SessionID: prepared.ThreadID,
		Messages:  lifecycle.Messages,
	})
}

func (s *Server) buildRunContextSpec(threadID string, taskSink func(subagent.TaskEvent), clarificationSink func(*clarification.Clarification)) harness.ContextSpec {
	return harness.ContextSpec{
		ThreadID:             threadID,
		ClarificationManager: s.clarify,
		RuntimeContext: map[string]any{
			"skill_paths": s.runtimeSkillPaths(),
		},
		Hooks: harness.RunHooks{
			TaskSink:          taskSink,
			ClarificationSink: clarificationSink,
		},
	}
}

func (s *Server) markRunError(run *Run, threadID string, err error) {
	run.Status = "error"
	run.Error = err.Error()
	run.UpdatedAt = time.Now().UTC()
	s.saveRun(run)
	s.markThreadStatus(threadID, "error")
}

func (s *Server) markRunCanceled(run *Run, threadID string) {
	run.Status = "interrupted"
	run.Error = ""
	run.UpdatedAt = time.Now().UTC()
	s.saveRun(run)
	s.markThreadStatus(threadID, "interrupted")
}

func (s *Server) finalizeCompletedRun(ctx context.Context, prepared *preparedRunRequest, result *agent.RunResult) *completedRun {
	if prepared.Lifecycle != nil {
		prepared.Lifecycle.Messages = append([]models.Message(nil), result.Messages...)
		_ = s.runtimeView().AfterRun(ctx, prepared.Lifecycle, result)
	}
	s.saveSession(prepared.ThreadID, result.Messages)

	outcome := harnessruntime.NewCompletionService(s, "generated_title", "clarification_interrupt").Apply(prepared.ThreadID, prepared.Lifecycle, result)
	if outcome.Interrupted {
		prepared.Run.Status = "interrupted"
	} else {
		prepared.Run.Status = "success"
	}

	state := s.getThreadState(prepared.ThreadID)
	prepared.Run.UpdatedAt = time.Now().UTC()
	s.saveRun(prepared.Run)

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
