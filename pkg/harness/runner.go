package harness

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

// RunRequest describes one runtime execution request.
type RunRequest struct {
	Agent     AgentRequest
	SessionID string
	Messages  []models.Message
}

// Execution owns one prepared agent run. Compat layers may observe Events, but
// they should not construct agents or call agent.Run directly.
type Execution struct {
	agent     *agent.Agent
	sessionID string
	messages  []models.Message
}

func (e *Execution) Events() <-chan agent.AgentEvent {
	if e == nil || e.agent == nil {
		return nil
	}
	return e.agent.Events()
}

func (e *Execution) Run(ctx context.Context) (*agent.RunResult, error) {
	if e == nil || e.agent == nil {
		return nil, nil
	}
	return e.agent.Run(ctx, e.sessionID, e.messages)
}

// Runner owns the create-agent + execute boundary for one runtime.
type Runner struct {
	factory *Factory
}

func NewRunner(factory *Factory) *Runner {
	return &Runner{factory: factory}
}

func (r *Runner) Prepare(req RunRequest) (*Execution, error) {
	if r == nil || r.factory == nil {
		return &Execution{
			agent:     agent.New(req.Agent.Spec.AgentConfig()),
			sessionID: req.SessionID,
			messages:  append([]models.Message(nil), req.Messages...),
		}, nil
	}
	runAgent, err := r.factory.NewAgent(req.Agent)
	if err != nil {
		return nil, err
	}
	return &Execution{
		agent:     runAgent,
		sessionID: req.SessionID,
		messages:  append([]models.Message(nil), req.Messages...),
	}, nil
}
