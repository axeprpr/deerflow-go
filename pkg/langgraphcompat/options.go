package langgraphcompat

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/llm"
)

func WithRuntimeNodeConfig(config harnessruntime.RuntimeNodeConfig) ServerOption {
	return func(s *Server) {
		if s == nil {
			return
		}
		s.runtimeNode = config
		if config.Sandbox.Name != "" {
			s.sandboxName = config.Sandbox.Name
		}
		if config.Sandbox.Root != "" {
			s.sandboxRoot = config.Sandbox.Root
		}
	}
}

func WithMaxTurns(maxTurns int) ServerOption {
	return func(s *Server) {
		if s == nil || maxTurns <= 0 {
			return
		}
		s.maxTurns = maxTurns
	}
}

func WithLLMProvider(provider llm.LLMProvider) ServerOption {
	return func(s *Server) {
		if s == nil || provider == nil {
			return
		}
		s.llmProvider = provider
	}
}

func WithLLMProviderName(name string) ServerOption {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	return WithLLMProvider(llm.NewProvider(name))
}
