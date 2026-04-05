package langgraphcompat

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

type gatewayModel struct {
	ID                      string `json:"id"`
	Name                    string `json:"name"`
	Model                   string `json:"model"`
	DisplayName             string `json:"display_name"`
	Description             string `json:"description,omitempty"`
	SupportsThinking        bool   `json:"supports_thinking,omitempty"`
	SupportsReasoningEffort bool   `json:"supports_reasoning_effort,omitempty"`
}

type gatewaySkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	License     string `json:"license"`
	Enabled     bool   `json:"enabled"`
}

type gatewayMCPServerConfig struct {
	Enabled     bool                   `json:"enabled"`
	Type        string                 `json:"type,omitempty"`
	Command     string                 `json:"command,omitempty"`
	Args        []string               `json:"args,omitempty"`
	Env         map[string]string      `json:"env,omitempty"`
	URL         string                 `json:"url,omitempty"`
	Headers     map[string]string      `json:"headers,omitempty"`
	OAuth       *gatewayMCPOAuthConfig `json:"oauth,omitempty"`
	Description string                 `json:"description"`
}

type gatewayMCPOAuthConfig struct {
	Enabled           bool              `json:"enabled"`
	TokenURL          string            `json:"token_url,omitempty"`
	GrantType         string            `json:"grant_type,omitempty"`
	ClientID          string            `json:"client_id,omitempty"`
	ClientSecret      string            `json:"client_secret,omitempty"`
	RefreshToken      string            `json:"refresh_token,omitempty"`
	Scope             string            `json:"scope,omitempty"`
	Audience          string            `json:"audience,omitempty"`
	TokenField        string            `json:"token_field,omitempty"`
	TokenTypeField    string            `json:"token_type_field,omitempty"`
	ExpiresInField    string            `json:"expires_in_field,omitempty"`
	DefaultTokenType  string            `json:"default_token_type,omitempty"`
	RefreshSkewSecond int               `json:"refresh_skew_seconds"`
	ExtraTokenParams  map[string]string `json:"extra_token_params,omitempty"`
}

type gatewayMCPConfig struct {
	MCPServers map[string]gatewayMCPServerConfig `json:"mcp_servers"`
}

type gatewayPersistedState struct {
	Models      map[string]gatewayModel `json:"models,omitempty"`
	Skills      map[string]gatewaySkill `json:"skills"`
	MCPConfig   gatewayMCPConfig        `json:"mcp_config"`
	Agents      map[string]gatewayAgent `json:"agents,omitempty"`
	UserProfile string                  `json:"user_profile,omitempty"`
	Memory      gatewayMemoryResponse   `json:"memory"`
}

const maxSkillArchiveSize int64 = 512 << 20

var skillInstallSeq uint64
var agentNameRE = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

type gatewayAgent struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Model       *string  `json:"model"`
	ToolGroups  []string `json:"tool_groups"`
	Soul        string   `json:"soul,omitempty"`
}

type memorySection struct {
	Summary   string `json:"summary"`
	UpdatedAt string `json:"updatedAt"`
}

type memoryUser struct {
	WorkContext     memorySection `json:"workContext"`
	PersonalContext memorySection `json:"personalContext"`
	TopOfMind       memorySection `json:"topOfMind"`
}

type memoryHistory struct {
	RecentMonths       memorySection `json:"recentMonths"`
	EarlierContext     memorySection `json:"earlierContext"`
	LongTermBackground memorySection `json:"longTermBackground"`
}

type memoryFact struct {
	ID         string  `json:"id"`
	Content    string  `json:"content"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
	CreatedAt  string  `json:"createdAt"`
	Source     string  `json:"source"`
}

type gatewayMemoryResponse struct {
	Version     string        `json:"version"`
	LastUpdated string        `json:"lastUpdated"`
	User        memoryUser    `json:"user"`
	History     memoryHistory `json:"history"`
	Facts       []memoryFact  `json:"facts"`
}

type gatewayMemoryConfig struct {
	Enabled                 bool    `json:"enabled"`
	StoragePath             string  `json:"storage_path"`
	DebounceSeconds         int     `json:"debounce_seconds"`
	MaxFacts                int     `json:"max_facts"`
	FactConfidenceThreshold float64 `json:"fact_confidence_threshold"`
	InjectionEnabled        bool    `json:"injection_enabled"`
	MaxInjectionTokens      int     `json:"max_injection_tokens"`
}

type gatewayChannelsStatus struct {
	ServiceRunning bool                      `json:"service_running"`
	Channels       map[string]map[string]any `json:"channels"`
}

func (s *Server) registerGatewayRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/models", s.handleModelsList)
	mux.HandleFunc("GET /api/models/{model_name...}", s.handleModelGet)
	mux.HandleFunc("GET /api/skills", s.handleSkillsList)
	mux.HandleFunc("GET /api/skills/{skill_name}", s.handleSkillGet)
	mux.HandleFunc("PUT /api/skills/{skill_name}", s.handleSkillSetEnabled)
	mux.HandleFunc("POST /api/skills/install", s.handleSkillInstall)
	mux.HandleFunc("GET /api/agents", s.handleAgentsList)
	mux.HandleFunc("POST /api/agents", s.handleAgentCreate)
	mux.HandleFunc("GET /api/agents/check", s.handleAgentCheck)
	mux.HandleFunc("GET /api/agents/{name}", s.handleAgentGet)
	mux.HandleFunc("PUT /api/agents/{name}", s.handleAgentUpdate)
	mux.HandleFunc("DELETE /api/agents/{name}", s.handleAgentDelete)
	mux.HandleFunc("GET /api/user-profile", s.handleUserProfileGet)
	mux.HandleFunc("PUT /api/user-profile", s.handleUserProfilePut)
	mux.HandleFunc("GET /api/memory", s.handleMemoryGet)
	mux.HandleFunc("POST /api/memory/reload", s.handleMemoryReload)
	mux.HandleFunc("DELETE /api/memory", s.handleMemoryClear)
	mux.HandleFunc("DELETE /api/memory/facts/{fact_id}", s.handleMemoryFactDelete)
	mux.HandleFunc("GET /api/memory/config", s.handleMemoryConfigGet)
	mux.HandleFunc("GET /api/memory/status", s.handleMemoryStatusGet)
	mux.HandleFunc("GET /api/channels", s.handleChannelsGet)
	mux.HandleFunc("POST /api/channels/{name}/restart", s.handleChannelRestart)
	mux.HandleFunc("GET /api/mcp/config", s.handleMCPConfigGet)
	mux.HandleFunc("PUT /api/mcp/config", s.handleMCPConfigPut)
	mux.HandleFunc("DELETE /api/threads/{thread_id}", s.handleGatewayThreadDelete)
	mux.HandleFunc("POST /api/threads/{thread_id}/uploads", s.handleUploadsCreate)
	mux.HandleFunc("GET /api/threads/{thread_id}/uploads/list", s.handleUploadsList)
	mux.HandleFunc("DELETE /api/threads/{thread_id}/uploads/{filename}", s.handleUploadsDelete)
	mux.HandleFunc("GET /api/threads/{thread_id}/artifacts/{artifact_path...}", s.handleArtifactGet)
	mux.HandleFunc("POST /api/threads/{thread_id}/suggestions", s.handleSuggestions)
}

func (s *Server) handleModelsList(w http.ResponseWriter, r *http.Request) {
	s.uiStateMu.RLock()
	models := make([]gatewayModel, 0, len(s.models))
	for _, model := range s.getModelsLocked() {
		models = append(models, model)
	}
	s.uiStateMu.RUnlock()
	sort.Slice(models, func(i, j int) bool {
		return models[i].Name < models[j].Name
	})
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (s *Server) handleModelGet(w http.ResponseWriter, r *http.Request) {
	modelName := strings.TrimSpace(r.PathValue("model_name"))
	if modelName == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "Model '' not found"})
		return
	}
	s.uiStateMu.RLock()
	model, ok := s.findModelLocked(modelName)
	s.uiStateMu.RUnlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": fmt.Sprintf("Model '%s' not found", modelName)})
		return
	}
	writeJSON(w, http.StatusOK, model)
}

func (s *Server) handleSkillsList(w http.ResponseWriter, r *http.Request) {
	s.uiStateMu.RLock()
	defer s.uiStateMu.RUnlock()
	skills := make([]gatewaySkill, 0, len(s.getSkillsLocked()))
	for _, skill := range s.getSkillsLocked() {
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
	writeJSON(w, http.StatusOK, map[string]any{"skills": skills})
}

func (s *Server) handleSkillGet(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("skill_name"))
	s.uiStateMu.RLock()
	skill, ok := s.getSkillsLocked()[name]
	s.uiStateMu.RUnlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": fmt.Sprintf("Skill '%s' not found", name)})
		return
	}
	writeJSON(w, http.StatusOK, skill)
}

func (s *Server) handleSkillSetEnabled(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("skill_name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "skill_name is required"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}

	s.uiStateMu.Lock()
	skill, ok := s.getSkillsLocked()[name]
	if !ok {
		s.uiStateMu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "skill not found"})
		return
	}
	skill.Enabled = req.Enabled
	s.skills[name] = skill
	s.uiStateMu.Unlock()
	if err := s.persistGatewayState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "skill": skill})
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
	archivePath, err := s.resolveSkillArchivePath(threadID, req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	if _, err := os.Stat(archivePath); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "skill file not found"})
		return
	}
	if filepath.Ext(archivePath) != ".skill" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "file must have .skill extension"})
		return
	}

	skill, err := s.installSkillArchive(archivePath)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeJSON(w, http.StatusConflict, map[string]any{"detail": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"skill_name": skill.Name,
		"message":    fmt.Sprintf("Skill '%s' installed successfully", skill.Name),
		"skill":      skill,
	})
}

func (s *Server) handleMCPConfigGet(w http.ResponseWriter, r *http.Request) {
	s.uiStateMu.RLock()
	defer s.uiStateMu.RUnlock()
	writeJSON(w, http.StatusOK, s.mcpConfig)
}

func (s *Server) handleMCPConfigPut(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	req := gatewayMCPConfigFromMap(raw)
	if req.MCPServers == nil {
		req.MCPServers = map[string]gatewayMCPServerConfig{}
	}
	for name, server := range req.MCPServers {
		req.MCPServers[name] = normalizeGatewayMCPServer(server)
	}

	s.uiStateMu.Lock()
	s.mcpConfig = req
	s.uiStateMu.Unlock()
	if err := s.persistGatewayState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) handleAgentsList(w http.ResponseWriter, r *http.Request) {
	s.uiStateMu.RLock()
	agents := make([]gatewayAgent, 0, len(s.getAgentsLocked()))
	for _, a := range s.getAgentsLocked() {
		out := a
		out.Soul = ""
		agents = append(agents, out)
	}
	s.uiStateMu.RUnlock()
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

func (s *Server) handleAgentCheck(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if !agentNameRE.MatchString(name) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": "Invalid agent name"})
		return
	}
	normalized := strings.ToLower(name)
	s.uiStateMu.RLock()
	_, exists := s.getAgentsLocked()[normalized]
	s.uiStateMu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"available": !exists, "name": normalized})
}

func (s *Server) handleAgentGet(w http.ResponseWriter, r *http.Request) {
	name, ok := normalizeAgentName(r.PathValue("name"))
	if !ok {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": "Invalid agent name"})
		return
	}
	s.uiStateMu.RLock()
	agent, exists := s.getAgentsLocked()[name]
	s.uiStateMu.RUnlock()
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": fmt.Sprintf("Agent '%s' not found", name)})
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
	agentReq := gatewayAgent{
		Name:        req.Name,
		Description: req.Description,
		Model:       req.Model,
		ToolGroups:  firstNonEmptySlice(req.ToolGroups, req.ToolGroupsX),
		Soul:        req.Soul,
	}
	name, ok := normalizeAgentName(agentReq.Name)
	if !ok {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": "Invalid agent name"})
		return
	}

	s.uiStateMu.Lock()
	agents := s.getAgentsLocked()
	if _, exists := agents[name]; exists {
		s.uiStateMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]any{"detail": fmt.Sprintf("Agent '%s' already exists", name)})
		return
	}
	agentReq.Name = name
	agents[name] = agentReq
	s.uiStateMu.Unlock()
	if err := s.persistAgentFiles(name, agentReq); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist agent files"})
		return
	}
	if err := s.persistGatewayState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	writeJSON(w, http.StatusCreated, agentReq)
}

func (s *Server) handleAgentUpdate(w http.ResponseWriter, r *http.Request) {
	name, ok := normalizeAgentName(r.PathValue("name"))
	if !ok {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": "Invalid agent name"})
		return
	}
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}

	s.uiStateMu.Lock()
	agents := s.getAgentsLocked()
	agent, exists := agents[name]
	if !exists {
		s.uiStateMu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": fmt.Sprintf("Agent '%s' not found", name)})
		return
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
	agents[name] = agent
	s.uiStateMu.Unlock()
	if err := s.persistAgentFiles(name, agent); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist agent files"})
		return
	}
	if err := s.persistGatewayState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	writeJSON(w, http.StatusOK, agent)
}

func (s *Server) handleAgentDelete(w http.ResponseWriter, r *http.Request) {
	name, ok := normalizeAgentName(r.PathValue("name"))
	if !ok {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": "Invalid agent name"})
		return
	}
	s.uiStateMu.Lock()
	agents := s.getAgentsLocked()
	if _, exists := agents[name]; !exists {
		s.uiStateMu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": fmt.Sprintf("Agent '%s' not found", name)})
		return
	}
	delete(agents, name)
	s.uiStateMu.Unlock()
	if err := os.RemoveAll(s.agentDir(name)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to delete agent files"})
		return
	}
	if err := s.persistGatewayState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUserProfileGet(w http.ResponseWriter, r *http.Request) {
	s.uiStateMu.RLock()
	content := s.getUserProfileLocked()
	s.uiStateMu.RUnlock()
	if strings.TrimSpace(content) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"content": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": content})
}

func (s *Server) handleUserProfilePut(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	content := stringFromAny(req["content"])
	s.uiStateMu.Lock()
	s.setUserProfileLocked(content)
	s.uiStateMu.Unlock()
	if err := os.WriteFile(s.userProfilePath(), []byte(content), 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist user profile"})
		return
	}
	if err := s.persistGatewayState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": nullableString(content)})
}

func (s *Server) handleMemoryGet(w http.ResponseWriter, r *http.Request) {
	s.uiStateMu.RLock()
	m := s.getMemoryLocked()
	s.uiStateMu.RUnlock()
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleMemoryReload(w http.ResponseWriter, r *http.Request) {
	s.handleMemoryGet(w, r)
}

func (s *Server) handleMemoryClear(w http.ResponseWriter, r *http.Request) {
	s.uiStateMu.Lock()
	s.setMemoryLocked(defaultGatewayMemory())
	s.uiStateMu.Unlock()
	if err := s.persistMemoryFile(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist memory"})
		return
	}
	if err := s.persistGatewayState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	s.handleMemoryGet(w, r)
}

func (s *Server) handleMemoryFactDelete(w http.ResponseWriter, r *http.Request) {
	factID := strings.TrimSpace(r.PathValue("fact_id"))
	s.uiStateMu.Lock()
	mem := s.getMemoryLocked()
	newFacts := make([]memoryFact, 0, len(mem.Facts))
	found := false
	for _, fact := range mem.Facts {
		if fact.ID == factID {
			found = true
			continue
		}
		newFacts = append(newFacts, fact)
	}
	if !found {
		s.uiStateMu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": fmt.Sprintf("Memory fact '%s' not found", factID)})
		return
	}
	mem.Facts = newFacts
	mem.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	s.setMemoryLocked(mem)
	s.uiStateMu.Unlock()
	if err := s.persistMemoryFile(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist memory"})
		return
	}
	if err := s.persistGatewayState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	writeJSON(w, http.StatusOK, mem)
}

func (s *Server) handleMemoryConfigGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.memoryConfig)
}

func (s *Server) handleMemoryStatusGet(w http.ResponseWriter, r *http.Request) {
	s.uiStateMu.RLock()
	mem := s.getMemoryLocked()
	s.uiStateMu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"config": s.memoryConfig,
		"data":   mem,
	})
}

func (s *Server) handleChannelsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.channels)
}

func (s *Server) handleChannelRestart(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if _, ok := s.channels.Channels[name]; ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"message": fmt.Sprintf("Channel %s restart requested", name),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": false,
		"message": fmt.Sprintf("Channel %s is not running in deerflow-go", name),
	})
}

func (s *Server) handleGatewayThreadDelete(w http.ResponseWriter, r *http.Request) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	if threadID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "thread_id is required"})
		return
	}
	if err := os.RemoveAll(s.threadRoot(threadID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to delete local thread data"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "local thread data deleted",
	})
}

func (s *Server) handleUploadsCreate(w http.ResponseWriter, r *http.Request) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	if threadID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "thread_id is required"})
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid multipart form"})
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "no files uploaded"})
		return
	}

	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to create upload dir"})
		return
	}

	infos := make([]map[string]any, 0, len(files))
	for _, fh := range files {
		info, err := s.saveUploadedFile(threadID, uploadDir, fh)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
			return
		}
		infos = append(infos, info)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"files":   infos,
		"message": "files uploaded",
	})
}

func (s *Server) handleUploadsList(w http.ResponseWriter, r *http.Request) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	if threadID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "thread_id is required"})
		return
	}

	uploadDir := s.uploadsDir(threadID)
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]any{"files": []any{}, "count": 0})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to list uploads"})
		return
	}

	files := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		fullPath := filepath.Join(uploadDir, name)
		stat, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, s.uploadInfo(threadID, fullPath, name, stat.Size(), stat.ModTime().Unix()))
	}

	sort.Slice(files, func(i, j int) bool {
		li := toInt64(files[i]["modified"])
		lj := toInt64(files[j]["modified"])
		if li == lj {
			return asString(files[i]["filename"]) < asString(files[j]["filename"])
		}
		return li > lj
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"files": files,
		"count": len(files),
	})
}

func (s *Server) handleUploadsDelete(w http.ResponseWriter, r *http.Request) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	filename := sanitizeFilename(r.PathValue("filename"))
	if threadID == "" || filename == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request"})
		return
	}

	target := filepath.Join(s.uploadsDir(threadID), filename)
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to delete file"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "file deleted",
	})
}

func (s *Server) handleArtifactGet(w http.ResponseWriter, r *http.Request) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	artifactPath := strings.TrimSpace(r.PathValue("artifact_path"))
	if threadID == "" || artifactPath == "" {
		http.NotFound(w, r)
		return
	}

	absPath := s.resolveArtifactPath(threadID, artifactPath)
	if !s.artifactAllowed(threadID, absPath) {
		http.NotFound(w, r)
		return
	}
	if _, err := os.Stat(absPath); err != nil {
		http.NotFound(w, r)
		return
	}

	if strings.EqualFold(r.URL.Query().Get("download"), "true") {
		w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(absPath)+`"`)
	}
	http.ServeFile(w, r, absPath)
}

func (s *Server) handleSuggestions(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		N          int    `json:"n"`
		Count      int    `json:"count"`
		ModelName  string `json:"model_name"`
		ModelNameX string `json:"modelName"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"suggestions": []string{}})
		return
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &raw)
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"suggestions": []string{}})
		return
	}
	_, hasN := raw["n"]
	_, hasCount := raw["count"]
	countProvided := hasN || hasCount
	if req.N == 0 {
		req.N = req.Count
	}
	if req.N < 0 {
		req.N = 0
	}
	if !countProvided && req.N == 0 {
		req.N = 3
	}
	if req.N > 5 {
		req.N = 5
	}
	if req.N == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"suggestions": []string{}})
		return
	}

	lastUser := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if strings.EqualFold(req.Messages[i].Role, "user") {
			lastUser = strings.TrimSpace(req.Messages[i].Content)
			break
		}
	}
	if lastUser == "" {
		writeJSON(w, http.StatusOK, map[string]any{"suggestions": []string{}})
		return
	}

	subject := compactSubject(lastUser)
	candidates := []string{
		"请基于以上内容给出一个可执行的分步计划。",
		"请总结关键结论，并标注不确定性。",
		"请给出 3 个下一步可选方案并比较利弊。",
	}
	if subject != "" {
		candidates[0] = "围绕“" + subject + "”给出一个可执行的分步计划。"
		candidates[1] = "继续深入“" + subject + "”：请总结关键结论并标注不确定性。"
	}
	if req.N < len(candidates) {
		candidates = candidates[:req.N]
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": candidates})
}

func (s *Server) saveUploadedFile(threadID, uploadDir string, fh *multipart.FileHeader) (map[string]any, error) {
	name := sanitizeFilename(fh.Filename)
	if name == "" {
		return nil, errBadFileName
	}

	src, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	dstPath := filepath.Join(uploadDir, name)
	dst, err := os.Create(dstPath)
	if err != nil {
		return nil, err
	}
	defer dst.Close()

	n, err := io.Copy(dst, src)
	if err != nil {
		return nil, err
	}
	return s.uploadInfo(threadID, dstPath, name, n, nowUnix()), nil
}

func (s *Server) uploadInfo(threadID, fullPath, name string, size int64, modified int64) map[string]any {
	virtualPath := "/mnt/user-data/uploads/" + name
	return map[string]any{
		"filename":     name,
		"size":         size,
		"path":         fullPath,
		"virtual_path": virtualPath,
		"artifact_url": "/api/threads/" + threadID + "/artifacts" + virtualPath,
		"extension":    strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), "."),
		"modified":     modified,
	}
}

func (s *Server) artifactAllowed(threadID, absPath string) bool {
	threadRoot := s.threadRoot(threadID)
	threadRootPrefix := filepath.Clean(threadRoot) + string(filepath.Separator)
	if strings.HasPrefix(absPath, threadRootPrefix) {
		return true
	}

	s.sessionsMu.RLock()
	session := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if session == nil || session.PresentFiles == nil {
		return false
	}
	for _, file := range session.PresentFiles.List() {
		if filepath.Clean(file.Path) == absPath {
			return true
		}
	}
	for _, path := range sessionArtifactPaths(session) {
		if filepath.Clean(path) == absPath {
			return true
		}
	}
	return false
}

func (s *Server) resolveArtifactPath(threadID, artifactPath string) string {
	clean := filepath.Clean("/" + strings.TrimSpace(artifactPath))
	if strings.HasPrefix(clean, "/mnt/user-data/") {
		suffix := strings.TrimPrefix(clean, "/mnt/user-data/")
		return filepath.Join(s.threadRoot(threadID), filepath.FromSlash(suffix))
	}
	return clean
}

func (s *Server) threadRoot(threadID string) string {
	return filepath.Join(s.dataRoot, "threads", threadID, "user-data")
}

func (s *Server) uploadsDir(threadID string) string {
	return filepath.Join(s.threadRoot(threadID), "uploads")
}

var errBadFileName = errors.New("invalid filename")

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name == "." || name == "" {
		return ""
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return ""
		}
	}
	return name
}

func compactSubject(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if len(text) > 48 {
		return text[:48]
	}
	return text
}

func nowUnix() int64 { return time.Now().UTC().Unix() }

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	default:
		return 0
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func (s *Server) resolveSkillArchivePath(threadID, path string) (string, error) {
	threadID = strings.TrimSpace(threadID)
	path = strings.TrimSpace(path)
	if threadID == "" || path == "" {
		return "", errors.New("thread_id and path are required")
	}
	if strings.HasPrefix(path, "/mnt/user-data/") {
		suffix := strings.TrimPrefix(path, "/mnt/user-data/")
		return filepath.Join(s.threadRoot(threadID), filepath.FromSlash(suffix)), nil
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Join(s.uploadsDir(threadID), filepath.Base(path)), nil
}

func (s *Server) installSkillArchive(archivePath string) (gatewaySkill, error) {
	skillsRoot := filepath.Join(s.dataRoot, "skills", "custom")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		return gatewaySkill{}, err
	}

	tempDir := filepath.Join(s.dataRoot, "tmp", fmt.Sprintf("skill-install-%d", atomic.AddUint64(&skillInstallSeq, 1)))
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return gatewaySkill{}, err
	}
	defer os.RemoveAll(tempDir)

	zipReader, err := zip.OpenReader(archivePath)
	if err != nil {
		return gatewaySkill{}, errors.New("file is not a valid ZIP archive")
	}
	defer zipReader.Close()

	var written int64
	for _, f := range zipReader.File {
		if strings.Contains(f.Name, "..") || strings.HasPrefix(f.Name, "/") || strings.HasPrefix(f.Name, "\\") {
			return gatewaySkill{}, errors.New("archive contains unsafe path")
		}
		target := filepath.Join(tempDir, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(tempDir)+string(filepath.Separator)) {
			return gatewaySkill{}, errors.New("archive entry escapes destination")
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return gatewaySkill{}, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return gatewaySkill{}, err
		}
		src, err := f.Open()
		if err != nil {
			return gatewaySkill{}, err
		}
		dst, err := os.Create(target)
		if err != nil {
			src.Close()
			return gatewaySkill{}, err
		}
		n, copyErr := io.Copy(dst, src)
		src.Close()
		dst.Close()
		if copyErr != nil {
			return gatewaySkill{}, copyErr
		}
		written += n
		if written > maxSkillArchiveSize {
			return gatewaySkill{}, errors.New("skill archive is too large")
		}
	}

	root, err := resolveArchiveSkillRoot(tempDir)
	if err != nil {
		return gatewaySkill{}, err
	}
	skillFile := filepath.Join(root, "SKILL.md")
	content, err := os.ReadFile(skillFile)
	if err != nil {
		return gatewaySkill{}, errors.New("invalid skill: missing SKILL.md")
	}
	metadata := parseSkillFrontmatter(string(content))
	skillName := metadata["name"]
	if skillName == "" {
		skillName = sanitizeSkillName(filepath.Base(root))
	}
	if skillName == "" {
		return gatewaySkill{}, errors.New("invalid skill: missing name")
	}

	targetDir := filepath.Join(skillsRoot, skillName)
	if _, err := os.Stat(targetDir); err == nil {
		return gatewaySkill{}, fmt.Errorf("skill '%s' already exists", skillName)
	}
	if err := copyDir(root, targetDir); err != nil {
		return gatewaySkill{}, err
	}

	skill := gatewaySkill{
		Name:        skillName,
		Description: firstNonEmpty(metadata["description"], "Installed from .skill archive"),
		Category:    firstNonEmpty(metadata["category"], "custom"),
		License:     firstNonEmpty(metadata["license"], "Unknown"),
		Enabled:     true,
	}

	s.uiStateMu.Lock()
	s.skills = s.discoverGatewaySkills(map[string]bool{skillName: true})
	s.uiStateMu.Unlock()
	if err := s.persistGatewayState(); err != nil {
		return gatewaySkill{}, err
	}
	return skill, nil
}

func resolveArchiveSkillRoot(tempDir string) (string, error) {
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return "", err
	}
	filtered := make([]os.DirEntry, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "__MACOSX" {
			continue
		}
		filtered = append(filtered, e)
	}
	if len(filtered) == 0 {
		return "", errors.New("skill archive is empty")
	}
	if len(filtered) == 1 && filtered[0].IsDir() {
		return filepath.Join(tempDir, filtered[0].Name()), nil
	}
	return tempDir, nil
}

func parseSkillFrontmatter(content string) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return result
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		switch key {
		case "name":
			result["name"] = sanitizeSkillName(value)
		case "description":
			result["description"] = value
		case "category":
			result["category"] = value
		case "license":
			result["license"] = value
		}
	}
	return result
}

func sanitizeSkillName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-_")
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

func (s *Server) gatewayStatePath() string {
	return filepath.Join(s.dataRoot, "gateway_state.json")
}

func (s *Server) agentsRoot() string {
	return filepath.Join(s.dataRoot, "agents")
}

func (s *Server) agentDir(name string) string {
	return filepath.Join(s.agentsRoot(), name)
}

func (s *Server) userProfilePath() string {
	return filepath.Join(s.dataRoot, "USER.md")
}

func (s *Server) memoryPath() string {
	if path := strings.TrimSpace(s.memoryConfig.StoragePath); path != "" {
		return path
	}
	return filepath.Join(s.dataRoot, "memory.json")
}

func (s *Server) loadGatewayState() error {
	path := s.gatewayStatePath()
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
	var state gatewayPersistedState
	_ = json.Unmarshal(data, &state)
	if raw == nil {
		raw = map[string]any{}
	}
	if modelsRaw := mapFromAny(raw["models"]); modelsRaw != nil {
		models := make(map[string]gatewayModel, len(modelsRaw))
		for name, value := range modelsRaw {
			model := gatewayModelFromMap(mapFromAny(value))
			if strings.TrimSpace(model.Name) == "" {
				model.Name = name
			}
			if strings.TrimSpace(model.ID) == "" {
				model.ID = model.Name
			}
			models[name] = model
		}
		if len(models) > 0 {
			state.Models = models
		}
	} else if items, ok := raw["models"].([]any); ok {
		models := make(map[string]gatewayModel, len(items))
		for _, item := range items {
			model, ok := normalizeGatewayModel(gatewayModelFromMap(mapFromAny(item)))
			if !ok {
				continue
			}
			models[model.Name] = model
		}
		if len(models) > 0 {
			state.Models = models
		}
	}
	if skillsRaw := mapFromAny(raw["skills"]); skillsRaw != nil {
		skills := make(map[string]gatewaySkill, len(skillsRaw))
		for name, value := range skillsRaw {
			if enabled, ok := value.(bool); ok {
				skills[name] = gatewaySkill{Name: name, Enabled: enabled}
				continue
			}
			skillMap := mapFromAny(value)
			if skillMap == nil {
				continue
			}
			skills[name] = gatewaySkill{
				Name:        firstNonEmpty(stringFromAny(skillMap["name"]), name),
				Description: stringFromAny(skillMap["description"]),
				Category:    stringFromAny(skillMap["category"]),
				License:     stringFromAny(skillMap["license"]),
				Enabled:     boolValue(skillMap["enabled"]),
			}
		}
		if len(skills) > 0 {
			state.Skills = skills
		}
	} else if items, ok := raw["skills"].([]any); ok {
		skills := make(map[string]gatewaySkill, len(items))
		for _, item := range items {
			skillMap := mapFromAny(item)
			if skillMap == nil {
				continue
			}
			name := strings.TrimSpace(stringFromAny(skillMap["name"]))
			if name == "" {
				continue
			}
			skills[name] = gatewaySkill{
				Name:        name,
				Description: stringFromAny(skillMap["description"]),
				Category:    stringFromAny(skillMap["category"]),
				License:     stringFromAny(skillMap["license"]),
				Enabled:     boolValue(skillMap["enabled"]),
			}
		}
		if len(skills) > 0 {
			state.Skills = skills
		}
	}
	if mcpRaw := mapFromAny(firstNonNil(raw["mcp_config"], raw["mcpConfig"])); mcpRaw != nil {
		state.MCPConfig = gatewayMCPConfigFromMap(mcpRaw)
	}
	if agentsRaw := mapFromAny(raw["agents"]); agentsRaw != nil {
		agents := make(map[string]gatewayAgent, len(agentsRaw))
		for name, value := range agentsRaw {
			agentMap := mapFromAny(value)
			if agentMap == nil {
				continue
			}
			agent := gatewayAgent{
				Name:        firstNonEmpty(stringFromAny(agentMap["name"]), name),
				Description: stringFromAny(agentMap["description"]),
				Soul:        stringFromAny(agentMap["soul"]),
			}
			if rawModel := firstNonNil(agentMap["model"], agentMap["model_name"], agentMap["modelName"]); rawModel != nil {
				if rawModel == nil {
					agent.Model = nil
				} else {
					model := stringFromAny(rawModel)
					agent.Model = &model
				}
			}
			if rawToolGroups, exists := agentMap["tool_groups"]; exists {
				agent.ToolGroups = stringsFromAny(rawToolGroups)
			} else if rawToolGroups, exists := agentMap["toolGroups"]; exists {
				agent.ToolGroups = stringsFromAny(rawToolGroups)
			}
			agents[name] = agent
		}
		if len(agents) > 0 {
			state.Agents = agents
		}
	} else if items, ok := raw["agents"].([]any); ok {
		agents := make(map[string]gatewayAgent, len(items))
		for _, item := range items {
			agentMap := mapFromAny(item)
			if agentMap == nil {
				continue
			}
			name := strings.TrimSpace(stringFromAny(agentMap["name"]))
			if name == "" {
				continue
			}
			agent := gatewayAgent{
				Name:        name,
				Description: stringFromAny(agentMap["description"]),
				Soul:        stringFromAny(agentMap["soul"]),
			}
			if rawModel := firstNonNil(agentMap["model"], agentMap["model_name"], agentMap["modelName"]); rawModel != nil {
				if rawModel == nil {
					agent.Model = nil
				} else {
					model := stringFromAny(rawModel)
					agent.Model = &model
				}
			}
			if rawToolGroups, exists := agentMap["tool_groups"]; exists {
				agent.ToolGroups = stringsFromAny(rawToolGroups)
			} else if rawToolGroups, exists := agentMap["toolGroups"]; exists {
				agent.ToolGroups = stringsFromAny(rawToolGroups)
			}
			agents[name] = agent
		}
		if len(agents) > 0 {
			state.Agents = agents
		}
	}
	if state.UserProfile == "" {
		state.UserProfile = firstNonEmpty(stringFromAny(raw["user_profile"]), stringFromAny(raw["userProfile"]))
	}
	if memRaw := mapFromAny(raw["memory"]); memRaw != nil {
		compatMem := gatewayMemoryResponseFromMap(memRaw)
		if state.Memory.Version == "" {
			state.Memory = compatMem
		} else {
			if state.Memory.LastUpdated == "" {
				state.Memory.LastUpdated = compatMem.LastUpdated
			}
			if state.Memory.User == (memoryUser{}) {
				state.Memory.User = compatMem.User
			}
			if state.Memory.History == (memoryHistory{}) {
				state.Memory.History = compatMem.History
			}
			if len(state.Memory.Facts) == 0 && len(compatMem.Facts) > 0 {
				state.Memory.Facts = compatMem.Facts
			}
		}
	}
	s.uiStateMu.Lock()
	defer s.uiStateMu.Unlock()
	if state.Models != nil {
		s.setModelsLocked(state.Models)
	}
	s.skills = s.discoverGatewaySkills(gatewaySkillEnabledState(state.Skills))
	if state.MCPConfig.MCPServers != nil {
		for name, server := range state.MCPConfig.MCPServers {
			state.MCPConfig.MCPServers[name] = normalizeGatewayMCPServer(server)
		}
		s.mcpConfig = state.MCPConfig
	}
	if state.Agents != nil {
		s.setAgentsLocked(state.Agents)
	}
	s.setUserProfileLocked(state.UserProfile)
	if state.Memory.Version != "" {
		s.setMemoryLocked(state.Memory)
	}
	if agents := s.loadAgentsFromFiles(); len(agents) > 0 {
		s.setAgentsLocked(agents)
	}
	if content, ok := s.loadUserProfileFromFile(); ok {
		s.setUserProfileLocked(content)
	}
	if mem, ok := s.loadMemoryFromFile(); ok {
		s.setMemoryLocked(mem)
	}
	return nil
}

func (s *Server) persistGatewayState() error {
	s.uiStateMu.RLock()
	state := gatewayPersistedState{
		Models:      s.getModelsLocked(),
		Skills:      s.getSkillsLocked(),
		MCPConfig:   s.mcpConfig,
		Agents:      s.getAgentsLocked(),
		UserProfile: s.getUserProfileLocked(),
		Memory:      s.getMemoryLocked(),
	}
	s.uiStateMu.RUnlock()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.gatewayStatePath(), data, 0o644)
}

func defaultGatewayModels(defaultModel string) map[string]gatewayModel {
	configured := gatewayModelsFromEnv(os.Getenv("DEERFLOW_MODELS"))
	if len(configured) == 0 {
		name := strings.TrimSpace(defaultModel)
		if name == "" {
			name = "qwen/Qwen3.5-9B"
		}
		return map[string]gatewayModel{
			name: newGatewayDefaultModel(name),
		}
	}
	models := make(map[string]gatewayModel, len(configured))
	for _, model := range configured {
		models[model.Name] = model
	}
	if name := strings.TrimSpace(defaultModel); name != "" {
		if _, ok := models[name]; !ok {
			models[name] = newGatewayDefaultModel(name)
		}
	}
	return models
}

func defaultGatewayMCPConfig() gatewayMCPConfig {
	if configured := gatewayMCPConfigFromEnv(os.Getenv("DEERFLOW_MCP_CONFIG")); configured.MCPServers != nil {
		return configured
	}
	return gatewayMCPConfig{
		MCPServers: map[string]gatewayMCPServerConfig{
			"filesystem": {
				Enabled:     false,
				Type:        "stdio",
				Command:     "npx",
				Args:        []string{"-y", "@modelcontextprotocol/server-filesystem"},
				Description: "File system access",
			},
		},
	}
}

func defaultGatewayMemory() gatewayMemoryResponse {
	now := time.Now().UTC().Format(time.RFC3339)
	empty := memorySection{Summary: "", UpdatedAt: ""}
	return gatewayMemoryResponse{
		Version:     "1.0",
		LastUpdated: now,
		User: memoryUser{
			WorkContext:     empty,
			PersonalContext: empty,
			TopOfMind:       empty,
		},
		History: memoryHistory{
			RecentMonths:       empty,
			EarlierContext:     empty,
			LongTermBackground: empty,
		},
		Facts: []memoryFact{},
	}
}

func defaultGatewayMemoryConfig(dataRoot string) gatewayMemoryConfig {
	config := gatewayMemoryConfig{
		Enabled:                 true,
		StoragePath:             filepath.Join(dataRoot, "memory.json"),
		DebounceSeconds:         30,
		MaxFacts:                100,
		FactConfidenceThreshold: 0.7,
		InjectionEnabled:        true,
		MaxInjectionTokens:      2000,
	}
	if raw := strings.TrimSpace(os.Getenv("DEERFLOW_MEMORY_CONFIG")); raw != "" {
		var override gatewayMemoryConfig
		var overrideRaw map[string]any
		if err := json.Unmarshal([]byte(raw), &override); err == nil {
			_ = json.Unmarshal([]byte(raw), &overrideRaw)
			if override.StoragePath == "" {
				if value := stringFromAny(overrideRaw["storagePath"]); value != "" {
					override.StoragePath = value
				}
			}
			if _, ok := overrideRaw["debounceSeconds"]; ok {
				override.DebounceSeconds = intValue(overrideRaw["debounceSeconds"])
			}
			if _, ok := overrideRaw["maxFacts"]; ok {
				override.MaxFacts = intValue(overrideRaw["maxFacts"])
			}
			if _, ok := overrideRaw["factConfidenceThreshold"]; ok {
				if value := floatPointerFromAny(overrideRaw["factConfidenceThreshold"]); value != nil {
					override.FactConfidenceThreshold = *value
				}
			}
			if _, ok := overrideRaw["maxInjectionTokens"]; ok {
				override.MaxInjectionTokens = intValue(overrideRaw["maxInjectionTokens"])
			}
			if override.StoragePath == "" {
				override.StoragePath = config.StoragePath
			}
			if _, ok := overrideRaw["debounce_seconds"]; !ok {
				if _, ok := overrideRaw["debounceSeconds"]; !ok && override.DebounceSeconds == 0 {
					override.DebounceSeconds = config.DebounceSeconds
				}
			}
			if _, ok := overrideRaw["max_facts"]; !ok {
				if _, ok := overrideRaw["maxFacts"]; !ok && override.MaxFacts == 0 {
					override.MaxFacts = config.MaxFacts
				}
			}
			if _, ok := overrideRaw["fact_confidence_threshold"]; !ok {
				if _, ok := overrideRaw["factConfidenceThreshold"]; !ok && override.FactConfidenceThreshold == 0 {
					override.FactConfidenceThreshold = config.FactConfidenceThreshold
				}
			}
			if _, ok := overrideRaw["max_injection_tokens"]; !ok {
				if _, ok := overrideRaw["maxInjectionTokens"]; !ok && override.MaxInjectionTokens == 0 {
					override.MaxInjectionTokens = config.MaxInjectionTokens
				}
			}
			if _, ok := overrideRaw["injectionEnabled"]; ok {
				override.InjectionEnabled = boolValue(overrideRaw["injectionEnabled"])
			}
			if _, ok := overrideRaw["enabled"]; ok {
				override.Enabled = boolValue(overrideRaw["enabled"])
			}
			config = override
		}
	}
	if path := strings.TrimSpace(os.Getenv("DEERFLOW_MEMORY_PATH")); path != "" {
		config.StoragePath = path
	}
	return config
}

func defaultGatewayChannelsStatus() gatewayChannelsStatus {
	status := gatewayChannelsStatus{
		ServiceRunning: false,
		Channels:       map[string]map[string]any{},
	}
	if raw := strings.TrimSpace(os.Getenv("DEERFLOW_CHANNELS_CONFIG")); raw != "" {
		var rawMap map[string]any
		if err := json.Unmarshal([]byte(raw), &status); err == nil {
			_ = json.Unmarshal([]byte(raw), &rawMap)
			if _, ok := rawMap["serviceRunning"]; ok {
				status.ServiceRunning = boolValue(rawMap["serviceRunning"])
			}
			if status.Channels == nil {
				status.Channels = map[string]map[string]any{}
			}
			return status
		}
	}
	return status
}

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
		configPath := filepath.Join(s.agentDir(name), "config.json")
		data, err := os.ReadFile(configPath)
		agent := gatewayAgent{Name: name}
		if err == nil {
			if err := json.Unmarshal(data, &agent); err == nil {
				var raw map[string]any
				_ = json.Unmarshal(data, &raw)
				if agent.Name == "" {
					agent.Name = firstNonEmpty(stringFromAny(raw["name"]), name)
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
			}
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

func (s *Server) loadMemoryFromFile() (gatewayMemoryResponse, bool) {
	data, err := os.ReadFile(s.memoryPath())
	if err != nil {
		return gatewayMemoryResponse{}, false
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil {
		if nested, ok := wrapper["memory"]; ok && len(nested) > 0 {
			data = nested
		}
	}
	var mem gatewayMemoryResponse
	if err := json.Unmarshal(data, &mem); err != nil {
		return gatewayMemoryResponse{}, false
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return gatewayMemoryResponse{}, false
	}
	compat := gatewayMemoryResponseFromMap(raw)
	if mem.Version == "" {
		mem.Version = compat.Version
	}
	if mem.LastUpdated == "" {
		mem.LastUpdated = compat.LastUpdated
	}
	if mem.User == (memoryUser{}) {
		mem.User = compat.User
	}
	if mem.History == (memoryHistory{}) {
		mem.History = compat.History
	}
	if len(mem.Facts) == 0 && len(compat.Facts) > 0 {
		mem.Facts = compat.Facts
	} else if len(mem.Facts) == len(compat.Facts) {
		for i := range mem.Facts {
			if mem.Facts[i].ID == "" {
				mem.Facts[i].ID = compat.Facts[i].ID
			}
			if mem.Facts[i].Content == "" {
				mem.Facts[i].Content = compat.Facts[i].Content
			}
			if mem.Facts[i].Category == "" {
				mem.Facts[i].Category = compat.Facts[i].Category
			}
			if mem.Facts[i].Confidence == 0 {
				mem.Facts[i].Confidence = compat.Facts[i].Confidence
			}
			if mem.Facts[i].CreatedAt == "" {
				mem.Facts[i].CreatedAt = compat.Facts[i].CreatedAt
			}
			if mem.Facts[i].Source == "" {
				mem.Facts[i].Source = compat.Facts[i].Source
			}
		}
	}
	if mem.Version == "" {
		return gatewayMemoryResponse{}, false
	}
	return mem, true
}

func gatewayMemoryResponseFromMap(raw map[string]any) gatewayMemoryResponse {
	if raw == nil {
		return gatewayMemoryResponse{}
	}
	userRaw := mapFromAny(raw["user"])
	historyRaw := mapFromAny(raw["history"])
	mem := gatewayMemoryResponse{
		Version:     firstNonEmpty(stringFromAny(raw["version"])),
		LastUpdated: firstNonEmpty(stringFromAny(raw["lastUpdated"]), stringFromAny(raw["last_updated"])),
		User: memoryUser{
			WorkContext: memorySectionFromMap(mapFromAny(firstNonNil(
				raw["workContext"],
				raw["work_context"],
				userRaw["workContext"],
				userRaw["work_context"],
			))),
			PersonalContext: memorySectionFromMap(mapFromAny(firstNonNil(
				raw["personalContext"],
				raw["personal_context"],
				userRaw["personalContext"],
				userRaw["personal_context"],
			))),
			TopOfMind: memorySectionFromMap(mapFromAny(firstNonNil(
				raw["topOfMind"],
				raw["top_of_mind"],
				userRaw["topOfMind"],
				userRaw["top_of_mind"],
			))),
		},
		History: memoryHistory{
			RecentMonths: memorySectionFromMap(mapFromAny(firstNonNil(
				raw["recentMonths"],
				raw["recent_months"],
				historyRaw["recentMonths"],
				historyRaw["recent_months"],
			))),
			EarlierContext: memorySectionFromMap(mapFromAny(firstNonNil(
				raw["earlierContext"],
				raw["earlier_context"],
				historyRaw["earlierContext"],
				historyRaw["earlier_context"],
			))),
			LongTermBackground: memorySectionFromMap(mapFromAny(firstNonNil(
				raw["longTermBackground"],
				raw["long_term_background"],
				historyRaw["longTermBackground"],
				historyRaw["long_term_background"],
			))),
		},
		Facts: memoryFactsFromAny(raw["facts"]),
	}
	return mem
}

func memorySectionFromMap(raw map[string]any) memorySection {
	if raw == nil {
		return memorySection{}
	}
	return memorySection{
		Summary:   firstNonEmpty(stringFromAny(raw["summary"])),
		UpdatedAt: firstNonEmpty(stringFromAny(raw["updatedAt"]), stringFromAny(raw["updated_at"])),
	}
}

func memoryFactsFromAny(raw any) []memoryFact {
	items, _ := raw.([]any)
	facts := make([]memoryFact, 0, len(items))
	for _, item := range items {
		factMap := mapFromAny(item)
		if factMap == nil {
			continue
		}
		facts = append(facts, memoryFact{
			ID:       firstNonEmpty(stringFromAny(factMap["id"])),
			Content:  firstNonEmpty(stringFromAny(factMap["content"])),
			Category: firstNonEmpty(stringFromAny(factMap["category"])),
			Confidence: func() float64 {
				if v := floatPointerFromAny(factMap["confidence"]); v != nil {
					return *v
				}
				return 0
			}(),
			CreatedAt: firstNonEmpty(stringFromAny(factMap["createdAt"]), stringFromAny(factMap["created_at"])),
			Source:    firstNonEmpty(stringFromAny(factMap["source"])),
		})
	}
	return facts
}

func mapFromAny(value any) map[string]any {
	if value == nil {
		return nil
	}
	out, _ := value.(map[string]any)
	return out
}

func (s *Server) persistMemoryFile() error {
	s.uiStateMu.RLock()
	mem := s.getMemoryLocked()
	s.uiStateMu.RUnlock()
	data, err := json.MarshalIndent(mem, "", "  ")
	if err != nil {
		return err
	}
	path := s.memoryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func normalizeAgentName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if !agentNameRE.MatchString(name) {
		return "", false
	}
	return strings.ToLower(name), true
}

func gatewaySkillEnabledState(skills map[string]gatewaySkill) map[string]bool {
	if len(skills) == 0 {
		return nil
	}
	enabled := make(map[string]bool, len(skills))
	for name, skill := range skills {
		enabled[name] = skill.Enabled
	}
	return enabled
}

func gatewaySkillCategoryFromPath(path string) string {
	switch filepath.Base(filepath.Dir(path)) {
	case "public":
		return "public"
	case "custom":
		return "custom"
	default:
		return "custom"
	}
}

func readGatewaySkill(skillDir string, enabledOverride map[string]bool) (gatewaySkill, bool) {
	content, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return gatewaySkill{}, false
	}
	metadata := parseSkillFrontmatter(string(content))
	name := metadata["name"]
	if name == "" {
		name = sanitizeSkillName(filepath.Base(skillDir))
	}
	if name == "" {
		return gatewaySkill{}, false
	}
	license := firstNonEmpty(metadata["license"], "Unknown")
	if license == "Unknown" {
		if data, err := os.ReadFile(filepath.Join(skillDir, "LICENSE.txt")); err == nil {
			if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
				license = compactSubject(trimmed)
			}
		}
	}
	enabled := true
	if enabledOverride != nil {
		if value, ok := enabledOverride[name]; ok {
			enabled = value
		}
	}
	return gatewaySkill{
		Name:        name,
		Description: firstNonEmpty(metadata["description"], "Skill available to deerflow-go"),
		Category:    firstNonEmpty(metadata["category"], gatewaySkillCategoryFromPath(skillDir)),
		License:     license,
		Enabled:     enabled,
	}, true
}

func (s *Server) gatewaySkillRoots() []string {
	roots := []string{
		filepath.Join(s.dataRoot, "skills", "public"),
		filepath.Join(s.dataRoot, "skills", "custom"),
		filepath.Join(s.dataRoot, "skills"),
		filepath.Join("third_party", "deerflow-ui", "skills", "public"),
		filepath.Join("skills", "public"),
		filepath.Join("skills", "custom"),
	}
	if raw := strings.TrimSpace(os.Getenv("DEERFLOW_SKILLS_DIRS")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			roots = append([]string{part}, roots...)
		}
	}
	seen := map[string]struct{}{}
	deduped := make([]string, 0, len(roots))
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		deduped = append(deduped, abs)
	}
	return deduped
}

func (s *Server) discoverGatewaySkills(enabledOverride map[string]bool) map[string]gatewaySkill {
	skills := map[string]gatewaySkill{}
	for _, root := range s.gatewaySkillRoots() {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skill, ok := readGatewaySkill(filepath.Join(root, entry.Name()), enabledOverride)
			if !ok {
				continue
			}
			if existing, ok := skills[skill.Name]; ok {
				if existing.Category == "public" && skill.Category == "custom" {
					skills[skill.Name] = skill
				}
				continue
			}
			skills[skill.Name] = skill
		}
	}
	if len(skills) == 0 {
		skills["deep-research"] = gatewaySkill{
			Name:        "deep-research",
			Description: "Research and summarize a topic with structured outputs.",
			Category:    "public",
			License:     "MIT",
			Enabled:     true,
		}
	}
	return skills
}

func nullableString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func newGatewayDefaultModel(name string) gatewayModel {
	name = strings.TrimSpace(name)
	return gatewayModel{
		ID:                      name,
		Name:                    name,
		Model:                   name,
		DisplayName:             name,
		Description:             "Configured model available to deerflow-go",
		SupportsThinking:        true,
		SupportsReasoningEffort: true,
	}
}

func gatewayModelsFromEnv(raw string) []gatewayModel {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var fromJSON []map[string]any
		if err := json.Unmarshal([]byte(raw), &fromJSON); err == nil {
			models := make([]gatewayModel, 0, len(fromJSON))
			for _, item := range fromJSON {
				models = append(models, gatewayModelFromMap(item))
			}
			return normalizeGatewayModels(models)
		}
	}
	parts := strings.Split(raw, ",")
	models := make([]gatewayModel, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		models = append(models, newGatewayDefaultModel(name))
	}
	return normalizeGatewayModels(models)
}

func gatewayModelFromMap(raw map[string]any) gatewayModel {
	if raw == nil {
		return gatewayModel{}
	}
	return gatewayModel{
		ID:                      firstNonEmpty(stringFromAny(raw["id"])),
		Name:                    firstNonEmpty(stringFromAny(raw["name"])),
		Model:                   firstNonEmpty(stringFromAny(raw["model"]), stringFromAny(raw["model_name"]), stringFromAny(raw["modelName"])),
		DisplayName:             firstNonEmpty(stringFromAny(raw["display_name"]), stringFromAny(raw["displayName"])),
		Description:             firstNonEmpty(stringFromAny(raw["description"])),
		SupportsThinking:        boolValue(firstNonNil(raw["supports_thinking"], raw["supportsThinking"])),
		SupportsReasoningEffort: boolValue(firstNonNil(raw["supports_reasoning_effort"], raw["supportsReasoningEffort"])),
	}
}

func gatewayMCPConfigFromEnv(raw string) gatewayMCPConfig {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return gatewayMCPConfig{}
	}
	var configRaw map[string]any
	if err := json.Unmarshal([]byte(raw), &configRaw); err != nil {
		return gatewayMCPConfig{}
	}
	return gatewayMCPConfigFromMap(configRaw)
}

func normalizeGatewayMCPServer(server gatewayMCPServerConfig) gatewayMCPServerConfig {
	if strings.TrimSpace(server.Type) == "" {
		server.Type = "stdio"
	}
	if server.Env == nil {
		server.Env = map[string]string{}
	}
	if server.Headers == nil {
		server.Headers = map[string]string{}
	}
	if server.Args == nil {
		server.Args = []string{}
	}
	if server.OAuth != nil {
		if strings.TrimSpace(server.OAuth.GrantType) == "" {
			server.OAuth.GrantType = "client_credentials"
		}
		if strings.TrimSpace(server.OAuth.TokenField) == "" {
			server.OAuth.TokenField = "access_token"
		}
		if strings.TrimSpace(server.OAuth.TokenTypeField) == "" {
			server.OAuth.TokenTypeField = "token_type"
		}
		if strings.TrimSpace(server.OAuth.ExpiresInField) == "" {
			server.OAuth.ExpiresInField = "expires_in"
		}
		if strings.TrimSpace(server.OAuth.DefaultTokenType) == "" {
			server.OAuth.DefaultTokenType = "Bearer"
		}
		if server.OAuth.ExtraTokenParams == nil {
			server.OAuth.ExtraTokenParams = map[string]string{}
		}
	}
	return server
}

func gatewayMCPConfigFromMap(raw map[string]any) gatewayMCPConfig {
	req := gatewayMCPConfig{MCPServers: map[string]gatewayMCPServerConfig{}}
	serversRaw := firstNonNil(raw["mcp_servers"], raw["mcpServers"])
	serversMap, _ := serversRaw.(map[string]any)
	for name, value := range serversMap {
		serverMap, _ := value.(map[string]any)
		req.MCPServers[name] = normalizeGatewayMCPServer(gatewayMCPServerFromMap(serverMap))
	}
	return req
}

func gatewayMCPServerFromMap(raw map[string]any) gatewayMCPServerConfig {
	if raw == nil {
		return gatewayMCPServerConfig{}
	}
	server := gatewayMCPServerConfig{
		Enabled:     boolValue(raw["enabled"]),
		Type:        firstNonEmpty(stringFromAny(raw["type"]), "stdio"),
		Command:     stringFromAny(raw["command"]),
		Args:        stringSliceFromAny(raw["args"]),
		Env:         stringMapFromAny(raw["env"]),
		URL:         stringFromAny(raw["url"]),
		Headers:     stringMapFromAny(raw["headers"]),
		Description: stringFromAny(raw["description"]),
	}
	if oauthRaw := firstNonNil(raw["oauth"], raw["oAuth"]); oauthRaw != nil {
		if oauthMap, _ := oauthRaw.(map[string]any); oauthMap != nil {
			server.OAuth = gatewayMCPOAuthFromMap(oauthMap)
		}
	}
	return server
}

func gatewayMCPOAuthFromMap(raw map[string]any) *gatewayMCPOAuthConfig {
	if raw == nil {
		return nil
	}
	oauth := &gatewayMCPOAuthConfig{
		Enabled:           boolValue(raw["enabled"]),
		TokenURL:          firstNonEmpty(stringFromAny(raw["token_url"]), stringFromAny(raw["tokenUrl"])),
		GrantType:         firstNonEmpty(stringFromAny(raw["grant_type"]), stringFromAny(raw["grantType"])),
		ClientID:          firstNonEmpty(stringFromAny(raw["client_id"]), stringFromAny(raw["clientId"])),
		ClientSecret:      firstNonEmpty(stringFromAny(raw["client_secret"]), stringFromAny(raw["clientSecret"])),
		RefreshToken:      firstNonEmpty(stringFromAny(raw["refresh_token"]), stringFromAny(raw["refreshToken"])),
		Scope:             stringFromAny(raw["scope"]),
		Audience:          stringFromAny(raw["audience"]),
		TokenField:        firstNonEmpty(stringFromAny(raw["token_field"]), stringFromAny(raw["tokenField"])),
		TokenTypeField:    firstNonEmpty(stringFromAny(raw["token_type_field"]), stringFromAny(raw["tokenTypeField"])),
		ExpiresInField:    firstNonEmpty(stringFromAny(raw["expires_in_field"]), stringFromAny(raw["expiresInField"])),
		DefaultTokenType:  firstNonEmpty(stringFromAny(raw["default_token_type"]), stringFromAny(raw["defaultTokenType"])),
		RefreshSkewSecond: intValue(firstNonNil(raw["refresh_skew_seconds"], raw["refreshSkewSeconds"])),
		ExtraTokenParams:  stringMapFromAny(firstNonNil(raw["extra_token_params"], raw["extraTokenParams"])),
	}
	if _, ok := raw["refresh_skew_seconds"]; !ok {
		if _, ok := raw["refreshSkewSeconds"]; !ok && oauth.RefreshSkewSecond == 0 {
			oauth.RefreshSkewSecond = 60
		}
	}
	return oauth
}

func stringSliceFromAny(value any) []string {
	items, _ := value.([]any)
	if len(items) == 0 {
		if direct, ok := value.([]string); ok {
			return append([]string(nil), direct...)
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := stringFromAny(item); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func stringMapFromAny(value any) map[string]string {
	switch typed := value.(type) {
	case map[string]string:
		return mapsClone(typed)
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, raw := range typed {
			out[key] = stringFromAny(raw)
		}
		return out
	default:
		return nil
	}
}

func mapsClone(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	default:
		return 0
	}
}

func firstNonEmptySlice[T any](primary []T, fallback []T) []T {
	if len(primary) > 0 {
		return primary
	}
	if len(fallback) > 0 {
		return fallback
	}
	return primary
}

func stringsFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(stringFromAny(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func normalizeGatewayModels(models []gatewayModel) []gatewayModel {
	normalized := make([]gatewayModel, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		next, ok := normalizeGatewayModel(model)
		if !ok {
			continue
		}
		if _, ok := seen[next.Name]; ok {
			continue
		}
		seen[next.Name] = struct{}{}
		normalized = append(normalized, next)
	}
	return normalized
}

func normalizeGatewayModel(model gatewayModel) (gatewayModel, bool) {
	name := strings.TrimSpace(model.Name)
	if name == "" {
		name = strings.TrimSpace(model.Model)
	}
	if name == "" {
		name = strings.TrimSpace(model.ID)
	}
	if name == "" {
		return gatewayModel{}, false
	}
	model.Name = name
	if strings.TrimSpace(model.ID) == "" {
		model.ID = name
	}
	if strings.TrimSpace(model.Model) == "" {
		model.Model = name
	}
	if strings.TrimSpace(model.DisplayName) == "" {
		model.DisplayName = name
	}
	return model, true
}

func (s *Server) ensureDefaultModelLocked() {
	if s.models == nil {
		s.models = map[string]gatewayModel{}
	}
	name := strings.TrimSpace(s.defaultModel)
	if name == "" {
		name = "qwen/Qwen3.5-9B"
	}
	if _, ok := s.models[name]; ok {
		return
	}
	s.models[name] = newGatewayDefaultModel(name)
}

func (s *Server) findModelLocked(name string) (gatewayModel, bool) {
	models := s.getModelsLocked()
	if model, ok := models[name]; ok {
		return model, true
	}
	for _, model := range models {
		if model.Name == name || model.Model == name || model.ID == name {
			return model, true
		}
	}
	return gatewayModel{}, false
}

func (s *Server) getModelsLocked() map[string]gatewayModel {
	if s.models == nil {
		s.models = map[string]gatewayModel{}
	}
	s.ensureDefaultModelLocked()
	return s.models
}

func (s *Server) getSkillsLocked() map[string]gatewaySkill {
	if s.skills == nil {
		s.skills = s.discoverGatewaySkills(nil)
	}
	return s.skills
}

func (s *Server) setModelsLocked(models map[string]gatewayModel) {
	normalized := make(map[string]gatewayModel, len(models))
	for _, model := range models {
		next, ok := normalizeGatewayModel(model)
		if !ok {
			continue
		}
		normalized[next.Name] = next
	}
	s.models = normalized
	s.ensureDefaultModelLocked()
}

func (s *Server) getAgentsLocked() map[string]gatewayAgent {
	if s.agents == nil {
		s.agents = map[string]gatewayAgent{}
	}
	return s.agents
}

func (s *Server) setAgentsLocked(agents map[string]gatewayAgent) {
	s.agents = agents
}

func (s *Server) getUserProfileLocked() string {
	return s.userProfile
}

func (s *Server) setUserProfileLocked(content string) {
	s.userProfile = content
}

func (s *Server) getMemoryLocked() gatewayMemoryResponse {
	if s.memory.Version == "" {
		return defaultGatewayMemory()
	}
	return s.memory
}

func (s *Server) setMemoryLocked(memory gatewayMemoryResponse) {
	s.memory = memory
}
