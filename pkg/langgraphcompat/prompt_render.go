package langgraphcompat

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func renderMessagesForPrompt(messages []models.Message) string {
	if len(messages) == 0 {
		return "(no messages)"
	}

	var b strings.Builder
	for _, msg := range messages {
		role := string(msg.Role)
		content := strings.TrimSpace(msg.Content)
		if content == "" && msg.ToolResult != nil {
			content = strings.TrimSpace(msg.ToolResult.Content)
			if content == "" {
				content = strings.TrimSpace(msg.ToolResult.Error)
			}
		}
		if content == "" {
			continue
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteString("\n")
	}

	out := strings.TrimSpace(b.String())
	if out == "" {
		return "(no textual messages)"
	}
	return out
}
