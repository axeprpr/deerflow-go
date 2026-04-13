package harnessruntime

import (
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

// PortableAgentSpec is the worker-transport-safe subset of harness.AgentSpec.
// Runtime-local objects must be rehydrated by a worker-side resolver.
type PortableAgentSpec struct {
	ExecutionMode          string
	AgentType              agent.AgentType
	MaxTurns               int
	MaxConcurrentSubagents int
	Model                  string
	ReasoningEffort        string
	SystemPrompt           string
	Temperature            *float64
	MaxTokens              *int
	RequestTimeout         time.Duration
	GuardrailFailClosed    *bool
	GuardrailPassport      string
}

func PortableAgentSpecFromAgentSpec(spec harness.AgentSpec) PortableAgentSpec {
	return PortableAgentSpec{
		ExecutionMode:          spec.ExecutionMode,
		AgentType:              spec.AgentType,
		MaxTurns:               spec.MaxTurns,
		MaxConcurrentSubagents: spec.MaxConcurrentSubagents,
		Model:                  spec.Model,
		ReasoningEffort:        spec.ReasoningEffort,
		SystemPrompt:           spec.SystemPrompt,
		Temperature:            spec.Temperature,
		MaxTokens:              spec.MaxTokens,
		RequestTimeout:         spec.RequestTimeout,
		GuardrailFailClosed:    spec.GuardrailFailClosed,
		GuardrailPassport:      spec.GuardrailPassport,
	}
}

func (s PortableAgentSpec) AgentSpec() harness.AgentSpec {
	return harness.AgentSpec{
		ExecutionMode:          s.ExecutionMode,
		AgentType:              s.AgentType,
		MaxTurns:               s.MaxTurns,
		MaxConcurrentSubagents: s.MaxConcurrentSubagents,
		Model:                  s.Model,
		ReasoningEffort:        s.ReasoningEffort,
		SystemPrompt:           s.SystemPrompt,
		Temperature:            s.Temperature,
		MaxTokens:              s.MaxTokens,
		RequestTimeout:         s.RequestTimeout,
		GuardrailFailClosed:    s.GuardrailFailClosed,
		GuardrailPassport:      s.GuardrailPassport,
	}
}

type WorkerSpecRuntime interface {
	ResolveWorkerAgentSpec(threadID string, spec PortableAgentSpec) harness.AgentSpec
}

type WorkerContextRuntime interface {
	ResolveWorkerContextSpec(threadID string) harness.ContextSpec
}
