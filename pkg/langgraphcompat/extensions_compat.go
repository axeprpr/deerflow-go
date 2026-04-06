package langgraphcompat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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

var gatewayEnvPlaceholderRE = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

func resolveGatewayExtensionsConfigPath() string {
	for _, key := range []string{"DEERFLOW_EXTENSIONS_CONFIG_PATH", "DEER_FLOW_EXTENSIONS_CONFIG_PATH"} {
		if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
			return filepath.Clean(raw)
		}
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
		for rawKey, state := range cfg.Skills {
			category, normalizedName := splitSkillStorageKey(rawKey)
			if normalizedName == "" {
				continue
			}

			applied := false
			if category != "" {
				key := skillStorageKey(category, normalizedName)
				if skill, ok := currentSkills[key]; ok {
					skill.Enabled = state.Enabled
					merged[key] = skill
					applied = true
				}
			} else {
				for key, skill := range currentSkills {
					if skill.Name != normalizedName {
						continue
					}
					skill.Enabled = state.Enabled
					merged[key] = skill
					applied = true
				}
			}
			if !applied {
				category = inferSkillCategory(normalizedName)
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

func (s *Server) refreshGatewayExtensionsConfig() {
	if s == nil {
		return
	}
	if err := s.loadGatewayExtensionsConfig(); err != nil && s.logger != nil {
		s.logger.Printf("Warning: failed to refresh gateway extensions config: %v", err)
	}
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
		return expandGatewayEnvString(v)
	default:
		return value
	}
}

func expandGatewayEnvString(raw string) string {
	if raw == "" || !strings.Contains(raw, "$") {
		return raw
	}
	return gatewayEnvPlaceholderRE.ReplaceAllStringFunc(raw, func(match string) string {
		submatches := gatewayEnvPlaceholderRE.FindStringSubmatch(match)
		if len(submatches) == 0 {
			return match
		}
		name := strings.TrimSpace(submatches[1])
		if name == "" {
			name = strings.TrimSpace(submatches[4])
		}
		if name == "" {
			return ""
		}
		if value, ok := os.LookupEnv(name); ok {
			return value
		}
		if submatches[3] != "" {
			return expandGatewayEnvString(submatches[3])
		}
		return ""
	})
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
	persistedSkills := normalizePersistedSkills(s.skills)
	cfg := gatewayExtensionsConfig{
		MCPServers: cloneGatewayMCPServers(s.mcpConfig.MCPServers),
		Skills:     gatewayExtensionsSkillsFromState(s.currentGatewaySkills(), persistedSkills),
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
		dst[key] = cloneGatewayMCPServerConfig(value)
	}
	return dst
}

func cloneGatewayMCPServerConfig(src gatewayMCPServerConfig) gatewayMCPServerConfig {
	dst := src
	if len(src.Args) > 0 {
		dst.Args = append([]string(nil), src.Args...)
	}
	if len(src.Env) > 0 {
		dst.Env = make(map[string]string, len(src.Env))
		for key, value := range src.Env {
			dst.Env[key] = value
		}
	}
	if len(src.Headers) > 0 {
		dst.Headers = make(map[string]string, len(src.Headers))
		for key, value := range src.Headers {
			dst.Headers[key] = value
		}
	}
	if src.OAuth != nil {
		oauth := *src.OAuth
		if len(src.OAuth.ExtraTokenParams) > 0 {
			oauth.ExtraTokenParams = make(map[string]string, len(src.OAuth.ExtraTokenParams))
			for key, value := range src.OAuth.ExtraTokenParams {
				oauth.ExtraTokenParams[key] = value
			}
		}
		dst.OAuth = &oauth
	}
	return dst
}

func gatewayExtensionsSkillsFromState(currentSkills, persistedSkills map[string]gatewaySkill) map[string]gatewayExtensionsSkillState {
	if len(persistedSkills) == 0 {
		return map[string]gatewayExtensionsSkillState{}
	}

	keys := make([]string, 0, len(persistedSkills))
	nameCounts := make(map[string]int, len(currentSkills))
	for _, skill := range currentSkills {
		if skill.Name != "" {
			nameCounts[skill.Name]++
		}
	}
	for key := range persistedSkills {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make(map[string]gatewayExtensionsSkillState, len(keys))
	for _, key := range keys {
		skill := persistedSkills[key]
		if skill.Name == "" {
			continue
		}
		stateKey := skill.Name
		if nameCounts[skill.Name] > 1 && skill.Category != "" {
			stateKey = skillStorageKey(skill.Category, skill.Name)
		}
		out[stateKey] = gatewayExtensionsSkillState{Enabled: skill.Enabled}
	}
	return out
}
