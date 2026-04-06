package langgraphcompat

const defaultGatewaySubagentMaxConcurrent = 4

func boolFromAny(value any) bool {
	return boolValue(value)
}

func defaultThreadConfig(threadID string) map[string]any {
	configurable := map[string]any{}
	if threadID != "" {
		configurable["thread_id"] = threadID
	}
	return configurable
}
