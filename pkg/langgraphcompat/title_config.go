package langgraphcompat

import (
	"os"
	"strconv"
	"strings"
)

const (
	titleEnabledEnv  = "DEERFLOW_TITLE_ENABLED"
	titleMaxWordsEnv = "DEERFLOW_TITLE_MAX_WORDS"
	titleMaxCharsEnv = "DEERFLOW_TITLE_MAX_CHARS"
	titleModelEnv    = "DEERFLOW_TITLE_MODEL"

	defaultTitleMaxWords = 6
	defaultTitleMaxChars = 60
	minTitleMaxWords     = 1
	maxTitleMaxWords     = 20
	minTitleMaxChars     = 10
	maxTitleMaxChars     = 200
)

type titleConfig struct {
	Enabled  bool
	MaxWords int
	MaxChars int
	Model    string
}

func loadTitleConfig() titleConfig {
	cfg := titleConfig{
		Enabled:  true,
		MaxWords: defaultTitleMaxWords,
		MaxChars: defaultTitleMaxChars,
		Model:    strings.TrimSpace(os.Getenv(titleModelEnv)),
	}

	if raw := strings.TrimSpace(os.Getenv(titleEnabledEnv)); raw != "" {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			cfg.Enabled = parsed
		}
	}
	if parsed, ok := parseTitleBound(titleMaxWordsEnv, minTitleMaxWords, maxTitleMaxWords); ok {
		cfg.MaxWords = parsed
	}
	if parsed, ok := parseTitleBound(titleMaxCharsEnv, minTitleMaxChars, maxTitleMaxChars); ok {
		cfg.MaxChars = parsed
	}

	return cfg
}

func parseTitleBound(env string, min, max int) (int, bool) {
	raw := strings.TrimSpace(os.Getenv(env))
	if raw == "" {
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, false
	}
	return value, true
}
