package guardrails

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config controls optional pre-tool-call guardrails.
type Config struct {
	Enabled      bool
	AllowedTools []string
	DeniedTools  []string
	FailClosed   bool
	Passport     string
	ProviderUse  string
	ProviderArgs map[string]any
}

func LoadConfigFromEnv() Config {
	cfg := loadConfigFromFile()
	if raw := strings.TrimSpace(os.Getenv("DEERFLOW_GUARDRAILS_PASSPORT")); raw != "" {
		cfg.Passport = raw
	}
	if hasEnv("DEERFLOW_GUARDRAILS_ENABLED") {
		cfg.Enabled = parseEnvBool("DEERFLOW_GUARDRAILS_ENABLED", cfg.Enabled)
	}
	if hasEnv("DEERFLOW_GUARDRAILS_FAIL_CLOSED") {
		cfg.FailClosed = parseEnvBool("DEERFLOW_GUARDRAILS_FAIL_CLOSED", cfg.FailClosed)
	}
	if tools := parseCSVEnv("DEERFLOW_GUARDRAILS_ALLOWED_TOOLS"); tools != nil {
		cfg.AllowedTools = tools
		cfg.ProviderUse = allowlistProviderUse
	}
	if tools := parseCSVEnv("DEERFLOW_GUARDRAILS_DENIED_TOOLS"); tools != nil {
		cfg.DeniedTools = tools
		cfg.ProviderUse = allowlistProviderUse
	}
	return cfg
}

func (c Config) BuildProvider() Provider {
	if !c.Enabled {
		return nil
	}
	switch normalizeProviderUse(c.ProviderUse) {
	case "", allowlistProviderUse:
		if len(c.AllowedTools) == 0 && len(c.DeniedTools) == 0 && len(c.ProviderArgs) > 0 {
			c.AllowedTools = readStringList(c.ProviderArgs["allowed_tools"])
			c.DeniedTools = readStringList(c.ProviderArgs["denied_tools"])
		}
		return NewAllowlistProvider(c.AllowedTools, c.DeniedTools)
	default:
		return invalidProvider{name: c.ProviderUse}
	}
}

const allowlistProviderUse = "deerflow.guardrails.builtin:allowlistprovider"

type invalidProvider struct {
	name string
}

func (p invalidProvider) Name() string {
	if value := strings.TrimSpace(p.name); value != "" {
		return value
	}
	return "invalid_guardrail_provider"
}

func (p invalidProvider) Evaluate(Request) (Decision, error) {
	name := strings.TrimSpace(p.name)
	if name == "" {
		name = "unknown"
	}
	return Decision{}, &providerConfigError{Provider: name}
}

type providerConfigError struct {
	Provider string
}

func (e *providerConfigError) Error() string {
	if e == nil || strings.TrimSpace(e.Provider) == "" {
		return "guardrail provider is not supported"
	}
	return "guardrail provider is not supported: " + strings.TrimSpace(e.Provider)
}

func loadConfigFromFile() Config {
	cfg := Config{FailClosed: true}
	path, ok := resolveConfigPath()
	if !ok {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	var raw struct {
		Guardrails struct {
			Enabled    *bool  `yaml:"enabled"`
			FailClosed *bool  `yaml:"fail_closed"`
			Passport   string `yaml:"passport"`
			Provider   struct {
				Use    string         `yaml:"use"`
				Config map[string]any `yaml:"config"`
			} `yaml:"provider"`
		} `yaml:"guardrails"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg
	}
	if raw.Guardrails.Enabled != nil {
		cfg.Enabled = *raw.Guardrails.Enabled
	}
	if raw.Guardrails.FailClosed != nil {
		cfg.FailClosed = *raw.Guardrails.FailClosed
	}
	cfg.Passport = strings.TrimSpace(raw.Guardrails.Passport)
	cfg.ProviderUse = strings.TrimSpace(raw.Guardrails.Provider.Use)
	cfg.ProviderArgs = cloneMap(raw.Guardrails.Provider.Config)
	if tools := readStringList(raw.Guardrails.Provider.Config["allowed_tools"]); tools != nil {
		cfg.AllowedTools = tools
	}
	if tools := readStringList(raw.Guardrails.Provider.Config["denied_tools"]); tools != nil {
		cfg.DeniedTools = tools
	}
	return cfg
}

func resolveConfigPath() (string, bool) {
	for _, key := range []string{"DEERFLOW_CONFIG_PATH", "DEER_FLOW_CONFIG_PATH"} {
		if path := strings.TrimSpace(os.Getenv(key)); path != "" {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path, true
			}
			return "", false
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for _, candidate := range []string{
		filepath.Join(wd, "config.yaml"),
		filepath.Join(filepath.Dir(wd), "config.yaml"),
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func normalizeProviderUse(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasSuffix(lower, ":allowlistprovider") || lower == "allowlistprovider" {
		return allowlistProviderUse
	}
	return lower
}

func readStringList(raw any) []string {
	switch items := raw.(type) {
	case []string:
		return normalizeStringList(items)
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if value := strings.TrimSpace(stringValue(item)); value != "" {
				out = append(out, value)
			}
		}
		return normalizeStringList(out)
	default:
		return nil
	}
}

func normalizeStringList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value := strings.TrimSpace(item); value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func stringValue(v any) string {
	switch value := v.(type) {
	case string:
		return value
	default:
		return ""
	}
}

func hasEnv(key string) bool {
	_, ok := os.LookupEnv(key)
	return ok
}

func parseCSVEnv(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseEnvBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}
