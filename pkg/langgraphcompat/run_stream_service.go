package langgraphcompat

import (
	"net/http"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
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

func (s *Server) newRunStreamEmitter(w http.ResponseWriter, flusher http.Flusher, run *Run, filter streamModeFilter) runStreamEmitter {
	return runStreamEmitter{
		server:  s,
		w:       w,
		flusher: flusher,
		run:     run,
		filter:  filter,
	}
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
