package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

var normalizedToolCallSeq uint64

// NormalizeToolCall repairs recoverable tool call fields so they are safe to
// persist in history and send back to the model.
func NormalizeToolCall(call ToolCall) (ToolCall, bool) {
	call.ID = strings.TrimSpace(call.ID)
	call.Name = strings.TrimSpace(call.Name)
	if call.Name == "" {
		return ToolCall{}, false
	}
	call.Arguments = normalizeToolArguments(call.Arguments)
	if call.ID == "" {
		call.ID = newNormalizedToolCallID(call.Name)
	}
	if call.Status == "" {
		call.Status = CallStatusPending
	}
	if err := call.Validate(); err != nil {
		return ToolCall{}, false
	}
	return call, true
}

// NormalizeToolResult repairs recoverable tool result fields so they are safe
// to persist in history and send back to the model.
func NormalizeToolResult(result ToolResult) (ToolResult, bool) {
	result.CallID = strings.TrimSpace(result.CallID)
	result.ToolName = strings.TrimSpace(result.ToolName)
	if result.CallID == "" || result.ToolName == "" {
		return ToolResult{}, false
	}
	if result.Status == "" {
		if strings.TrimSpace(result.Error) != "" {
			result.Status = CallStatusFailed
		} else {
			result.Status = CallStatusCompleted
		}
	}
	if err := result.Validate(); err != nil {
		return ToolResult{}, false
	}
	return result, true
}

func newNormalizedToolCallID(name string) string {
	seq := atomic.AddUint64(&normalizedToolCallSeq, 1)
	return fmt.Sprintf("%s_norm_%d_%d", name, time.Now().UTC().UnixNano(), seq)
}

func normalizeToolArguments(args map[string]any) map[string]any {
	if len(args) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(args))
	for key, value := range args {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		out[trimmed] = normalizeToolArgumentValue(value)
	}
	if len(out) == 0 {
		return map[string]any{}
	}
	return out
}

func normalizeToolArgumentValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return normalizeToolArguments(typed)
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			strKey := strings.TrimSpace(fmt.Sprint(key))
			if strKey == "" {
				continue
			}
			out[strKey] = normalizeToolArgumentValue(nested)
		}
		if len(out) == 0 {
			return map[string]any{}
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, nested := range typed {
			out = append(out, normalizeToolArgumentValue(nested))
		}
		return out
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(typed, &decoded); err != nil {
			return strings.TrimSpace(string(typed))
		}
		return normalizeToolArgumentValue(decoded)
	default:
		return value
	}
}
