package llm

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
	einoSchema "github.com/cloudwego/eino/schema"
)

func TestToEinoMessageUsesUserInputMultiContent(t *testing.T) {
	msg := models.Message{
		ID:        "m1",
		SessionID: "s1",
		Role:      models.RoleHuman,
		Content:   "fallback",
		Metadata: map[string]string{
			"multi_content": `[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]`,
		},
	}

	out := toEinoMessage(msg)
	if out.Role != einoSchema.User {
		t.Fatalf("role=%s", out.Role)
	}
	if out.Content != "" {
		t.Fatalf("content=%q want empty", out.Content)
	}
	if len(out.UserInputMultiContent) != 2 {
		t.Fatalf("parts=%d want 2", len(out.UserInputMultiContent))
	}
	if out.UserInputMultiContent[1].Type != einoSchema.ChatMessagePartTypeImageURL {
		t.Fatalf("part type=%s", out.UserInputMultiContent[1].Type)
	}
}

func TestToEinoMessageKeepsPlainTextForTextOnlyMultiContent(t *testing.T) {
	msg := models.Message{
		ID:        "m1",
		SessionID: "s1",
		Role:      models.RoleHuman,
		Content:   "帮我生成一个小鱼游泳的页面",
		Metadata: map[string]string{
			"multi_content": `[{"type":"text","text":"帮我生成一个小鱼游泳的页面"}]`,
		},
	}

	out := toEinoMessage(msg)
	if out.Role != einoSchema.User {
		t.Fatalf("role=%s", out.Role)
	}
	if out.Content != "帮我生成一个小鱼游泳的页面" {
		t.Fatalf("content=%q", out.Content)
	}
	if len(out.UserInputMultiContent) != 0 {
		t.Fatalf("parts=%d want 0", len(out.UserInputMultiContent))
	}
}
