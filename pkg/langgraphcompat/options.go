package langgraphcompat

import "github.com/axeprpr/deerflow-go/pkg/harnessruntime"

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
