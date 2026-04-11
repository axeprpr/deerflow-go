package harness

import (
	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

// Features describes which harness-managed capabilities are intended to be
// active for a runtime. It mirrors upstream DeerFlow's feature-oriented
// assembly shape without forcing the runtime to adopt middleware semantics yet.
type Features struct {
	Sandbox       bool
	Memory        bool
	Summarization bool
	Subagent      bool
	Vision        bool
	AutoTitle     bool
	Guardrail     bool
}

// Middleware is a placeholder boundary for future harness-managed lifecycle
// hooks. The current runtime does not execute generic middlewares yet, but the
// abstraction is introduced now so feature ownership can move out of compat.
type Middleware interface {
	Name() string
}

// RuntimeDeps captures the stable dependencies needed to assemble an agent.
type RuntimeDeps struct {
	LLMProvider     llm.LLMProvider
	Tools           *tools.Registry
	DefaultMaxTurns int
	SandboxProvider SandboxProvider
}

// AgentRequest describes one harness assembly request.
type AgentRequest struct {
	Config      agent.AgentConfig
	Features    Features
	Middlewares []Middleware
}
