package models

import (
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
