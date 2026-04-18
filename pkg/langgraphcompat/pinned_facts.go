package langgraphcompat

import "strings"

const pinnedFactsValueKey = "pinned_facts"

func pinnedFactsFromValues(values map[string]any) map[string]string {
	if len(values) == 0 {
		return nil
	}
	return pinnedFactsFromAny(values[pinnedFactsValueKey])
}

func pinnedFactsFromAny(raw any) map[string]string {
	switch typed := raw.(type) {
	case map[string]string:
		return normalizePinnedFacts(typed)
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, value := range typed {
			key = strings.TrimSpace(key)
			val := strings.TrimSpace(stringFromAny(value))
			if key == "" || val == "" {
				continue
			}
			out[key] = val
		}
		return normalizePinnedFacts(out)
	default:
		return nil
	}
}

func normalizePinnedFacts(raw map[string]string) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func pinnedFactsToAny(raw map[string]string) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	normalized := normalizePinnedFacts(raw)
	if len(normalized) == 0 {
		return nil
	}
	out := make(map[string]any, len(normalized))
	for key, value := range normalized {
		out[key] = value
	}
	return out
}
