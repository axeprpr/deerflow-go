package langgraphcompat

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLocalizedFallbackSuggestionsChinese(t *testing.T) {
	got := localizedFallbackSuggestions("请帮我分析部署方案", 2)
	if len(got) != 2 {
		t.Fatalf("len=%d want=2", len(got))
	}
	if !strings.Contains(got[0], "部署方案") {
		t.Fatalf("first suggestion=%q missing subject", got[0])
	}
	if strings.Contains(got[0], "step-by-step") {
		t.Fatalf("first suggestion=%q unexpectedly English", got[0])
	}
}

func TestLocalizedFallbackSuggestionsEnglish(t *testing.T) {
	got := localizedFallbackSuggestions("Help me plan the migration rollout", 2)
	if len(got) != 2 {
		t.Fatalf("len=%d want=2", len(got))
	}
	if !strings.Contains(got[0], "migration rollout") {
		t.Fatalf("first suggestion=%q missing subject", got[0])
	}
	if strings.Contains(got[0], "请") {
		t.Fatalf("first suggestion=%q unexpectedly Chinese", got[0])
	}
}

func TestLocalizedFallbackSuggestionsIntentAwareChineseSummary(t *testing.T) {
	got := localizedFallbackSuggestions("请总结一下这份迁移方案", 3)
	if len(got) != 3 {
		t.Fatalf("len=%d want=3", len(got))
	}
	if !strings.Contains(got[0], "精炼摘要") {
		t.Fatalf("first suggestion=%q want summary-specific phrasing", got[0])
	}
	if strings.Contains(got[0], "分步计划") {
		t.Fatalf("first suggestion=%q unexpectedly fell back to generic planning", got[0])
	}
}

func TestLocalizedFallbackSuggestionsIntentAwareEnglishCompare(t *testing.T) {
	got := localizedFallbackSuggestions("Compare our cloud migration options", 3)
	if len(got) != 3 {
		t.Fatalf("len=%d want=3", len(got))
	}
	if !strings.Contains(strings.ToLower(got[0]), "compare") {
		t.Fatalf("first suggestion=%q want compare-specific phrasing", got[0])
	}
	if strings.Contains(strings.ToLower(got[0]), "step-by-step plan") {
		t.Fatalf("first suggestion=%q unexpectedly generic", got[0])
	}
}

func TestCompactSubjectTruncatesRunesSafely(t *testing.T) {
	input := strings.Repeat("迁", 60)
	got := compactSubject(input)
	if len([]rune(got)) != 48 {
		t.Fatalf("runes=%d want=48", len([]rune(got)))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("got invalid UTF-8: %q", got)
	}
}

func TestDetectSuggestionLanguage(t *testing.T) {
	tests := map[string]string{
		"请帮我分析这份方案":             "zh",
		"この内容を整理して":             "ja",
		"이 계획을 검토해 줘":           "ko",
		"Summarize the rollout": "en",
	}
	for input, want := range tests {
		if got := detectSuggestionLanguage(input); got != want {
			t.Fatalf("detectSuggestionLanguage(%q)=%q want=%q", input, got, want)
		}
	}
}

func TestDetectSuggestionIntent(t *testing.T) {
	tests := []struct {
		text     string
		language string
		want     string
	}{
		{text: "请总结一下这个方案", language: "zh", want: "summarize"},
		{text: "比较一下两个实现方案", language: "zh", want: "compare"},
		{text: "Write a reply to this customer", language: "en", want: "write"},
		{text: "Analyze the rollout risks", language: "en", want: "analyze"},
		{text: "Help me with this", language: "en", want: "general"},
	}

	for _, tc := range tests {
		if got := detectSuggestionIntent(tc.text, tc.language); got != tc.want {
			t.Fatalf("detectSuggestionIntent(%q, %q)=%q want=%q", tc.text, tc.language, got, tc.want)
		}
	}
}
