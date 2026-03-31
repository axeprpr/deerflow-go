package langgraphcompat

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/tools"
	"gopkg.in/yaml.v3"
)

func loadACPAgentConfigs() map[string]tools.ACPAgentConfig {
	if configs := loadACPAgentsFromEnv(); len(configs) > 0 {
		return configs
	}
	return loadACPAgentsFromConfig()
}

func loadACPAgentsFromEnv() map[string]tools.ACPAgentConfig {
	raw := strings.TrimSpace(os.Getenv("DEERFLOW_ACP_AGENTS_JSON"))
	if raw == "" {
		return nil
	}

	var cfg map[string]tools.ACPAgentConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil
	}
	return normalizeACPAgentConfigs(cfg)
}

func loadACPAgentsFromConfig() map[string]tools.ACPAgentConfig {
	path, ok := resolveGatewayConfigPath()
	if !ok {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var raw struct {
		ACPAgents map[string]tools.ACPAgentConfig `yaml:"acp_agents"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return normalizeACPAgentConfigs(raw.ACPAgents)
}

func normalizeACPAgentConfigs(input map[string]tools.ACPAgentConfig) map[string]tools.ACPAgentConfig {
	if len(input) == 0 {
		return nil
	}

	normalized := make(map[string]tools.ACPAgentConfig, len(input))
	keys := make([]string, 0, len(input))
	for name := range input {
		keys = append(keys, name)
	}
	sort.Strings(keys)

	for _, name := range keys {
		trimmed := strings.TrimSpace(name)
		cfg := input[name]
		cfg.Command = strings.TrimSpace(cfg.Command)
		if trimmed == "" || cfg.Command == "" {
			continue
		}
		cfg.Description = strings.TrimSpace(cfg.Description)
		cfg.Model = strings.TrimSpace(cfg.Model)
		normalized[trimmed] = cfg
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
