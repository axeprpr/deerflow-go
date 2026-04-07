package langgraphcompat

import (
	"os"
	"strings"
)

func (s *Server) backgroundReasoningEffort(modelName string) string {
	resolved := resolveTitleModel(modelName, s.defaultModel)
	if resolved == "" {
		return ""
	}

	if configured := gatewayModelsFromEnv(os.Getenv("DEERFLOW_MODELS_JSON")); len(configured) > 0 {
		for _, model := range configured {
			if model.Name == resolved || model.Model == resolved || model.ID == resolved {
				if model.SupportsReasoningEffort {
					return "minimal"
				}
				return ""
			}
		}
	}

	s.uiStateMu.RLock()
	model, ok := s.findModelLocked(resolved)
	s.uiStateMu.RUnlock()
	if ok {
		if model.SupportsReasoningEffort {
			return "minimal"
		}
		return ""
	}

	if supportsReasoningEffortByModelName(resolved) {
		return "minimal"
	}
	return ""
}

func supportsReasoningEffortByModelName(name string) bool {
	value := strings.ToLower(strings.TrimSpace(name))
	if value == "" {
		return false
	}
	for _, token := range []string{
		"gpt-5",
		"o1",
		"o3",
		"o4",
		"r1",
		"reasoner",
	} {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}
