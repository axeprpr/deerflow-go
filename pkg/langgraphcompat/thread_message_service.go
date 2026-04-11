package langgraphcompat

import (
	"strconv"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func (s *Server) messagesToLangChain(messages []models.Message) []Message {
	result := make([]Message, 0, len(messages))
	for _, msg := range messages {
		msgType := "human"
		role := "human"

		switch msg.Role {
		case models.RoleAI:
			msgType = "ai"
			role = "assistant"
		case models.RoleSystem:
			msgType = "system"
			role = "system"
		case models.RoleTool:
			msgType = "tool"
			role = "tool"
		}

		converted := Message{
			Type:    msgType,
			ID:      msg.ID,
			Role:    role,
			Content: rewriteAssistantArtifactLinks(msg.SessionID, msg.Content, msg.Role),
		}
		if msg.Role == models.RoleHuman {
			if multi := decodeMultiContent(msg.Metadata); len(multi) > 0 {
				converted.Content = multi
			}
		}
		if usageMetadata := usageMetadataFromMessageMetadata(msg.Metadata); len(usageMetadata) > 0 {
			converted.UsageMetadata = usageMetadata
		}
		if additionalKwargs := additionalKwargsFromMessageMetadata(msg.Metadata); len(additionalKwargs) > 0 {
			converted.AdditionalKwargs = additionalKwargs
		}
		if len(msg.ToolCalls) > 0 {
			converted.ToolCalls = convertToolCalls(msg.ToolCalls)
		}
		if msg.ToolResult != nil {
			converted.Name = msg.ToolResult.ToolName
			converted.ToolCallID = msg.ToolResult.CallID
			converted.Status = toolMessageStatus(msg)
			converted.Data = map[string]any{
				"status":   string(msg.ToolResult.Status),
				"error":    msg.ToolResult.Error,
				"duration": msg.ToolResult.Duration.String(),
			}
			if len(msg.ToolResult.Data) > 0 {
				converted.Data["data"] = msg.ToolResult.Data
			}
			if converted.Content == "" {
				converted.Content = firstNonEmpty(msg.ToolResult.Content, msg.ToolResult.Error)
			}
		}
		result = append(result, converted)
	}
	return result
}

func additionalKwargsFromMessageMetadata(metadata map[string]string) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	if decoded := decodeAdditionalKwargs(metadata); len(decoded) > 0 {
		return decoded
	}
	out := make(map[string]any)
	for key, value := range metadata {
		if strings.HasPrefix(key, "usage_") || key == "message_status" || key == "multi_content" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func usageMetadataFromMessageMetadata(metadata map[string]string) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		raw, ok := metadata["usage_"+key]
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
			out[key] = n
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toolMessageStatus(msg models.Message) string {
	if msg.Metadata != nil {
		if status := strings.TrimSpace(msg.Metadata["message_status"]); status != "" {
			return status
		}
	}
	if msg.ToolResult == nil {
		return ""
	}
	switch msg.ToolResult.Status {
	case models.CallStatusCompleted:
		return "success"
	case models.CallStatusFailed:
		return "error"
	case models.CallStatusRunning:
		return "running"
	case models.CallStatusPending:
		return "pending"
	default:
		return ""
	}
}

func convertToolCalls(calls []models.ToolCall) []ToolCall {
	out := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		name := call.Name
		args := call.Arguments
		if normalizedArgs, ok := normalizePresentFileCall(call); ok {
			name = "present_files"
			args = normalizedArgs
		}
		out = append(out, ToolCall{
			ID:   call.ID,
			Name: name,
			Args: args,
		})
	}
	return out
}

func normalizePresentFileCall(call models.ToolCall) (map[string]any, bool) {
	if call.Name != "present_file" {
		return nil, false
	}
	path, _ := call.Arguments["path"].(string)
	if strings.TrimSpace(path) == "" {
		return nil, false
	}
	return map[string]any{
		"filepaths": []string{path},
	}, true
}

func stringMetadataToAny(metadata map[string]string) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = value
	}
	return out
}
