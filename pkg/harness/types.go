package harness

import (
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/guardrails"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

// FeatureSet describes which harness-managed capabilities are intended to be
// active for a runtime. It mirrors upstream DeerFlow's feature-oriented
// assembly shape without forcing the runtime to adopt middleware semantics yet.
type FeatureSet struct {
	Sandbox       bool
	Memory        bool
	Summarization bool
	Subagent      bool
	Vision        bool
	AutoTitle     bool
	Guardrail     bool
}

// AgentSpec is the harness-facing runtime assembly contract. Compat and
// transport layers should produce this spec instead of wiring raw AgentConfig
// values directly.
type AgentSpec struct {
	DeferredTools          []models.Tool
	PresentFiles           *tools.PresentFileRegistry
	AgentType              agent.AgentType
	MaxTurns               int
	MaxConcurrentSubagents int
	Model                  string
	ReasoningEffort        string
	SystemPrompt           string
	Temperature            *float64
	MaxTokens              *int
	Sandbox                *sandbox.Sandbox
	RequestTimeout         time.Duration
	GuardrailProvider      guardrails.Provider
	GuardrailFailClosed    *bool
	GuardrailPassport      string
}

func (s AgentSpec) AgentConfig() agent.AgentConfig {
	return agent.AgentConfig{
		DeferredTools:          append([]models.Tool(nil), s.DeferredTools...),
		PresentFiles:           s.PresentFiles,
		AgentType:              s.AgentType,
		MaxTurns:               s.MaxTurns,
		MaxConcurrentSubagents: s.MaxConcurrentSubagents,
		Model:                  s.Model,
		ReasoningEffort:        s.ReasoningEffort,
		SystemPrompt:           s.SystemPrompt,
		Temperature:            s.Temperature,
		MaxTokens:              s.MaxTokens,
		Sandbox:                s.Sandbox,
		RequestTimeout:         s.RequestTimeout,
		GuardrailProvider:      s.GuardrailProvider,
		GuardrailFailClosed:    s.GuardrailFailClosed,
		GuardrailPassport:      s.GuardrailPassport,
	}
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
	ToolRuntime     ToolRuntime
	DefaultMaxTurns int
	SandboxRuntime  SandboxRuntime
	SandboxProvider SandboxProvider
}

// AgentRequest describes one harness assembly request.
type AgentRequest struct {
	Spec        AgentSpec
	Features    FeatureSet
	Middlewares []Middleware
}
