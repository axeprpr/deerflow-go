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
	PinnedFacts            map[string]string
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
		PinnedFacts:            copyPortableStringMap(spec.PinnedFacts),
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
		PinnedFacts:            copyPortableStringMap(s.PinnedFacts),
		Temperature:            s.Temperature,
		MaxTokens:              s.MaxTokens,
		RequestTimeout:         s.RequestTimeout,
		GuardrailFailClosed:    s.GuardrailFailClosed,
		GuardrailPassport:      s.GuardrailPassport,
	}
}

func copyPortableStringMap(raw map[string]string) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		out[key] = value
	}
	return out
}

type WorkerSpecRuntime interface {
	ResolveWorkerAgentSpec(threadID string, spec PortableAgentSpec) harness.AgentSpec
}

type WorkerContextRuntime interface {
	ResolveWorkerContextSpec(threadID string) harness.ContextSpec
}
