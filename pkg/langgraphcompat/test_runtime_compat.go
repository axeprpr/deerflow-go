package langgraphcompat

import (
	"encoding/json"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func applyThreadStateUpdate(session *Session, values, metadata map[string]any) {
	if session == nil {
		return
	}
	if session.Values == nil {
		session.Values = map[string]any{}
	}
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	for key, value := range values {
		session.Values[key] = value
	}
	applyThreadMetadata(session, metadata)
}

func filterTransientMessages(messages []models.Message) []models.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]models.Message, 0, len(messages))
	for _, msg := range messages {
		if isTransientTodoReminderMessage(msg) || isInjectedSummaryMessage(msg) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(msg.Metadata["transient_viewed_images"]), "true") {
			continue
		}
		msg.Metadata = stripTransientUploadImageURLs(msg.Metadata)
		out = append(out, msg)
	}
	return out
}

func stripTransientUploadImageURLs(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return metadata
	}
	multi := decodeMultiContent(metadata)
	if len(multi) == 0 {
		return metadata
	}
	textOnly := make([]map[string]any, 0, len(multi))
	for _, item := range multi {
		if asString(item["type"]) == "image_url" {
			if image, ok := item["image_url"].(map[string]any); ok {
				if strings.HasPrefix(asString(image["url"]), "data:") {
					continue
				}
			}
		}
		textOnly = append(textOnly, item)
	}
	if len(textOnly) == len(multi) {
		return metadata
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	if encoded, err := json.Marshal(textOnly); err == nil {
		cloned["multi_content"] = string(encoded)
	}
	return cloned
}

func decodeMultiContent(metadata map[string]string) []map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	raw := strings.TrimSpace(metadata["multi_content"])
	if raw == "" {
		return nil
	}
	var multi []map[string]any
	if err := json.Unmarshal([]byte(raw), &multi); err != nil {
		return nil
	}
	return multi
}

func decodeAdditionalKwargs(metadata map[string]string) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	raw := strings.TrimSpace(metadata["additional_kwargs"])
	if raw == "" {
		return nil
	}
	var kwargs map[string]any
	if err := json.Unmarshal([]byte(raw), &kwargs); err != nil {
		return nil
	}
	return kwargs
}
