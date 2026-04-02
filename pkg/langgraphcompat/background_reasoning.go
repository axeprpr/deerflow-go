package langgraphcompat

func (s *Server) backgroundReasoningEffort(modelName string) string {
	resolved := resolveTitleModel(modelName, s.defaultModel)
	if resolved == "" {
		return ""
	}

	if model, ok := findConfiguredGatewayModel(s.defaultModel, resolved); ok {
		if model.SupportsReasoningEffort {
			return "minimal"
		}
		return ""
	}

	_, supportsReasoning := inferGatewayModelCapabilities(resolved)
	if supportsReasoning {
		return "minimal"
	}
	return ""
}
