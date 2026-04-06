package langgraphcompat

import (
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestMessagesToLangChainRewritesAssistantArtifactLinks(t *testing.T) {
	s, _ := newCompatTestServer(t)
	messages := []models.Message{
		{
			ID:        "ai-artifact-links",
			SessionID: "thread-links",
			Role:      models.RoleAI,
			Content:   "Open [report](/mnt/user-data/outputs/final report.md) and ![chart](/mnt/user-data/outputs/chart.png)",
			Metadata: map[string]string{
				"multi_content": `[{"type":"text","text":"Open [report](/mnt/user-data/outputs/final report.md)"},{"type":"image_url","image_url":{"url":"/mnt/user-data/outputs/chart.png"}}]`,
			},
		},
	}

	got := s.messagesToLangChain(messages)
	if len(got) != 1 {
		t.Fatalf("messages=%d want=1", len(got))
	}

	content, ok := got[0].Content.(string)
	if !ok {
		t.Fatalf("content type=%T want string", got[0].Content)
	}

	if !strings.Contains(content, "/api/threads/thread-links/artifacts/mnt/user-data/outputs/final%20report.md") {
		t.Fatalf("content=%q missing rewritten artifact url", got[0].Content)
	}
	if !strings.Contains(content, "/mnt/user-data/outputs/chart.png") {
		t.Fatalf("content=%q missing image reference", got[0].Content)
	}
}

func TestMessagesToLangChainRewritesAssistantPlainTextArtifactLinks(t *testing.T) {
	s, _ := newCompatTestServer(t)
	messages := []models.Message{
		{
			ID:        "ai-plain-artifact-links",
			SessionID: "thread-links",
			Role:      models.RoleAI,
			Content:   "Open /mnt/user-data/outputs/final report.md for the latest summary.",
		},
	}

	got := s.messagesToLangChain(messages)
	if len(got) != 1 {
		t.Fatalf("messages=%d want=1", len(got))
	}

	content, ok := got[0].Content.(string)
	if !ok {
		t.Fatalf("content type=%T want string", got[0].Content)
	}

	if !strings.Contains(content, "/api/threads/thread-links/artifacts/mnt/user-data/outputs/final%20report.md") {
		t.Fatalf("content=%q missing rewritten artifact url", got[0].Content)
	}
}
