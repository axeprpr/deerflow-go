package langgraphcompat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

const (
	historySummaryMetadataKey          = "history_summary"
	historySummaryUpdatedAtMetadataKey = "history_summary_updated_at"
	summaryPromptMessageMetadataKey    = "transient_history_summary"
	defaultSummaryTriggerMessages      = 30
	defaultSummaryKeepMessages         = 12
	defaultSummaryTriggerApproxTokens  = 8000
	summaryPromptMaxChars              = 12000
	summaryFallbackMaxChars            = 1600
)

type historyCompactionResult struct {
	Summary  string
	Messages []models.Message
	Changed  bool
}

func (s *Server) compactConversationHistory(ctx context.Context, threadID string, modelName string, existingSummary string, messages []models.Message) historyCompactionResult {
	if len(messages) == 0 {
		return historyCompactionResult{
			Summary:  strings.TrimSpace(existingSummary),
			Messages: nil,
		}
	}

	existingSummary = strings.TrimSpace(existingSummary)
	if !shouldCompactConversation(existingSummary, messages) {
		return historyCompactionResult{
			Summary:  existingSummary,
			Messages: append([]models.Message(nil), messages...),
		}
	}

	cut := findHistoryCompactionCut(messages)
	if cut <= 0 || cut >= len(messages) {
		return historyCompactionResult{
			Summary:  existingSummary,
			Messages: append([]models.Message(nil), messages...),
		}
	}

	toSummarize := append([]models.Message(nil), messages[:cut]...)
	kept := append([]models.Message(nil), messages[cut:]...)
	summary := s.generateConversationSummary(ctx, threadID, modelName, existingSummary, toSummarize)
	if summary == "" {
		summary = buildFallbackConversationSummary(existingSummary, toSummarize)
	}

	return historyCompactionResult{
		Summary:  summary,
		Messages: kept,
		Changed:  strings.TrimSpace(summary) != existingSummary || cut < len(messages),
	}
}

func shouldCompactConversation(existingSummary string, messages []models.Message) bool {
	if len(messages) <= defaultSummaryKeepMessages {
		return false
	}
	if len(messages) >= defaultSummaryTriggerMessages {
		return true
	}
	return approximateTokenCount(existingSummary)+approximateMessagesTokenCount(messages) >= defaultSummaryTriggerApproxTokens
}

func findHistoryCompactionCut(messages []models.Message) int {
	cut := len(messages) - defaultSummaryKeepMessages
	if cut <= 0 {
		return 0
	}
	for cut > 0 && messages[cut].Role == models.RoleTool {
		cut--
	}
	if cut > 0 && messages[cut-1].Role == models.RoleAI && len(messages[cut-1].ToolCalls) > 0 {
		cut--
	}
	if cut < 0 {
		return 0
	}
	return cut
}

func (s *Server) generateConversationSummary(ctx context.Context, threadID string, modelName string, existingSummary string, messages []models.Message) string {
	if s == nil || s.llmProvider == nil || len(messages) == 0 {
		return buildFallbackConversationSummary(existingSummary, messages)
	}

	resolvedModel := resolveTitleModel(modelName, s.defaultModel)
	maxTokens := 320
	resp, err := s.llmProvider.Chat(ctx, llm.ChatRequest{
		Model:           resolvedModel,
		ReasoningEffort: s.backgroundReasoningEffort(resolvedModel),
		Messages: []models.Message{{
			ID:        "history-summary",
			SessionID: threadID,
			Role:      models.RoleHuman,
			Content:   buildConversationSummaryPrompt(existingSummary, messages),
		}},
		MaxTokens: &maxTokens,
	})
	if err != nil {
		return buildFallbackConversationSummary(existingSummary, messages)
	}
	return sanitizeConversationSummary(resp.Message.Content)
}

func buildConversationSummaryPrompt(existingSummary string, messages []models.Message) string {
	var b strings.Builder
	b.WriteString("Update the running conversation summary for a long chat.\n")
	b.WriteString("Preserve user goals, confirmed constraints, important findings, pending work, and notable tool outcomes.\n")
	b.WriteString("Omit filler. Return only the updated summary.\n\n")
	if existingSummary != "" {
		b.WriteString("Current summary:\n")
		b.WriteString(existingSummary)
		b.WriteString("\n\n")
	}
	b.WriteString("New conversation segment:\n")
	segment := renderConversationSegment(messages, summaryPromptMaxChars)
	if segment == "" {
		segment = "(empty)"
	}
	b.WriteString(segment)
	return b.String()
}

func renderConversationSegment(messages []models.Message, limit int) string {
	if len(messages) == 0 || limit <= 0 {
		return ""
	}
	var b strings.Builder
	for _, msg := range messages {
		line := summarizeMessageForPrompt(msg)
		if line == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		if b.Len()+len(line) > limit {
			remaining := limit - b.Len()
			if remaining <= 0 {
				break
			}
			b.WriteString(truncateRunes(line, remaining))
			break
		}
		b.WriteString(line)
	}
	return strings.TrimSpace(b.String())
}

func summarizeMessageForPrompt(msg models.Message) string {
	var role string
	switch msg.Role {
	case models.RoleHuman:
		role = "User"
	case models.RoleAI:
		role = "Assistant"
	case models.RoleTool:
		role = "Tool"
	case models.RoleSystem:
		role = "System"
	default:
		role = "Message"
	}

	content := strings.TrimSpace(msg.Content)
	if msg.Role == models.RoleTool && msg.ToolResult != nil {
		content = strings.TrimSpace(firstNonEmpty(msg.ToolResult.Content, msg.ToolResult.Error))
		if msg.ToolResult.ToolName != "" {
			role = fmt.Sprintf("Tool %s", msg.ToolResult.ToolName)
		}
	}
	if content == "" && len(msg.ToolCalls) > 0 {
		names := make([]string, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			if name := strings.TrimSpace(call.Name); name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			content = "Tool calls: " + strings.Join(names, ", ")
		}
	}
	content = strings.Join(strings.Fields(content), " ")
	if content == "" {
		return ""
	}
	return role + ": " + truncateRunes(content, 400)
}

func sanitizeConversationSummary(raw string) string {
	summary := strings.TrimSpace(raw)
	summary = strings.Trim(summary, "\"'`")
	summary = strings.Join(strings.Fields(summary), " ")
	return strings.TrimSpace(summary)
}

func buildFallbackConversationSummary(existingSummary string, messages []models.Message) string {
	parts := make([]string, 0, len(messages)+1)
	if existingSummary != "" {
		parts = append(parts, existingSummary)
	}
	for _, msg := range messages {
		line := summarizeMessageForPrompt(msg)
		if line == "" {
			continue
		}
		parts = append(parts, line)
	}
	return truncateRunes(strings.Join(parts, " "), summaryFallbackMaxChars)
}

func approximateMessagesTokenCount(messages []models.Message) int {
	total := 0
	for _, msg := range messages {
		total += approximateTokenCount(msg.Content)
		if msg.ToolResult != nil {
			total += approximateTokenCount(msg.ToolResult.Content)
			total += approximateTokenCount(msg.ToolResult.Error)
		}
		for _, call := range msg.ToolCalls {
			total += approximateTokenCount(call.Name)
		}
	}
	return total
}

func approximateTokenCount(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	return (len(text) + 3) / 4
}

func conversationSummaryMessage(threadID string, summary string) models.Message {
	return models.Message{
		ID:        "summary-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()),
		SessionID: threadID,
		Role:      models.RoleSystem,
		Content:   "Conversation summary:\n" + strings.TrimSpace(summary),
		Metadata: map[string]string{
			summaryPromptMessageMetadataKey: "true",
		},
		CreatedAt: time.Now().UTC(),
	}
}

func (s *Server) threadHistorySummary(threadID string) string {
	if s == nil || strings.TrimSpace(threadID) == "" {
		return ""
	}
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	session := s.sessions[threadID]
	if session == nil || session.Metadata == nil {
		return ""
	}
	return strings.TrimSpace(stringValue(session.Metadata[historySummaryMetadataKey]))
}

func (s *Server) setThreadHistorySummary(threadID string, summary string) {
	if s == nil || strings.TrimSpace(threadID) == "" {
		return
	}
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	session := s.sessions[threadID]
	if session == nil {
		return
	}
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		delete(session.Metadata, historySummaryMetadataKey)
		delete(session.Metadata, historySummaryUpdatedAtMetadataKey)
		return
	}
	session.Metadata[historySummaryMetadataKey] = summary
	session.Metadata[historySummaryUpdatedAtMetadataKey] = time.Now().UTC().Format(time.RFC3339Nano)
}

func isInjectedSummaryMessage(msg models.Message) bool {
	if len(msg.Metadata) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(msg.Metadata[summaryPromptMessageMetadataKey]), "true")
}
