package langgraphcompat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
)

func (s *Server) handleRunsStream(w http.ResponseWriter, r *http.Request) {
	s.handleStreamRequest(w, r, "")
}

func (s *Server) handleThreadRunsStream(w http.ResponseWriter, r *http.Request) {
	s.handleStreamRequest(w, r, r.PathValue("thread_id"))
}

func (s *Server) handleThreadRunsCreate(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if threadID == "" {
		writeDetailError(w, http.StatusBadRequest, "thread ID required")
		return
	}

	var req RunCreateRequest
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeDetailError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
			return
		}
	}
	if err := validateStrictLangGraphRunRequest(r, threadID, req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": err.Error()})
		return
	}
	prepared := s.prepareRunRequest(threadID, req)
	preparedExecution, err := s.buildRunExecution(r.Context(), prepared, req)
	if err != nil {
		s.markRunError(prepared.Run, prepared.ThreadID, err)
		writeDetailError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var result *agent.RunResult
	if preparedExecution.Completed != nil {
		result = preparedExecution.Completed
	} else {
		ctx := s.bindRunContext(r.Context(), prepared.ThreadID, func(evt subagent.TaskEvent) {}, func(item *clarification.Clarification) {})
		result, err = preparedExecution.Execution.Run(ctx)
		if err != nil {
			s.markRunError(prepared.Run, prepared.ThreadID, err)
			writeDetailError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	completed := s.finalizeCompletedRun(r.Context(), prepared, result)

	values := map[string]any{}
	if completed.State != nil {
		for k, v := range completed.State.Values {
			values[k] = v
		}
	}
	values["run_id"] = prepared.Run.RunID
	values["thread_id"] = prepared.ThreadID
	writeJSON(w, http.StatusOK, values)
}

func (s *Server) handleStreamRequest(w http.ResponseWriter, r *http.Request, routeThreadID string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeDetailError(w, http.StatusBadRequest, fmt.Sprintf("failed to read body: %v", err))
		return
	}
	defer r.Body.Close()

	var req RunCreateRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeDetailError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
			return
		}
	}
	if err := validateStrictLangGraphRunRequest(r, routeThreadID, req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": err.Error()})
		return
	}
	prepared := s.prepareRunRequest(routeThreadID, req)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeDetailError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	sse := newSSEWriter(w, flusher)
	w = sse
	flusher = sse

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Content-Location", fmt.Sprintf("/threads/%s/runs/%s", prepared.ThreadID, prepared.Run.RunID))

	filter := newStreamModeFilter(firstNonNil(req.StreamMode, req.StreamModeX))
	emitter := s.newRunStreamEmitter(w, flusher, prepared.Run, filter)
	emitter.Metadata(prepared.ThreadID, prepared.AssistantID)

	var remoteReplayDone chan struct{}
	var remoteReplayResult chan bool
	remoteReplayedEnd := false
	if s.usesRemoteDispatch() {
		remoteReplayDone = make(chan struct{})
		remoteReplayResult = make(chan bool, 1)
		go func() {
			remoteReplayResult <- s.newRunReplayStreamer(w, flusher, filter).Poll(prepared.Run, remoteReplayDone)
		}()
	}

	preparedExecution, err := s.buildRunExecution(r.Context(), prepared, req)
	if remoteReplayDone != nil {
		close(remoteReplayDone)
		remoteReplayedEnd = <-remoteReplayResult
	}
	if err != nil {
		s.markRunError(prepared.Run, prepared.ThreadID, err)
		return
	}

	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancelRun()
	s.setRunCancel(prepared.Run.RunID, cancelRun)
	defer s.clearRunCancel(prepared.Run.RunID)
	runDone := make(chan struct{})
	defer close(runDone)
	if requestedOnDisconnect(req) == "cancel" {
		go s.cancelRunOnClientDisconnect(r.Context(), runDone, cancelRun)
	}
	heartbeatDone := make(chan struct{})
	go streamSSEHeartbeats(runCtx, heartbeatDone, w, flusher)
	defer close(heartbeatDone)

	ctx := s.bindRunContext(runCtx, prepared.ThreadID, func(evt subagent.TaskEvent) {
		emitter.Task(evt)
	}, func(item *clarification.Clarification) {
		emitter.Clarification(item)
	})
	var result *agent.RunResult
	if preparedExecution.Completed != nil {
		result = preparedExecution.Completed
	} else {
		eventsDone := make(chan struct{})
		go func() {
			defer close(eventsDone)
			for evt := range preparedExecution.Execution.Events() {
				emitter.Agent(evt)
			}
		}()

		result, err = preparedExecution.Execution.Run(ctx)
		<-eventsDone
		if err != nil {
			if isRunCanceledErr(err) {
				s.markRunCanceled(prepared.Run, prepared.ThreadID)
			} else {
				s.markRunError(prepared.Run, prepared.ThreadID, err)
			}
			return
		}
	}

	completed := s.finalizeCompletedRun(ctx, prepared, result)
	emitter.FinalMessages(prepared.ExistingMessages, result.Messages, result.Usage)
	emitter.Completion(completed, result.Usage, !remoteReplayedEnd)
}

func (s *Server) handleRunStream(w http.ResponseWriter, r *http.Request) {
	s.streamRecordedRun(w, r, "", r.PathValue("run_id"))
}

func (s *Server) handleRunGet(w http.ResponseWriter, r *http.Request) {
	run := s.threadRun("", r.PathValue("run_id"))
	if run == nil {
		writeDetailError(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, http.StatusOK, runResponse(run))
}

func (s *Server) handleThreadScopedRunGet(w http.ResponseWriter, r *http.Request) {
	run := s.threadRun(r.PathValue("thread_id"), r.PathValue("run_id"))
	if run == nil {
		writeDetailError(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, http.StatusOK, runResponse(run))
}

func (s *Server) handleThreadRunJoin(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	runID := r.PathValue("run_id")
	cancelOnDisconnect, _ := strconv.ParseBool(strings.TrimSpace(r.URL.Query().Get("cancel_on_disconnect")))

	run, found := s.waitForThreadRun(r.Context(), threadID, runID, cancelOnDisconnect)
	if !found {
		writeDetailError(w, http.StatusNotFound, "run not found")
		return
	}
	if run == nil {
		return
	}
	writeJSON(w, http.StatusOK, runResponse(run))
}

func (s *Server) handleThreadRunCancel(w http.ResponseWriter, r *http.Request) {
	resp, found, canceled := s.cancelThreadRun(r.PathValue("thread_id"), r.PathValue("run_id"))
	if !found {
		writeDetailError(w, http.StatusNotFound, "run not found")
		return
	}
	if !canceled {
		writeDetailError(w, http.StatusConflict, "run is not cancellable")
		return
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func (s *Server) handleThreadRunsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"runs": s.listThreadRunResponses(r.PathValue("thread_id"))})
}

func (s *Server) handleThreadRunStream(w http.ResponseWriter, r *http.Request) {
	s.streamRecordedRun(w, r, r.PathValue("thread_id"), r.PathValue("run_id"))
}

func (s *Server) handleThreadJoinStream(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("thread_id")
	if err := validateThreadID(threadID); err != nil {
		writeDetailError(w, http.StatusBadRequest, err.Error())
		return
	}
	selection := harnessruntime.NewQueryService(s.runtimeQueryAdapter()).SelectJoinRun(threadID)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeDetailError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	if !selection.ThreadFound {
		writeDetailError(w, http.StatusNotFound, "thread not found")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sse := newSSEWriter(w, flusher)
	w = sse
	flusher = sse

	if !selection.HasRun {
		fmt.Fprint(w, ": no active run\n\n")
		flusher.Flush()
		return
	}
	run := s.getRun(selection.Run.RunID)
	if run == nil {
		writeDetailError(w, http.StatusNotFound, "run not found")
		return
	}

	filter := newStreamModeFilter(streamModeFromQuery(r))
	streamer := s.newRunReplayStreamer(w, flusher, filter)
	replayedEnd := streamer.Replay(run)
	if replayedEnd || run.Status != "running" {
		return
	}
	streamer.Join(run)
}

func (s *Server) streamRecordedRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	run := s.getRun(runID)
	if run == nil {
		writeDetailError(w, http.StatusNotFound, "run not found")
		return
	}
	if threadID != "" && run.ThreadID != threadID {
		writeDetailError(w, http.StatusNotFound, "run not found")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Content-Location", fmt.Sprintf("/threads/%s/runs/%s", run.ThreadID, run.RunID))

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeDetailError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	s.newRunReplayStreamer(w, flusher, newStreamModeFilter(streamModeFromQuery(r))).Replay(run)
}

func (s *Server) usesRemoteDispatch() bool {
	node := s.ensureRuntimeSystem()
	if node == nil {
		return false
	}
	return node.Config.Transport.Backend == harnessruntime.WorkerTransportBackendRemote
}
