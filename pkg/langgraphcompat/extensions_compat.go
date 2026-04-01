package langgraphcompat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type gatewayExtensionsSkillState struct {
	Enabled bool `json:"enabled"`
}

type gatewayExtensionsConfig struct {
	MCPServers map[string]gatewayMCPServerConfig      `json:"mcpServers"`
	Skills     map[string]gatewayExtensionsSkillState `json:"skills"`
}

func resolveGatewayExtensionsConfigPath() string {
	if raw := strings.TrimSpace(os.Getenv("DEERFLOW_EXTENSIONS_CONFIG_PATH")); raw != "" {
		return filepath.Clean(raw)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	candidates := []string{
		filepath.Join(cwd, "extensions_config.json"),
		filepath.Join(filepath.Dir(cwd), "extensions_config.json"),
		filepath.Join(cwd, "mcp_config.json"),
		filepath.Join(filepath.Dir(cwd), "mcp_config.json"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return filepath.Join(cwd, "extensions_config.json")
}

func (s *Server) loadGatewayExtensionsConfig() error {
	path := resolveGatewayExtensionsConfigPath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	raw = resolveGatewayExtensionsEnvVariables(raw).(map[string]any)

	var cfg gatewayExtensionsConfig
	normalized, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(normalized, &cfg); err != nil {
		return err
	}

	currentSkills := s.currentGatewaySkills()

	s.uiStateMu.Lock()
	defer s.uiStateMu.Unlock()

	if len(cfg.MCPServers) > 0 {
		s.mcpConfig = gatewayMCPConfig{MCPServers: cfg.MCPServers}
	}
	if len(cfg.Skills) > 0 {
		merged := normalizePersistedSkills(s.skills)
		for name, state := range cfg.Skills {
			normalizedName := sanitizeSkillName(name)
			if normalizedName == "" {
				continue
			}

			applied := false
			for key, skill := range currentSkills {
				if skill.Name != normalizedName {
					continue
				}
				skill.Enabled = state.Enabled
				merged[key] = skill
				applied = true
			}
			if !applied {
				category := inferSkillCategory(normalizedName)
				merged[skillStorageKey(category, normalizedName)] = gatewaySkill{
					Name:     normalizedName,
					Category: category,
					Enabled:  state.Enabled,
				}
			}
		}
		s.skills = mergeGatewaySkills(defaultGatewaySkills(), merged)
	}
	return nil
}

func resolveGatewayExtensionsEnvVariables(value any) any {
	switch v := value.(type) {
	case map[string]any:
		resolved := make(map[string]any, len(v))
		for key, item := range v {
			resolved[key] = resolveGatewayExtensionsEnvVariables(item)
		}
		return resolved
	case []any:
		resolved := make([]any, len(v))
		for i, item := range v {
			resolved[i] = resolveGatewayExtensionsEnvVariables(item)
		}
		return resolved
	case string:
		if strings.HasPrefix(v, "$") {
			if envValue, ok := os.LookupEnv(strings.TrimPrefix(v, "$")); ok {
				return envValue
			}
			return ""
		}
		return v
	default:
		return value
	}
}

func (s *Server) persistGatewayExtensionsConfig() error {
	path := resolveGatewayExtensionsConfigPath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	s.uiStateMu.RLock()
	cfg := gatewayExtensionsConfig{
		MCPServers: cloneGatewayMCPServers(s.mcpConfig.MCPServers),
		Skills:     gatewayExtensionsSkillsFromState(normalizePersistedSkills(s.skills)),
	}
	s.uiStateMu.RUnlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func cloneGatewayMCPServers(src map[string]gatewayMCPServerConfig) map[string]gatewayMCPServerConfig {
	if len(src) == 0 {
		return map[string]gatewayMCPServerConfig{}
	}
	dst := make(map[string]gatewayMCPServerConfig, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func gatewayExtensionsSkillsFromState(skills map[string]gatewaySkill) map[string]gatewayExtensionsSkillState {
	if len(skills) == 0 {
		return map[string]gatewayExtensionsSkillState{}
	}

	keys := make([]string, 0, len(skills))
	for key := range skills {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make(map[string]gatewayExtensionsSkillState, len(keys))
	for _, key := range keys {
		skill := skills[key]
		if skill.Name == "" {
			continue
		}
		out[skill.Name] = gatewayExtensionsSkillState{Enabled: skill.Enabled}
	}
	return out
}
