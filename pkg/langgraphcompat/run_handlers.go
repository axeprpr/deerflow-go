package langgraphcompat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

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
	prepared := s.prepareRunRequest(threadID, req)
	execution, err := s.buildRunExecution(r.Context(), prepared, req)
	if err != nil {
		s.markRunError(prepared.Run, prepared.ThreadID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ctx := s.bindRunContext(r.Context(), prepared.ThreadID, func(evt subagent.TaskEvent) {}, func(item *clarification.Clarification) {})

	result, err := execution.Run(ctx)
	if err != nil {
		s.markRunError(prepared.Run, prepared.ThreadID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
	prepared := s.prepareRunRequest(routeThreadID, req)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
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

	execution, err := s.buildRunExecution(r.Context(), prepared, req)
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
	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		for evt := range execution.Events() {
			emitter.Agent(evt)
		}
	}()

	result, err := execution.Run(ctx)
	<-eventsDone
	if err != nil {
		if isRunCanceledErr(err) {
			s.markRunCanceled(prepared.Run, prepared.ThreadID)
		} else {
			s.markRunError(prepared.Run, prepared.ThreadID, err)
		}
		return
	}

	completed := s.finalizeCompletedRun(ctx, prepared, result)
	emitter.FinalMessages(prepared.ExistingMessages, result.Messages, result.Usage)
	emitter.Completion(completed, result.Usage)
}

func (s *Server) handleRunStream(w http.ResponseWriter, r *http.Request) {
	s.streamRecordedRun(w, r, "", r.PathValue("run_id"))
}

func (s *Server) handleRunGet(w http.ResponseWriter, r *http.Request) {
	run := s.threadRun("", r.PathValue("run_id"))
	if run == nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, runResponse(run))
}

func (s *Server) handleThreadScopedRunGet(w http.ResponseWriter, r *http.Request) {
	run := s.threadRun(r.PathValue("thread_id"), r.PathValue("run_id"))
	if run == nil {
		http.Error(w, "run not found", http.StatusNotFound)
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
		http.Error(w, "run not found", http.StatusNotFound)
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
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	if !canceled {
		http.Error(w, "run is not cancellable", http.StatusConflict)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	selection := harnessruntime.NewQueryService(s.runtimeQueryAdapter()).SelectJoinRun(threadID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	sse := newSSEWriter(w, flusher)
	w = sse
	flusher = sse

	if !selection.ThreadFound {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}
	if !selection.HasRun {
		fmt.Fprint(w, ": no active run\n\n")
		flusher.Flush()
		return
	}
	run := s.getRun(selection.Run.RunID)
	if run == nil {
		http.Error(w, "run not found", http.StatusNotFound)
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

	s.newRunReplayStreamer(w, flusher, newStreamModeFilter(streamModeFromQuery(r))).Replay(run)
}
