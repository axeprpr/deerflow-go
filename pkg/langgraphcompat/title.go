package langgraphcompat

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

const (
	titleFallbackMaxChars = 50
	titlePromptMaxChars   = 500
)

func (s *Server) maybeGenerateThreadTitle(ctx context.Context, threadID string, modelName string, messages []models.Message) {
	if s == nil {
		return
	}
	cfg := loadTitleConfig()
	if !cfg.Enabled {
		return
	}
	if !s.shouldGenerateThreadTitle(threadID, messages) {
		return
	}

	title := s.generateThreadTitle(ctx, modelName, messages, cfg)
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

func (s *Server) generateThreadTitle(ctx context.Context, modelName string, messages []models.Message, cfg titleConfig) string {
	userMsg, assistantMsg := firstExchange(messages)
	fallback := fallbackTitleWithConfig(userMsg, cfg)
	if strings.TrimSpace(userMsg) == "" {
		return fallback
	}

	provider := s.llmProvider
	if provider == nil {
		return fallback
	}

	maxTokens := 24
	resolvedModel := resolveTitleModel(cfg.Model, modelName, s.defaultModel)
	resp, err := provider.Chat(ctx, llm.ChatRequest{
		Model:           resolvedModel,
		ReasoningEffort: s.backgroundReasoningEffort(resolvedModel),
		Messages: []models.Message{
			{
				ID:        "title-user",
				SessionID: "title",
				Role:      models.RoleHuman,
				Content:   buildTitlePrompt(userMsg, assistantMsg, cfg),
			},
		},
		MaxTokens: &maxTokens,
	})
	if err != nil {
		return fallback
	}

	title := sanitizeTitle(resp.Message.Content, cfg.MaxChars)
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

func buildTitlePrompt(userMsg string, assistantMsg string, cfg titleConfig) string {
	return strings.TrimSpace("Generate a concise conversation title in at most " + strconv.Itoa(cfg.MaxWords) + " words. " +
		"Return only the title without quotes or punctuation wrappers.\n\n" +
		"User message:\n" + truncateRunes(strings.TrimSpace(userMsg), titlePromptMaxChars) +
		"\n\nAssistant reply:\n" + truncateRunes(strings.TrimSpace(assistantMsg), titlePromptMaxChars))
}

func sanitizeTitle(raw string, maxChars int) string {
	title := strings.TrimSpace(raw)
	title = strings.Trim(title, "\"'`")
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return ""
	}

	if maxChars <= 0 {
		maxChars = defaultTitleMaxChars
	}
	if utf8.RuneCountInString(title) > maxChars {
		title = truncateWithEllipsis(title, maxChars)
	}
	return strings.TrimSpace(title)
}

func fallbackTitle(userMsg string) string {
	return fallbackTitleWithConfig(userMsg, loadTitleConfig())
}

func fallbackTitleWithConfig(userMsg string, cfg titleConfig) string {
	title := strings.Join(strings.Fields(strings.TrimSpace(userMsg)), " ")
	if title == "" {
		return "New Conversation"
	}

	words := strings.Fields(title)
	maxWords := cfg.MaxWords
	if maxWords <= 0 {
		maxWords = defaultTitleMaxWords
	}
	if len(words) > maxWords {
		return strings.TrimSpace(strings.Join(words[:maxWords], " ")) + "..."
	}

	maxChars := cfg.MaxChars
	if maxChars <= 0 {
		maxChars = defaultTitleMaxChars
	}
	if maxChars > titleFallbackMaxChars {
		maxChars = titleFallbackMaxChars
	}
	if utf8.RuneCountInString(title) > maxChars {
		return truncateWithEllipsis(title, maxChars)
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

func truncateWithEllipsis(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	if limit <= 3 {
		return truncateRunes(value, limit)
	}
	return strings.TrimSpace(truncateRunes(value, limit-3)) + "..."
}

func resolveTitleModel(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return "gpt-4.1-mini"
}
