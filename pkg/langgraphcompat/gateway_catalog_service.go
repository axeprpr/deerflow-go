package langgraphcompat

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Server) listGatewayModels() []gatewayModel {
	s.uiStateMu.RLock()
	models := make([]gatewayModel, 0, len(s.models))
	for _, model := range s.getModelsLocked() {
		models = append(models, model)
	}
	s.uiStateMu.RUnlock()
	sort.Slice(models, func(i, j int) bool {
		return models[i].Name < models[j].Name
	})
	return models
}

func (s *Server) gatewayModelByName(modelName string) (gatewayModel, int, string) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return gatewayModel{}, http.StatusNotFound, "Model '' not found"
	}
	s.uiStateMu.RLock()
	model, ok := s.findModelLocked(modelName)
	s.uiStateMu.RUnlock()
	if !ok {
		return gatewayModel{}, http.StatusNotFound, fmt.Sprintf("Model '%s' not found", modelName)
	}
	return model, http.StatusOK, ""
}

func (s *Server) listGatewaySkills() []gatewaySkill {
	current := s.currentGatewaySkills()
	skills := make([]gatewaySkill, 0, len(current))
	for _, skill := range current {
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
	return skills
}

func (s *Server) gatewaySkillByName(name string) (gatewaySkill, int, string) {
	name = strings.TrimSpace(name)
	s.uiStateMu.RLock()
	skill, ok := s.getSkillsLocked()[name]
	s.uiStateMu.RUnlock()
	if !ok {
		return gatewaySkill{}, http.StatusNotFound, fmt.Sprintf("Skill '%s' not found", name)
	}
	return skill, http.StatusOK, ""
}

func (s *Server) setGatewaySkillEnabled(name string, enabled bool) (gatewaySkill, int, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return gatewaySkill{}, http.StatusBadRequest, "skill_name is required"
	}
	s.uiStateMu.Lock()
	skill, ok := s.getSkillsLocked()[name]
	if !ok {
		s.uiStateMu.Unlock()
		return gatewaySkill{}, http.StatusNotFound, fmt.Sprintf("Skill '%s' not found", name)
	}
	skill.Enabled = enabled
	s.skills[name] = skill
	s.uiStateMu.Unlock()
	if err := s.persistGatewayState(); err != nil {
		return gatewaySkill{}, http.StatusInternalServerError, "failed to persist state"
	}
	return skill, http.StatusOK, ""
}

func (s *Server) installGatewaySkill(threadID, archiveRef string) (gatewaySkill, int, string) {
	archivePath, err := s.resolveSkillArchivePath(threadID, archiveRef)
	if err != nil {
		return gatewaySkill{}, http.StatusBadRequest, err.Error()
	}
	if _, err := os.Stat(archivePath); err != nil {
		return gatewaySkill{}, http.StatusNotFound, "skill file not found"
	}
	if filepath.Ext(archivePath) != ".skill" {
		return gatewaySkill{}, http.StatusBadRequest, "file must have .skill extension"
	}

	skill, err := s.installSkillArchive(archivePath)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return gatewaySkill{}, http.StatusConflict, err.Error()
		}
		return gatewaySkill{}, http.StatusBadRequest, err.Error()
	}
	return skill, http.StatusOK, ""
}

func (s *Server) gatewayMCPConfig() gatewayMCPConfig {
	s.uiStateMu.RLock()
	defer s.uiStateMu.RUnlock()
	return mergeGatewayMCPConfig(defaultGatewayMCPConfig(), s.mcpConfig)
}

func (s *Server) updateGatewayMCPConfig(raw map[string]any) (gatewayMCPConfig, int, string) {
	req := gatewayMCPConfigFromMap(raw)
	req = mergeGatewayMCPConfig(defaultGatewayMCPConfig(), req)

	s.uiStateMu.Lock()
	s.mcpConfig = req
	s.uiStateMu.Unlock()
	if err := s.persistGatewayState(); err != nil {
		return gatewayMCPConfig{}, http.StatusInternalServerError, "failed to persist state"
	}
	return req, http.StatusOK, ""
}
