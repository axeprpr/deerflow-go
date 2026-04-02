package langgraphcompat

import (
	"os"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/subagent"
	"gopkg.in/yaml.v3"
)

const defaultGatewaySubagentTimeout = 15 * time.Minute

type subagentOverrideConfig struct {
	TimeoutSeconds int `yaml:"timeout_seconds"`
	MaxTurns       int `yaml:"max_turns"`
}

type subagentsAppConfig struct {
	TimeoutSeconds int                               `yaml:"timeout_seconds"`
	Agents         map[string]subagentOverrideConfig `yaml:"agents"`
}

func loadSubagentsAppConfig() subagentsAppConfig {
	cfg := subagentsAppConfig{
		TimeoutSeconds: int(defaultGatewaySubagentTimeout / time.Second),
	}

	path, ok := resolveGatewayConfigPath()
	if !ok {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	var raw struct {
		Subagents subagentsAppConfig `yaml:"subagents"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg
	}

	if raw.Subagents.TimeoutSeconds > 0 {
		cfg.TimeoutSeconds = raw.Subagents.TimeoutSeconds
	}
	if len(raw.Subagents.Agents) > 0 {
		cfg.Agents = normalizeSubagentOverrides(raw.Subagents.Agents)
	}
	return cfg
}

func normalizeSubagentOverrides(input map[string]subagentOverrideConfig) map[string]subagentOverrideConfig {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]subagentOverrideConfig, len(input))
	for name, override := range input {
		normalized := string(normalizeConfiguredSubagentType(name))
		if normalized == "" {
			continue
		}
		if override.TimeoutSeconds <= 0 && override.MaxTurns <= 0 {
			continue
		}
		out[normalized] = override
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeConfiguredSubagentType(name string) subagent.SubagentType {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case string(subagent.SubagentGeneralPurpose):
		return subagent.SubagentGeneralPurpose
	case string(subagent.SubagentBash):
		return subagent.SubagentBash
	default:
		return ""
	}
}

func (c subagentsAppConfig) timeoutFor(kind subagent.SubagentType) time.Duration {
	if override, ok := c.Agents[string(kind)]; ok && override.TimeoutSeconds > 0 {
		return time.Duration(override.TimeoutSeconds) * time.Second
	}
	if c.TimeoutSeconds > 0 {
		return time.Duration(c.TimeoutSeconds) * time.Second
	}
	return defaultGatewaySubagentTimeout
}

func (c subagentsAppConfig) maxTurnsFor(kind subagent.SubagentType, fallback int) int {
	if override, ok := c.Agents[string(kind)]; ok && override.MaxTurns > 0 {
		return override.MaxTurns
	}
	if fallback > 0 {
		return fallback
	}
	return 1
}
