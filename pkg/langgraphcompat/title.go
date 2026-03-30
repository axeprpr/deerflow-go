package langgraphcompat

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

const (
	defaultTitleMaxWords = 6
	defaultTitleMaxChars = 60
	titlePromptMaxChars  = 500
)

func (s *Server) maybeGenerateThreadTitle(ctx context.Context, threadID string, modelName string, messages []models.Message) {
	if s == nil {
		return
	}
	if !s.shouldGenerateThreadTitle(threadID, messages) {
		return
	}

	title := s.generateThreadTitle(ctx, modelName, messages)
	if title == "" {
		return
	}
	s.setThreadMetadata(threadID, "title", title)
}

func (s *Server) shouldGenerateThreadTitle(threadID string, messages []models.Message) bool {
	if strings.TrimSpace(threadID) == "" {
		return false
	}

	s.sessionsMu.RLock()
	session, exists := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if !exists {
		return false
	}
	if session.Metadata != nil && strings.TrimSpace(stringValue(session.Metadata["title"])) != "" {
		return false
	}

	userCount := 0
	assistantCount := 0
	for _, msg := range messages {
		switch msg.Role {
		case models.RoleHuman:
			if strings.TrimSpace(msg.Content) != "" {
				userCount++
			}
		case models.RoleAI:
			if strings.TrimSpace(msg.Content) != "" {
				assistantCount++
			}
		}
	}

	return userCount == 1 && assistantCount >= 1
}

func (s *Server) generateThreadTitle(ctx context.Context, modelName string, messages []models.Message) string {
	userMsg, assistantMsg := firstExchange(messages)
	fallback := fallbackTitle(userMsg)
	if strings.TrimSpace(userMsg) == "" {
		return fallback
	}

	provider := s.llmProvider
	if provider == nil {
		return fallback
	}

	prompt := buildTitlePrompt(userMsg, assistantMsg)
	maxTokens := 24
	resp, err := provider.Chat(ctx, llm.ChatRequest{
		Model: resolveTitleModel(modelName, s.defaultModel),
		Messages: []models.Message{
			{
				ID:        "title-user",
				SessionID: "title",
				Role:      models.RoleHuman,
				Content:   prompt,
			},
		},
		MaxTokens: &maxTokens,
	})
	if err != nil {
		return fallback
	}

	title := sanitizeTitle(resp.Message.Content)
	if title == "" {
		return fallback
	}
	return title
}

func firstExchange(messages []models.Message) (string, string) {
	var userMsg string
	var assistantMsg string
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		switch msg.Role {
		case models.RoleHuman:
			if userMsg == "" {
				userMsg = content
			}
		case models.RoleAI:
			if assistantMsg == "" {
				assistantMsg = content
			}
		}
		if userMsg != "" && assistantMsg != "" {
			return userMsg, assistantMsg
		}
	}
	return userMsg, assistantMsg
}

func buildTitlePrompt(userMsg string, assistantMsg string) string {
	return strings.TrimSpace("Generate a concise conversation title in at most 6 words. " +
		"Return only the title without quotes or punctuation wrappers.\n\n" +
		"User message:\n" + truncateRunes(strings.TrimSpace(userMsg), titlePromptMaxChars) +
		"\n\nAssistant reply:\n" + truncateRunes(strings.TrimSpace(assistantMsg), titlePromptMaxChars))
}

func sanitizeTitle(raw string) string {
	title := strings.TrimSpace(raw)
	title = strings.Trim(title, "\"'`")
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return ""
	}

	words := strings.Fields(title)
	if len(words) > defaultTitleMaxWords {
		title = strings.Join(words[:defaultTitleMaxWords], " ")
	}
	if utf8.RuneCountInString(title) > defaultTitleMaxChars {
		title = truncateRunes(title, defaultTitleMaxChars)
	}
	return strings.TrimSpace(title)
}

func fallbackTitle(userMsg string) string {
	title := strings.Join(strings.Fields(strings.TrimSpace(userMsg)), " ")
	if title == "" {
		return "New Conversation"
	}

	words := strings.Fields(title)
	if len(words) > defaultTitleMaxWords {
		title = strings.Join(words[:defaultTitleMaxWords], " ")
	}
	if utf8.RuneCountInString(title) > defaultTitleMaxChars {
		title = truncateRunes(title, defaultTitleMaxChars)
	}
	return strings.TrimSpace(title)
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= limit {
		return value
	}

	runes := []rune(value)
	return string(runes[:limit])
}

func resolveTitleModel(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return "gpt-4.1-mini"
}
