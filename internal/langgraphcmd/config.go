package langgraphcmd

import (
	"strings"

	"github.com/axeprpr/deerflow-go/internal/runtimecmd"
	"github.com/axeprpr/deerflow-go/pkg/langgraphcompat"
)

type Config struct {
	AuthToken   string
	Addr        string
	DatabaseURL string
	Provider    string
	Model       string
	Runtime     runtimecmd.NodeConfig
}

func DefaultConfig(defaultAddr string) Config {
	return Config{
		AuthToken:   strings.TrimSpace(firstNonEmptyEnv("DEERFLOW_AUTH_TOKEN")),
		Addr:        normalizeAddr(firstNonEmptyEnv("ADDR", "PORT"), defaultAddr),
		DatabaseURL: firstNonEmptyEnv("DATABASE_URL", "POSTGRES_URL"),
		Provider:    firstNonEmptyEnvOrDefault("siliconflow", "DEFAULT_LLM_PROVIDER"),
		Model:       firstNonEmptyEnvOrDefault("qwen/Qwen3.5-9B", "DEFAULT_LLM_MODEL"),
		Runtime:     runtimecmd.DefaultLangGraphNodeConfig(),
	}
}

func (c Config) Validate() error {
	return c.Runtime.ValidateForLangGraph()
}

func (c Config) ServerOptions() []langgraphcompat.ServerOption {
	return []langgraphcompat.ServerOption{
		langgraphcompat.WithRuntimeNodeConfig(c.Runtime.RuntimeNodeConfig()),
		langgraphcompat.WithMaxTurns(c.Runtime.MaxTurns),
		langgraphcompat.WithLLMProviderName(c.Provider),
	}
}

func (c Config) BuildServer() (*langgraphcompat.Server, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return langgraphcompat.NewServer(c.Addr, c.DatabaseURL, c.Model, c.ServerOptions()...)
}

func normalizeAddr(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	if value == "" {
		value = ":8080"
	}
	if strings.HasPrefix(value, ":") {
		return value
	}
	return ":" + value
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if key == "" {
			continue
		}
		if value := strings.TrimSpace(getenv(key)); value != "" {
			if key == "PORT" && !strings.HasPrefix(value, ":") {
				return ":" + value
			}
			return value
		}
	}
	return ""
}

func firstNonEmptyEnvOrDefault(fallback string, keys ...string) string {
	if value := firstNonEmptyEnv(keys...); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

var getenv = func(string) string { return "" }
