package langgraphcompat

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (s *Server) handleModelsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"models": s.listGatewayModels()})
}

func (s *Server) handleModelGet(w http.ResponseWriter, r *http.Request) {
	model, status, detail := s.gatewayModelByName(r.PathValue("model_name"))
	if detail != "" {
		writeJSON(w, status, map[string]any{"detail": detail})
		return
	}
	writeJSON(w, http.StatusOK, model)
}

func (s *Server) handleSkillsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"skills": s.listGatewaySkills()})
}

func (s *Server) handleSkillGet(w http.ResponseWriter, r *http.Request) {
	skill, status, detail := s.gatewaySkillByName(r.PathValue("skill_name"))
	if detail != "" {
		writeJSON(w, status, map[string]any{"detail": detail})
		return
	}
	writeJSON(w, http.StatusOK, skill)
}

func (s *Server) handleSkillSetEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	skill, status, detail := s.setGatewaySkillEnabled(r.PathValue("skill_name"), req.Enabled)
	if detail != "" {
		writeJSON(w, status, map[string]any{"detail": detail})
		return
	}
	writeJSON(w, status, skill)
}

func (s *Server) handleSkillInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ThreadID  string `json:"thread_id"`
		ThreadIDX string `json:"threadId"`
		Path      string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	threadID := firstNonEmpty(req.ThreadID, req.ThreadIDX)
	skill, status, detail := s.installGatewaySkill(threadID, req.Path)
	if detail != "" {
		writeJSON(w, status, map[string]any{"detail": detail})
		return
	}
	writeJSON(w, status, map[string]any{
		"success":    true,
		"skill_name": skill.Name,
		"message":    fmt.Sprintf("Skill '%s' installed successfully", skill.Name),
	})
}

func (s *Server) handleMCPConfigGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.gatewayMCPConfig())
}

func (s *Server) handleMCPConfigPut(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	cfg, status, detail := s.updateGatewayMCPConfig(raw)
	if detail != "" {
		writeJSON(w, status, map[string]any{"detail": detail})
		return
	}
	writeJSON(w, status, cfg)
}

func (s *Server) handleAgentsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"agents": s.listGatewayAgents()})
}

func (s *Server) handleAgentCheck(w http.ResponseWriter, r *http.Request) {
	name, available := s.checkGatewayAgentName(r.URL.Query().Get("name"))
	if name == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": "Invalid agent name"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": available, "name": name})
}

func (s *Server) handleAgentGet(w http.ResponseWriter, r *http.Request) {
	agent, status, detail := s.gatewayAgentByName(r.PathValue("name"))
	if detail != "" {
		writeJSON(w, status, map[string]any{"detail": detail})
		return
	}
	writeJSON(w, http.StatusOK, agent)
}

func (s *Server) handleAgentCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Model       *string  `json:"model"`
		ToolGroups  []string `json:"tool_groups"`
		ToolGroupsX []string `json:"toolGroups"`
		Soul        string   `json:"soul"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	created, status, detail := s.createGatewayAgent(gatewayAgent{
		Name:        req.Name,
		Description: req.Description,
		Model:       req.Model,
		ToolGroups:  firstNonEmptySlice(req.ToolGroups, req.ToolGroupsX),
		Soul:        req.Soul,
	})
	if detail != "" {
		writeJSON(w, status, map[string]any{"detail": detail})
		return
	}
	writeJSON(w, status, created)
}

func (s *Server) handleAgentUpdate(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	agent, status, detail := s.updateGatewayAgent(r.PathValue("name"), req)
	if detail != "" {
		writeJSON(w, status, map[string]any{"detail": detail})
		return
	}
	writeJSON(w, status, agent)
}

func (s *Server) handleAgentDelete(w http.ResponseWriter, r *http.Request) {
	status, detail := s.deleteGatewayAgent(r.Context(), r.PathValue("name"))
	if detail != "" {
		writeJSON(w, status, map[string]any{"detail": detail})
		return
	}
	w.WriteHeader(status)
}

func (s *Server) handleUserProfileGet(w http.ResponseWriter, r *http.Request) {
	content := s.gatewayUserProfileContent()
	if content == nil {
		writeJSON(w, http.StatusOK, map[string]any{"content": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": *content})
}

func (s *Server) handleUserProfilePut(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	content := stringFromAny(req["content"])
	status, detail := s.updateGatewayUserProfile(content)
	if detail != "" {
		writeJSON(w, status, map[string]any{"detail": detail})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": nullableString(content)})
}
