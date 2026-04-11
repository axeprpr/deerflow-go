package langgraphcompat

import (
	"encoding/json"
	"strconv"
	"strings"
)

func mapFromAny(value any) map[string]any {
	if value == nil {
		return nil
	}
	out, _ := value.(map[string]any)
	return out
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	default:
		return 0
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func stringSliceFromAny(value any) []string {
	items, _ := value.([]any)
	if len(items) == 0 {
		if direct, ok := value.([]string); ok {
			return append([]string(nil), direct...)
		}
		if text := strings.TrimSpace(stringFromAny(value)); text != "" {
			return []string{text}
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := stringFromAny(item); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func stringMapFromAny(value any) map[string]string {
	switch typed := value.(type) {
	case map[string]string:
		return mapsClone(typed)
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, raw := range typed {
			out[key] = stringFromAny(raw)
		}
		return out
	default:
		return nil
	}
}

func mapsClone(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	default:
		return 0
	}
}

func floatValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		n, _ := typed.Float64()
		return n
	case string:
		n, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return n
	default:
		return 0
	}
}

func firstNonEmptySlice[T any](primary []T, fallback []T) []T {
	if len(primary) > 0 {
		return primary
	}
	if len(fallback) > 0 {
		return fallback
	}
	return primary
}

func stringsFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(stringFromAny(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
