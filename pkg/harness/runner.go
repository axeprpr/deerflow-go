package harness

import (
	"context"
	"time"

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
	heartbeat func() error
	release   func() error
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
	if e.release != nil {
		defer func() {
			_ = e.release()
		}()
	}
	if e.heartbeat != nil {
		if err := e.heartbeat(); err != nil {
			return nil, err
		}
	}
	if e.heartbeat != nil {
		done := make(chan struct{})
		defer close(done)
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					_ = e.heartbeat()
				}
			}
		}()
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
	prepared, err := r.factory.PrepareAgent(req.Agent)
	if err != nil {
		return nil, err
	}
	return &Execution{
		agent:     prepared.Agent,
		sessionID: req.SessionID,
		messages:  append([]models.Message(nil), req.Messages...),
		heartbeat: prepared.Heartbeat,
		release:   prepared.Release,
	}, nil
}
