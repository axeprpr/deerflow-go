package llm

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

var thinkTagRE = regexp.MustCompile(`(?is)<think>\s*([\s\S]*?)\s*</think>`)

// NormalizeAssistantMessage converts inline reasoning blocks into the metadata
// shape expected by deerflow-ui and prevents empty assistant bubbles when a
// model puts the entire answer inside <think>...</think>.
func NormalizeAssistantMessage(msg models.Message) models.Message {
	if msg.Role != models.RoleAI {
		return msg
	}

	cleaned, reasoning := stripInlineThinkTags(msg.Content)
	if reasoning == "" {
		return msg
	}

	if cleaned != "" {
		msg.Content = cleaned
		msg.Metadata = mergeAdditionalKwargsMetadata(msg.Metadata, reasoning)
		return msg
	}

	msg.Content = ""
	msg.Metadata = mergeAdditionalKwargsMetadata(msg.Metadata, reasoning)
	return msg
}

func HasReasoningContent(msg models.Message) bool {
	if msg.Role != models.RoleAI || len(msg.Metadata) == 0 {
		return false
	}

	raw := strings.TrimSpace(msg.Metadata["additional_kwargs"])
	if raw == "" {
		return false
	}

	var additional map[string]any
	if err := json.Unmarshal([]byte(raw), &additional); err != nil {
		return false
	}

	reasoning, _ := additional["reasoning_content"].(string)
	return strings.TrimSpace(reasoning) != ""
}

func stripInlineThinkTags(content string) (string, string) {
	if strings.TrimSpace(content) == "" {
		return "", ""
	}

	reasoningParts := make([]string, 0, 1)
	cleaned := strings.TrimSpace(thinkTagRE.ReplaceAllStringFunc(content, func(match string) string {
		submatches := thinkTagRE.FindStringSubmatch(match)
		if len(submatches) > 1 {
			if normalized := strings.TrimSpace(submatches[1]); normalized != "" {
				reasoningParts = append(reasoningParts, normalized)
			}
		}
		return ""
	}))
	return cleaned, strings.Join(reasoningParts, "\n\n")
}

func mergeAdditionalKwargsMetadata(metadata map[string]string, reasoning string) map[string]string {
	if strings.TrimSpace(reasoning) == "" {
		return metadata
	}

	out := cloneMetadata(metadata)
	additional := map[string]any{}
	if raw := strings.TrimSpace(out["additional_kwargs"]); raw != "" {
		_ = json.Unmarshal([]byte(raw), &additional)
	}

	additional["reasoning_content"] = mergeReasoning(additional["reasoning_content"], reasoning)
	if raw, err := json.Marshal(additional); err == nil {
		out["additional_kwargs"] = string(raw)
	}
	return out
}

func withoutReasoningContent(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return metadata
	}

	out := cloneMetadata(metadata)
	raw := strings.TrimSpace(out["additional_kwargs"])
	if raw == "" {
		return out
	}

	additional := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &additional); err != nil {
		return out
	}

	delete(additional, "reasoning_content")
	if len(additional) == 0 {
		delete(out, "additional_kwargs")
		return out
	}
	if encoded, err := json.Marshal(additional); err == nil {
		out["additional_kwargs"] = string(encoded)
	}
	return out
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(metadata))
	for k, v := range metadata {
		out[k] = v
	}
	return out
}

func mergeReasoning(existing any, reasoning string) string {
	parts := make([]string, 0, 2)
	appendUnique := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, part := range parts {
			if part == value {
				return
			}
		}
		parts = append(parts, value)
	}

	if current, ok := existing.(string); ok {
		appendUnique(current)
	}
	appendUnique(reasoning)
	return strings.Join(parts, "\n\n")
}
