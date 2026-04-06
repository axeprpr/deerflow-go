package langgraphcompat

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type suggestionContext struct {
	Title     string
	AgentName string
	AgentHint string
	Uploads   []string
	Artifacts []string
}

func (s *Server) generateSuggestions(ctx context.Context, threadID string, messages []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}, n int, modelName string) []string {
	if len(messages) == 0 || n <= 0 {
		return []string{}
	}

	hints := s.suggestionContext(threadID)
	conversation := formatSuggestionConversation(messages)
	if conversation == "" {
		return finalizeSuggestions(nil, fallbackSuggestions(messages, hints, n), n)
	}

	return finalizeSuggestions(
		s.generateSuggestionsWithLLM(ctx, conversation, hints, n, modelName),
		fallbackSuggestions(messages, hints, n),
		n,
	)
}

func (s *Server) generateSuggestionsWithLLM(ctx context.Context, conversation string, hints suggestionContext, n int, modelName string) []string {
	provider := s.llmProvider
	if provider == nil {
		return nil
	}

	resolvedModel := resolveTitleModel(modelName, s.defaultModel)
	maxTokens := 128
	resp, err := provider.Chat(ctx, llm.ChatRequest{
		Model:           resolvedModel,
		ReasoningEffort: s.backgroundReasoningEffort(resolvedModel),
		Messages: []models.Message{{
			ID:        "suggestions-user",
			SessionID: "suggestions",
			Role:      models.RoleHuman,
			Content:   buildSuggestionsPrompt(conversation, hints, n),
		}},
		MaxTokens: &maxTokens,
	})
	if err != nil {
		return nil
	}

	suggestions := parseJSONStringList(resp.Message.Content)
	if len(suggestions) == 0 {
		return nil
	}
	if len(suggestions) > n {
		suggestions = suggestions[:n]
	}
	return suggestions
}

func buildSuggestionsPrompt(conversation string, hints suggestionContext, n int) string {
	var contextLines []string
	if title := strings.TrimSpace(hints.Title); title != "" {
		contextLines = append(contextLines, "Thread title: "+title)
	}
	if agentName := strings.TrimSpace(hints.AgentName); agentName != "" {
		line := "Custom agent: " + agentName
		if agentHint := strings.TrimSpace(hints.AgentHint); agentHint != "" {
			line += " - " + agentHint
		}
		contextLines = append(contextLines, line)
	}
	if len(hints.Uploads) > 0 {
		contextLines = append(contextLines, "Uploaded files: "+strings.Join(hints.Uploads, ", "))
	}
	if len(hints.Artifacts) > 0 {
		contextLines = append(contextLines, "Generated artifacts: "+strings.Join(hints.Artifacts, ", "))
	}

	extraContext := "None"
	if len(contextLines) > 0 {
		extraContext = strings.Join(contextLines, "\n")
	}

	return fmt.Sprintf(
		"You are generating follow-up questions to help the user continue the conversation.\n"+
			"Based on the conversation below, produce EXACTLY %d short questions the user might ask next.\n"+
			"Requirements:\n"+
			"- Questions must be relevant to the conversation.\n"+
			"- Prefer questions that make use of the thread context when relevant.\n"+
			"- Questions must be written in the same language as the user.\n"+
			"- Keep each question concise (ideally <= 20 words / <= 40 Chinese characters).\n"+
			"- Do NOT include numbering, markdown, or any extra text.\n"+
			"- Output MUST be a JSON array of strings only.\n\n"+
			"Thread context:\n%s\n\n"+
			"Conversation:\n%s\n",
		n,
		extraContext,
		conversation,
	)
}

func formatSuggestionConversation(messages []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}

		role := strings.ToLower(strings.TrimSpace(msg.Role))
		switch role {
		case "user", "human":
			parts = append(parts, "User: "+content)
		case "assistant", "ai":
			parts = append(parts, "Assistant: "+content)
		default:
			parts = append(parts, strings.TrimSpace(msg.Role)+": "+content)
		}
	}
	return strings.Join(parts, "\n")
}

func parseJSONStringList(raw string) []string {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return nil
	}
	if strings.HasPrefix(candidate, "```") {
		lines := strings.Split(candidate, "\n")
		if len(lines) >= 3 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			candidate = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}

	if parsed := decodeSuggestionPayload(candidate); len(parsed) > 0 {
		return parsed
	}

	start := strings.IndexAny(candidate, "[{")
	end := strings.LastIndexAny(candidate, "]}")
	if start >= 0 && end > start {
		if parsed := decodeSuggestionPayload(candidate[start : end+1]); len(parsed) > 0 {
			return parsed
		}
	}

	return parseBulletSuggestionList(candidate)
}

func decodeSuggestionPayload(candidate string) []string {
	var decoded any
	if err := json.Unmarshal([]byte(candidate), &decoded); err != nil {
		return nil
	}
	return suggestionStringsFromAny(decoded)
}

func suggestionStringsFromAny(value any) []string {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := suggestionTextFromAny(item)
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	case map[string]any:
		for _, key := range []string{"suggestions", "questions", "follow_ups", "followups", "items", "output", "choices"} {
			if parsed := suggestionStringsFromAny(typed[key]); len(parsed) > 0 {
				return parsed
			}
		}
		if message, ok := typed["message"].(map[string]any); ok {
			if text := suggestionTextFromAny(message); text != "" {
				return []string{text}
			}
		}
		if text := suggestionTextFromAny(typed); text != "" {
			return []string{text}
		}
	}
	return nil
}

func suggestionTextFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return normalizeSuggestionText(typed)
	case map[string]any:
		for _, key := range []string{"text", "question", "suggestion", "content", "title", "message"} {
			switch v := typed[key].(type) {
			case string:
				return normalizeSuggestionText(v)
			case map[string]any:
				if text := suggestionTextFromAny(v); text != "" {
					return text
				}
			case []any:
				for _, item := range v {
					if text := suggestionTextFromAny(item); text != "" {
						return text
					}
				}
			}
		}
		if value, ok := typed["value"].(string); ok {
			return normalizeSuggestionText(value)
		}
	}
	return ""
}

func parseBulletSuggestionList(candidate string) []string {
	lines := strings.Split(candidate, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		text := normalizeBulletSuggestion(line)
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}

func normalizeBulletSuggestion(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}

	if loc := suggestionBulletRE.FindStringIndex(line); loc != nil && loc[0] == 0 {
		return normalizeSuggestionText(line[loc[1]:])
	}
	return ""
}

func normalizeSuggestionText(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	text = strings.Trim(text, "\"'`")
	return strings.Join(strings.Fields(text), " ")
}

func finalizeSuggestions(primary, fallback []string, n int) []string {
	if n <= 0 {
		return []string{}
	}

	out := make([]string, 0, n)
	seen := make(map[string]struct{}, n)
	appendUnique := func(items []string) {
		for _, item := range items {
			text := strings.TrimSpace(strings.ReplaceAll(item, "\n", " "))
			if text == "" {
				continue
			}
			key := strings.ToLower(text)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, text)
			if len(out) == n {
				return
			}
		}
	}

	appendUnique(primary)
	if len(out) < n {
		appendUnique(fallback)
	}
	return out
}

func fallbackSuggestions(messages []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}, hints suggestionContext, n int) []string {
	lastUser := ""
	languageHint := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") || strings.EqualFold(messages[i].Role, "human") {
			lastUser = strings.TrimSpace(messages[i].Content)
			break
		}
		if languageHint == "" {
			languageHint = strings.TrimSpace(messages[i].Content)
		}
	}
	if lastUser == "" {
		return contextFallbackSuggestions(hints, languageHint, n)
	}
	return localizedFallbackSuggestions(lastUser, n)
}

func contextFallbackSuggestions(hints suggestionContext, languageHint string, n int) []string {
	return localizedContextFallbackSuggestions(hints, languageHint, n)
}

func (s *Server) suggestionContext(threadID string) suggestionContext {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || s == nil {
		return suggestionContext{}
	}

	s.sessionsMu.RLock()
	session := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if session == nil {
		return suggestionContext{}
	}

	ctx := suggestionContext{
		Title: stringValue(session.Metadata["title"]),
	}
	if agentName, ok := normalizeAgentName(stringValue(session.Metadata["agent_name"])); ok {
		ctx.AgentName = agentName
		s.uiStateMu.RLock()
		if agentCfg, exists := s.getAgentsLocked()[agentName]; exists {
			ctx.AgentHint = strings.TrimSpace(firstNonEmpty(agentCfg.Description, agentCfg.Soul))
		}
		s.uiStateMu.RUnlock()
	}

	files := s.listUploadedFiles(threadID)
	if len(files) > 0 {
		ctx.Uploads = make([]string, 0, min(4, len(files)))
		for _, info := range files {
			if name := strings.TrimSpace(asString(info["filename"])); name != "" {
				ctx.Uploads = append(ctx.Uploads, name)
				if len(ctx.Uploads) == 4 {
					break
				}
			}
		}
	}

	artifactPaths := sessionArtifactPaths(session)
	if len(artifactPaths) > 0 {
		ctx.Artifacts = make([]string, 0, min(4, len(artifactPaths)))
		for _, path := range artifactPaths {
			name := strings.TrimSpace(filepath.Base(path))
			if name == "" {
				continue
			}
			ctx.Artifacts = append(ctx.Artifacts, name)
			if len(ctx.Artifacts) == 4 {
				break
			}
		}
	}

	return ctx
}
