package langgraphcompat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func (s *Server) loadAgentsFromFiles() map[string]gatewayAgent {
	entries, err := os.ReadDir(s.agentsRoot())
	if err != nil {
		return nil
	}
	agents := map[string]gatewayAgent{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name, ok := normalizeAgentName(entry.Name())
		if !ok {
			continue
		}
		agent := gatewayAgent{Name: name}
		if loaded, ok := s.loadAgentConfigFile(name, "config.json"); ok {
			agent = loaded
		} else if loaded, ok := s.loadAgentConfigFile(name, "config.yaml"); ok {
			agent = loaded
		}
		agent.Name = name
		if soul, err := os.ReadFile(filepath.Join(s.agentDir(name), "SOUL.md")); err == nil {
			agent.Soul = string(soul)
		}
		if agent.Description == "" && agent.Soul == "" && agent.Model == nil && len(agent.ToolGroups) == 0 {
			continue
		}
		agents[name] = agent
	}
	return agents
}

func (s *Server) loadAgentConfigFile(name, filename string) (gatewayAgent, bool) {
	data, err := os.ReadFile(filepath.Join(s.agentDir(name), filename))
	if err != nil {
		return gatewayAgent{}, false
	}
	if strings.HasSuffix(filename, ".yaml") || strings.HasSuffix(filename, ".yml") {
		return parseGatewayAgentYAML(data, name)
	}
	return parseGatewayAgentJSON(data, name)
}

func parseGatewayAgentJSON(data []byte, fallbackName string) (gatewayAgent, bool) {
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil {
		if nested, ok := wrapper["agent"]; ok && len(nested) > 0 {
			data = nested
		} else if nested, ok := wrapper["config"]; ok && len(nested) > 0 {
			data = nested
		} else if nested, ok := wrapper["data"]; ok && len(nested) > 0 {
			data = nested
		}
	}
	agent := gatewayAgent{Name: fallbackName}
	if err := json.Unmarshal(data, &agent); err != nil {
		return gatewayAgent{}, false
	}
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	if agent.Name == "" {
		agent.Name = firstNonEmpty(stringFromAny(raw["name"]), fallbackName)
	}
	if agent.Description == "" {
		agent.Description = stringFromAny(raw["description"])
	}
	if rawModel := firstNonNil(raw["model"], raw["model_name"], raw["modelName"]); agent.Model == nil && rawModel != nil {
		model := stringFromAny(rawModel)
		agent.Model = &model
	}
	if len(agent.ToolGroups) == 0 {
		if rawToolGroups, exists := raw["tool_groups"]; exists {
			agent.ToolGroups = stringsFromAny(rawToolGroups)
		} else if rawToolGroups, exists := raw["toolGroups"]; exists {
			agent.ToolGroups = stringsFromAny(rawToolGroups)
		}
	}
	return agent, true
}

func parseGatewayAgentYAML(data []byte, fallbackName string) (gatewayAgent, bool) {
	var raw struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Model       *string  `yaml:"model"`
		ToolGroups  []string `yaml:"tool_groups"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return gatewayAgent{}, false
	}
	return gatewayAgent{
		Name:        firstNonEmpty(strings.TrimSpace(raw.Name), fallbackName),
		Description: strings.TrimSpace(raw.Description),
		Model:       raw.Model,
		ToolGroups:  append([]string(nil), raw.ToolGroups...),
	}, true
}

func (s *Server) persistAgentFiles(name string, agent gatewayAgent) error {
	dir := s.agentDir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cfg := gatewayAgent{
		Name:        name,
		Description: agent.Description,
		Model:       agent.Model,
		ToolGroups:  agent.ToolGroups,
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644); err != nil {
		return err
	}
	if data, err = yaml.Marshal(struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description,omitempty"`
		Model       *string  `yaml:"model,omitempty"`
		ToolGroups  []string `yaml:"tool_groups,omitempty"`
	}{
		Name:        name,
		Description: agent.Description,
		Model:       agent.Model,
		ToolGroups:  agent.ToolGroups,
	}); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0o644); err != nil {
		return err
	}
	if data, err = json.MarshalIndent(agent, "", "  "); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte(agent.Soul), 0o644)
}

func (s *Server) loadUserProfileFromFile() (string, bool) {
	data, err := os.ReadFile(s.userProfilePath())
	if err != nil {
		return "", false
	}
	var rawString string
	if err := json.Unmarshal(data, &rawString); err == nil {
		return rawString, true
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil {
		if nested, ok := wrapper["data"]; ok && len(nested) > 0 {
			data = nested
		}
	}
	if err := json.Unmarshal(data, &rawString); err == nil {
		return rawString, true
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err == nil {
		if value, ok := raw["content"]; ok {
			return stringFromAny(value), true
		}
		if value, ok := raw["user_profile"]; ok {
			return stringFromAny(value), true
		}
		if value, ok := raw["userProfile"]; ok {
			return stringFromAny(value), true
		}
	}
	return string(data), true
}

func (s *Server) persistUserProfileFile(content string) error {
	if err := os.MkdirAll(filepath.Dir(s.userProfilePath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.userProfilePath(), []byte(content), 0o644)
}

func (s *Server) listGatewayAgents() []gatewayAgent {
	s.refreshGatewayCompatFilesIfManaged()
	s.uiStateMu.RLock()
	agents := make([]gatewayAgent, 0, len(s.getAgentsLocked()))
	for _, a := range s.getAgentsLocked() {
		out := a
		out.Soul = ""
		agents = append(agents, out)
	}
	s.uiStateMu.RUnlock()
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents
}

func (s *Server) checkGatewayAgentName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if !agentNameRE.MatchString(name) {
		return "", false
	}
	normalized := strings.ToLower(name)
	s.uiStateMu.RLock()
	_, exists := s.getAgentsLocked()[normalized]
	s.uiStateMu.RUnlock()
	return normalized, !exists
}

func (s *Server) gatewayAgentByName(name string) (gatewayAgent, int, string) {
	normalized, ok := normalizeAgentName(name)
	if !ok {
		return gatewayAgent{}, http.StatusUnprocessableEntity, "Invalid agent name"
	}
	s.uiStateMu.RLock()
	agent, exists := s.getAgentsLocked()[normalized]
	s.uiStateMu.RUnlock()
	if !exists {
		return gatewayAgent{}, http.StatusNotFound, fmt.Sprintf("Agent '%s' not found", normalized)
	}
	return agent, http.StatusOK, ""
}

func (s *Server) createGatewayAgent(agentReq gatewayAgent) (gatewayAgent, int, string) {
	name, ok := normalizeAgentName(agentReq.Name)
	if !ok {
		return gatewayAgent{}, http.StatusUnprocessableEntity, "Invalid agent name"
	}

	s.uiStateMu.Lock()
	agents := s.getAgentsLocked()
	if _, exists := agents[name]; exists {
		s.uiStateMu.Unlock()
		return gatewayAgent{}, http.StatusConflict, fmt.Sprintf("Agent '%s' already exists", name)
	}
	agentReq.Name = name
	agents[name] = agentReq
	s.uiStateMu.Unlock()

	if err := s.persistAgentFiles(name, agentReq); err != nil {
		return gatewayAgent{}, http.StatusInternalServerError, "failed to persist agent files"
	}
	if err := s.persistGatewayState(); err != nil {
		return gatewayAgent{}, http.StatusInternalServerError, "failed to persist state"
	}
	return agentReq, http.StatusCreated, ""
}

func (s *Server) updateGatewayAgent(name string, req map[string]any) (gatewayAgent, int, string) {
	normalized, ok := normalizeAgentName(name)
	if !ok {
		return gatewayAgent{}, http.StatusUnprocessableEntity, "Invalid agent name"
	}

	s.uiStateMu.Lock()
	agents := s.getAgentsLocked()
	agent, exists := agents[normalized]
	if !exists {
		s.uiStateMu.Unlock()
		return gatewayAgent{}, http.StatusNotFound, fmt.Sprintf("Agent '%s' not found", normalized)
	}
	if rawDescription, exists := req["description"]; exists {
		agent.Description = stringFromAny(rawDescription)
	}
	if rawModel, exists := req["model"]; exists {
		if rawModel == nil {
			agent.Model = nil
		} else {
			model := stringFromAny(rawModel)
			agent.Model = &model
		}
	}
	if rawToolGroups, exists := req["tool_groups"]; exists {
		if rawToolGroups == nil {
			agent.ToolGroups = nil
		} else {
			agent.ToolGroups = stringsFromAny(rawToolGroups)
		}
	} else if rawToolGroups, exists := req["toolGroups"]; exists {
		if rawToolGroups == nil {
			agent.ToolGroups = nil
		} else {
			agent.ToolGroups = stringsFromAny(rawToolGroups)
		}
	}
	if rawSoul, exists := req["soul"]; exists {
		agent.Soul = stringFromAny(rawSoul)
	}
	agents[normalized] = agent
	s.uiStateMu.Unlock()

	if err := s.persistAgentFiles(normalized, agent); err != nil {
		return gatewayAgent{}, http.StatusInternalServerError, "failed to persist agent files"
	}
	if err := s.persistGatewayState(); err != nil {
		return gatewayAgent{}, http.StatusInternalServerError, "failed to persist state"
	}
	return agent, http.StatusOK, ""
}

func (s *Server) deleteGatewayAgent(ctx context.Context, name string) (int, string) {
	normalized, ok := normalizeAgentName(name)
	if !ok {
		return http.StatusUnprocessableEntity, "Invalid agent name"
	}

	s.uiStateMu.Lock()
	agents := s.getAgentsLocked()
	if _, exists := agents[normalized]; !exists {
		s.uiStateMu.Unlock()
		return http.StatusNotFound, fmt.Sprintf("Agent '%s' not found", normalized)
	}
	delete(agents, normalized)
	s.uiStateMu.Unlock()

	if err := os.RemoveAll(s.agentDir(normalized)); err != nil {
		return http.StatusInternalServerError, "failed to delete agent files"
	}
	if s.memoryRuntime != nil && s.memoryRuntime.Store() != nil {
		if deleter, ok := any(s.memoryRuntime.Store()).(interface {
			Delete(context.Context, string) error
		}); ok {
			_ = deleter.Delete(ctx, deriveMemorySessionID("", normalized))
		}
	}
	if err := s.persistGatewayState(); err != nil {
		return http.StatusInternalServerError, "failed to persist state"
	}
	return http.StatusNoContent, ""
}

func (s *Server) gatewayUserProfileContent() *string {
	s.refreshGatewayCompatFilesIfManaged()
	s.uiStateMu.RLock()
	content := s.getUserProfileLocked()
	s.uiStateMu.RUnlock()
	if strings.TrimSpace(content) == "" {
		return nil
	}
	return &content
}

func (s *Server) updateGatewayUserProfile(content string) (int, string) {
	s.uiStateMu.Lock()
	s.setUserProfileLocked(content)
	s.uiStateMu.Unlock()
	if err := s.persistUserProfileFile(content); err != nil {
		return http.StatusInternalServerError, "failed to persist user profile"
	}
	if err := s.persistGatewayState(); err != nil {
		return http.StatusInternalServerError, "failed to persist state"
	}
	return http.StatusOK, ""
}
