package langgraphcompat

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func (s *Server) agentsDir() string {
	return filepath.Join(s.dataRoot, "agents")
}

func (s *Server) userProfilePath() string {
	return filepath.Join(s.dataRoot, "USER.md")
}

func (s *Server) loadGatewayCompatFiles() error {
	s.uiStateMu.Lock()
	defer s.uiStateMu.Unlock()

	if agents, ok := s.loadAgentsFromDiskLocked(); ok {
		s.setAgentsLocked(agents)
	}
	if profile, ok := s.loadUserProfileFromDiskLocked(); ok {
		s.setUserProfileLocked(profile)
	}
	return nil
}

func (s *Server) loadAgentsFromDiskLocked() (map[string]gatewayAgent, bool) {
	agentsDir := s.agentsDir()
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, false
	}

	agents := make(map[string]gatewayAgent, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agent, ok := s.loadAgentFromDisk(entry.Name())
		if !ok {
			continue
		}
		agents[agent.Name] = agent
	}
	return agents, true
}

func (s *Server) loadAgentFromDisk(name string) (gatewayAgent, bool) {
	normalized, ok := normalizeAgentName(name)
	if !ok {
		return gatewayAgent{}, false
	}

	dir := s.agentDir(normalized)
	configPath := filepath.Join(dir, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return gatewayAgent{}, false
	}

	var raw struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Model       *string  `yaml:"model"`
		ToolGroups  []string `yaml:"tool_groups"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return gatewayAgent{}, false
	}

	soul, err := os.ReadFile(filepath.Join(dir, "SOUL.md"))
	if err != nil && !os.IsNotExist(err) {
		return gatewayAgent{}, false
	}

	loadedName := normalized
	if candidate, ok := normalizeAgentName(raw.Name); ok {
		loadedName = candidate
	}

	agent := gatewayAgent{
		Name:        loadedName,
		Description: strings.TrimSpace(raw.Description),
		Model:       raw.Model,
		ToolGroups:  append([]string(nil), raw.ToolGroups...),
		Soul:        strings.TrimSpace(string(soul)),
	}
	return agent, true
}

func (s *Server) loadUserProfileFromDiskLocked() (string, bool) {
	data, err := os.ReadFile(s.userProfilePath())
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}
