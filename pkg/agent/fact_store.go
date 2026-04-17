package agent

import (
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

const (
	defaultRunFactStoreMaxEntries     = 64
	defaultRunFactStoreMaxValueChars  = 240
	defaultRunFactStoreMaxPromptChars = 4000
)

type runFactStorePolicy struct {
	Enabled        bool
	MaxEntries     int
	MaxValueChars  int
	MaxPromptChars int
}

type runFactStoreState struct {
	policy runFactStorePolicy
	facts  map[string]string
}

func resolveRunFactStorePolicy() runFactStorePolicy {
	policy := runFactStorePolicy{
		Enabled:        boolFromEnvWithDefault("DEERFLOW_FACT_STORE_ENABLED", true),
		MaxEntries:     intFromEnvWithDefault("DEERFLOW_FACT_STORE_MAX_ENTRIES", defaultRunFactStoreMaxEntries),
		MaxValueChars:  intFromEnvWithDefault("DEERFLOW_FACT_STORE_MAX_VALUE_CHARS", defaultRunFactStoreMaxValueChars),
		MaxPromptChars: intFromEnvWithDefault("DEERFLOW_FACT_STORE_MAX_PROMPT_CHARS", defaultRunFactStoreMaxPromptChars),
	}
	if policy.MaxEntries <= 0 {
		policy.MaxEntries = defaultRunFactStoreMaxEntries
	}
	if policy.MaxValueChars <= 0 {
		policy.MaxValueChars = defaultRunFactStoreMaxValueChars
	}
	if policy.MaxPromptChars <= 0 {
		policy.MaxPromptChars = defaultRunFactStoreMaxPromptChars
	}
	return policy
}

func newRunFactStoreState(policy runFactStorePolicy) *runFactStoreState {
	return &runFactStoreState{
		policy: policy,
		facts:  map[string]string{},
	}
}

func (s *runFactStoreState) observeMessages(messages []models.Message) {
	if s == nil || !s.policy.Enabled {
		return
	}
	for _, msg := range messages {
		if msg.Role == models.RoleAI {
			s.observeAssistantMessage(msg)
		}
		if msg.ToolResult != nil && strings.EqualFold(strings.TrimSpace(msg.ToolResult.ToolName), "read_file") {
			s.upsertAll(extractFactCandidates(msg.ToolResult.Content, s.policy.MaxValueChars))
		}
	}
}

func (s *runFactStoreState) observeAssistantMessage(msg models.Message) {
	if s == nil || !s.policy.Enabled {
		return
	}
	if strings.TrimSpace(msg.Content) == "" {
		return
	}
	s.upsertAll(extractFactCandidates(msg.Content, s.policy.MaxValueChars))
}

func (s *runFactStoreState) upsertAll(facts map[string]string) {
	if s == nil || !s.policy.Enabled || len(facts) == 0 {
		return
	}
	keys := make([]string, 0, len(facts))
	for key := range facts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := strings.TrimSpace(facts[key])
		if value == "" {
			continue
		}
		if _, exists := s.facts[key]; !exists && len(s.facts) >= s.policy.MaxEntries {
			continue
		}
		s.facts[key] = value
	}
}

func (s *runFactStoreState) prompt() string {
	if s == nil || !s.policy.Enabled || len(s.facts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(s.facts))
	for key := range s.facts {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("<run_fact_store>\n")
	b.WriteString("Cross-turn fact anchors extracted from earlier turns.\n")
	b.WriteString("When the user asks to restate fields/JSON, keep these values unless newer tool output contradicts them.\n")
	b.WriteString("facts:\n")
	for _, key := range keys {
		line := "- " + key + ": " + s.facts[key] + "\n"
		if s.policy.MaxPromptChars > 0 && b.Len()+len(line)+len("</run_fact_store>") > s.policy.MaxPromptChars {
			break
		}
		b.WriteString(line)
	}
	b.WriteString("</run_fact_store>")
	return b.String()
}

func extractFactCandidates(text string, maxValueChars int) map[string]string {
	out := extractJSONScalarFacts(text, maxValueChars)
	for key, value := range extractDelimitedFacts(text, maxValueChars) {
		if _, exists := out[key]; exists {
			continue
		}
		out[key] = value
	}
	return out
}

func extractJSONScalarFacts(text string, maxValueChars int) map[string]string {
	obj := parseFirstJSONObject(text)
	if len(obj) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(obj))
	for key, raw := range obj {
		normKey := normalizeFactKey(key)
		if normKey == "" {
			continue
		}
		normValue := normalizeFactValue(raw, maxValueChars)
		if normValue == "" {
			continue
		}
		out[normKey] = normValue
	}
	return out
}

func extractDelimitedFacts(text string, maxValueChars int) map[string]string {
	if strings.TrimSpace(text) == "" {
		return map[string]string{}
	}
	out := map[string]string{}
	lines := strings.Split(text, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || len(line) > 320 {
			continue
		}
		for _, marker := range []string{"CRITICAL_FACT:", "FINAL_OVERRIDE:", "CRITICAL:", "最终生效:"} {
			if idx := strings.Index(line, marker); idx >= 0 {
				line = strings.TrimSpace(line[idx+len(marker):])
			}
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := normalizeFactKey(parts[0])
		if key == "" {
			continue
		}
		value := normalizeFactValue(parts[1], maxValueChars)
		if value == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func parseFirstJSONObject(text string) map[string]any {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	for _, candidate := range []string{extractJSONCodeFence(trimmed), trimmed} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if obj, ok := decodeJSONObjectCandidate(candidate); ok {
			return obj
		}
	}
	return nil
}

func extractJSONCodeFence(text string) string {
	start := strings.Index(text, "```")
	if start < 0 {
		return ""
	}
	rest := text[start+3:]
	end := strings.Index(rest, "```")
	if end < 0 {
		return ""
	}
	body := strings.TrimSpace(rest[:end])
	body = strings.TrimPrefix(body, "json")
	body = strings.TrimPrefix(body, "JSON")
	return strings.TrimSpace(body)
}

func decodeJSONObjectCandidate(candidate string) (map[string]any, bool) {
	for idx := 0; idx < len(candidate); idx++ {
		if candidate[idx] != '{' {
			continue
		}
		var obj map[string]any
		dec := json.NewDecoder(strings.NewReader(candidate[idx:]))
		dec.UseNumber()
		if err := dec.Decode(&obj); err == nil && len(obj) > 0 {
			return obj, true
		}
	}
	return nil, false
}

func normalizeFactKey(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '_' || r == '-' || unicode.IsSpace(r):
			b.WriteByte('_')
		}
	}
	key := strings.Trim(b.String(), "_")
	key = strings.ReplaceAll(key, "__", "_")
	for strings.Contains(key, "__") {
		key = strings.ReplaceAll(key, "__", "_")
	}
	if key == "" {
		return ""
	}
	if len(key) > 64 {
		key = key[:64]
	}
	return key
}

func normalizeFactValue(raw any, maxValueChars int) string {
	var text string
	switch typed := raw.(type) {
	case string:
		text = typed
	case json.Number:
		text = typed.String()
	case float64:
		text = strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		if typed {
			text = "true"
		} else {
			text = "false"
		}
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		text = string(encoded)
	}
	text = strings.TrimSpace(text)
	text = strings.Trim(text, `"'`)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if maxValueChars > 0 && len(runes) > maxValueChars {
		text = string(runes[:maxValueChars])
	}
	return strings.TrimSpace(text)
}

func boolFromEnvWithDefault(key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
