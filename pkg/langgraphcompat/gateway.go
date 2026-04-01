package langgraphcompat

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"gopkg.in/yaml.v3"
)

type gatewayModel struct {
	ID                      string `json:"id"`
	Name                    string `json:"name"`
	Model                   string `json:"model"`
	DisplayName             string `json:"display_name"`
	Description             string `json:"description,omitempty"`
	SupportsThinking        bool   `json:"supports_thinking,omitempty"`
	SupportsReasoningEffort bool   `json:"supports_reasoning_effort,omitempty"`
	SupportsVision          bool   `json:"supports_vision,omitempty"`
}

type gatewaySkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	License     string `json:"license"`
	Enabled     bool   `json:"enabled"`
}

type gatewayMCPServerConfig struct {
	Type        string                 `json:"type,omitempty"`
	Enabled     bool                   `json:"enabled"`
	Command     string                 `json:"command,omitempty"`
	Args        []string               `json:"args,omitempty"`
	Env         map[string]string      `json:"env,omitempty"`
	URL         string                 `json:"url,omitempty"`
	Headers     map[string]string      `json:"headers,omitempty"`
	OAuth       *gatewayMCPOAuthConfig `json:"oauth,omitempty"`
	Description string                 `json:"description"`
}

type gatewayMCPConfig struct {
	MCPServers map[string]gatewayMCPServerConfig `json:"mcp_servers"`
}

type gatewayMCPOAuthConfig struct {
	Enabled            bool              `json:"enabled"`
	TokenURL           string            `json:"token_url,omitempty"`
	GrantType          string            `json:"grant_type,omitempty"`
	ClientID           string            `json:"client_id,omitempty"`
	ClientSecret       string            `json:"client_secret,omitempty"`
	RefreshToken       string            `json:"refresh_token,omitempty"`
	Scope              string            `json:"scope,omitempty"`
	Audience           string            `json:"audience,omitempty"`
	TokenField         string            `json:"token_field,omitempty"`
	TokenTypeField     string            `json:"token_type_field,omitempty"`
	ExpiresInField     string            `json:"expires_in_field,omitempty"`
	DefaultTokenType   string            `json:"default_token_type,omitempty"`
	RefreshSkewSeconds int               `json:"refresh_skew_seconds,omitempty"`
	ExtraTokenParams   map[string]string `json:"extra_token_params,omitempty"`
}

type gatewayPersistedState struct {
	Skills       map[string]gatewaySkill `json:"skills"`
	MCPConfig    gatewayMCPConfig        `json:"mcp_config"`
	Agents       map[string]gatewayAgent `json:"agents,omitempty"`
	UserProfile  string                  `json:"user_profile,omitempty"`
	MemoryThread string                  `json:"memory_thread,omitempty"`
	Memory       gatewayMemoryResponse   `json:"memory"`
}

const maxSkillArchiveSize int64 = 512 << 20

const (
	skillCategoryPublic = "public"
	skillCategoryCustom = "custom"
)

var activeContentMIMETypes = map[string]struct{}{
	"text/html":             {},
	"application/xhtml+xml": {},
	"image/svg+xml":         {},
}

var skillInstallSeq uint64
var agentNameRE = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
var threadIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var skillFrontmatterNameRE = regexp.MustCompile(`^[a-z0-9-]+$`)

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

func (s *Server) registerGatewayRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/models", s.handleModelsList)
	mux.HandleFunc("GET /api/models/{model_name...}", s.handleModelGet)
	mux.HandleFunc("GET /api/skills", s.handleSkillsList)
	mux.HandleFunc("GET /api/skills/{skill_name}", s.handleSkillGet)
	mux.HandleFunc("PUT /api/skills/{skill_name}", s.handleSkillSetEnabled)
	mux.HandleFunc("POST /api/skills/{skill_name}/enable", s.handleSkillEnable)
	mux.HandleFunc("POST /api/skills/{skill_name}/disable", s.handleSkillDisable)
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
	writeJSON(w, http.StatusOK, map[string]any{"models": configuredGatewayModels(s.defaultModel)})
}

func (s *Server) handleModelGet(w http.ResponseWriter, r *http.Request) {
	modelName := strings.TrimSpace(r.PathValue("model_name"))
	model, ok := findConfiguredGatewayModel(s.defaultModel, modelName)
	if modelName == "" || !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": fmt.Sprintf("Model '%s' not found", modelName)})
		return
	}
	writeJSON(w, http.StatusOK, model)
}

func (s *Server) handleSkillsList(w http.ResponseWriter, r *http.Request) {
	currentSkills := s.currentGatewaySkills()
	skills := make([]gatewaySkill, 0, len(currentSkills))
	for _, skill := range currentSkills {
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Category == skills[j].Category {
			return skills[i].Name < skills[j].Name
		}
		return skills[i].Category < skills[j].Category
	})
	writeJSON(w, http.StatusOK, map[string]any{"skills": skills})
}

func (s *Server) handleSkillGet(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("skill_name"))
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	skill, ok := findGatewaySkill(s.currentGatewaySkills(), name, category)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": fmt.Sprintf("Skill '%s' not found", name)})
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
	s.updateSkillEnabled(w, r, req.Enabled)
}

func (s *Server) handleSkillEnable(w http.ResponseWriter, r *http.Request) {
	s.updateSkillEnabled(w, r, true)
}

func (s *Server) handleSkillDisable(w http.ResponseWriter, r *http.Request) {
	s.updateSkillEnabled(w, r, false)
}

func (s *Server) updateSkillEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	name := strings.TrimSpace(r.PathValue("skill_name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "skill_name is required"})
		return
	}
	category := strings.TrimSpace(r.URL.Query().Get("category"))

	currentSkills := s.currentGatewaySkills()
	s.uiStateMu.Lock()
	key, skill, ok := findGatewaySkillEntry(currentSkills, name, category)
	if !ok {
		s.uiStateMu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "skill not found"})
		return
	}
	skill.Enabled = enabled
	s.skills[key] = skill
	s.uiStateMu.Unlock()
	if err := s.persistGatewayState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "skill": skill})
}

func (s *Server) handleSkillInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ThreadID string `json:"thread_id"`
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	archivePath, err := s.resolveSkillArchivePath(req.ThreadID, req.Path)
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
	var req gatewayMCPConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	if req.MCPServers == nil {
		req.MCPServers = map[string]gatewayMCPServerConfig{}
	}

	s.uiStateMu.Lock()
	s.mcpConfig = req
	s.uiStateMu.Unlock()
	s.applyGatewayMCPConfig(r.Context(), req)
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
	var req gatewayAgent
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	name, ok := normalizeAgentName(req.Name)
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
	req.Name = name
	agents[name] = req
	s.uiStateMu.Unlock()
	if err := s.persistAgentFiles(req); err != nil {
		s.uiStateMu.Lock()
		delete(s.getAgentsLocked(), name)
		s.uiStateMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist agent files"})
		return
	}
	if err := s.persistGatewayState(); err != nil {
		s.uiStateMu.Lock()
		delete(s.getAgentsLocked(), name)
		s.uiStateMu.Unlock()
		_ = s.deleteAgentFiles(name)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	writeJSON(w, http.StatusCreated, req)
}

func (s *Server) handleAgentUpdate(w http.ResponseWriter, r *http.Request) {
	name, ok := normalizeAgentName(r.PathValue("name"))
	if !ok {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": "Invalid agent name"})
		return
	}
	var req struct {
		Description *string   `json:"description"`
		Model       **string  `json:"model"`
		ToolGroups  *[]string `json:"tool_groups"`
		Soul        *string   `json:"soul"`
	}
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
	previous := agent
	if req.Description != nil {
		agent.Description = *req.Description
	}
	if req.Model != nil {
		agent.Model = *req.Model
	}
	if req.ToolGroups != nil {
		agent.ToolGroups = *req.ToolGroups
	}
	if req.Soul != nil {
		agent.Soul = *req.Soul
	}
	agents[name] = agent
	s.uiStateMu.Unlock()
	if err := s.persistAgentFiles(agent); err != nil {
		s.uiStateMu.Lock()
		agents := s.getAgentsLocked()
		agents[name] = previous
		s.uiStateMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist agent files"})
		return
	}
	if err := s.persistGatewayState(); err != nil {
		s.uiStateMu.Lock()
		agents := s.getAgentsLocked()
		agents[name] = previous
		s.uiStateMu.Unlock()
		_ = s.persistAgentFiles(previous)
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
	agent, exists := agents[name]
	if !exists {
		s.uiStateMu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": fmt.Sprintf("Agent '%s' not found", name)})
		return
	}
	delete(agents, name)
	s.uiStateMu.Unlock()
	if err := s.deleteAgentFiles(name); err != nil {
		s.uiStateMu.Lock()
		s.getAgentsLocked()[name] = agent
		s.uiStateMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to delete agent files"})
		return
	}
	if err := s.persistGatewayState(); err != nil {
		s.uiStateMu.Lock()
		s.getAgentsLocked()[name] = agent
		s.uiStateMu.Unlock()
		_ = s.persistAgentFiles(agent)
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
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}
	s.uiStateMu.Lock()
	s.setUserProfileLocked(req.Content)
	s.uiStateMu.Unlock()
	if err := os.MkdirAll(s.dataRoot, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist user profile"})
		return
	}
	if err := os.WriteFile(s.userProfilePath(), []byte(req.Content), 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist user profile"})
		return
	}
	if err := s.persistGatewayState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": nullableString(req.Content)})
}

func (s *Server) handleMemoryGet(w http.ResponseWriter, r *http.Request) {
	s.refreshGatewayMemoryCache(r.Context())
	s.uiStateMu.RLock()
	m := s.getMemoryLocked()
	s.uiStateMu.RUnlock()
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleMemoryReload(w http.ResponseWriter, r *http.Request) {
	s.refreshGatewayMemoryCache(r.Context())
	s.handleMemoryGet(w, r)
}

func (s *Server) handleMemoryClear(w http.ResponseWriter, r *http.Request) {
	if err := s.replaceGatewayMemoryDocument(r.Context(), memory.Document{
		SessionID: strings.TrimSpace(s.memoryThread),
		Source:    strings.TrimSpace(s.memoryThread),
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to clear memory"})
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
	doc, err := s.loadGatewayMemoryDocument(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to load memory"})
		return
	}
	newFacts := make([]memory.Fact, 0, len(doc.Facts))
	found := false
	for _, fact := range doc.Facts {
		if fact.ID == factID {
			found = true
			continue
		}
		newFacts = append(newFacts, fact)
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": fmt.Sprintf("Memory fact '%s' not found", factID)})
		return
	}
	doc.Facts = newFacts
	doc.UpdatedAt = time.Now().UTC()
	if err := s.replaceGatewayMemoryDocument(r.Context(), doc); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist memory"})
		return
	}
	if err := s.persistGatewayState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	s.handleMemoryGet(w, r)
}

func (s *Server) handleMemoryConfigGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":                   s.memorySvc != nil,
		"storage_path":              s.gatewayMemoryStoragePath(),
		"debounce_seconds":          30,
		"max_facts":                 100,
		"fact_confidence_threshold": 0.7,
		"injection_enabled":         s.memorySvc != nil,
		"max_injection_tokens":      2000,
	})
}

func (s *Server) handleMemoryStatusGet(w http.ResponseWriter, r *http.Request) {
	s.refreshGatewayMemoryCache(r.Context())
	s.uiStateMu.RLock()
	mem := s.getMemoryLocked()
	s.uiStateMu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"config": map[string]any{
			"enabled":                   s.memorySvc != nil,
			"storage_path":              s.gatewayMemoryStoragePath(),
			"debounce_seconds":          30,
			"max_facts":                 100,
			"fact_confidence_threshold": 0.7,
			"injection_enabled":         s.memorySvc != nil,
			"max_injection_tokens":      2000,
		},
		"data": mem,
	})
}

func (s *Server) handleChannelsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.gatewayChannelStatus())
}

func (s *Server) handleChannelRestart(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	success, message := s.restartGatewayChannel(name)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": success,
		"message": fmt.Sprintf("Channel %s: %s", name, message),
	})
}

func (s *Server) handleGatewayThreadDelete(w http.ResponseWriter, r *http.Request) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	if err := validateThreadID(threadID); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": err.Error()})
		return
	}

	if err := s.deleteThreadResources(threadID, true); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to delete local thread data"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Deleted local thread data for %s", threadID),
	})
}

func (s *Server) handleUploadsCreate(w http.ResponseWriter, r *http.Request) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	if err := validateThreadID(threadID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
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

	seenNames, err := existingUploadNames(uploadDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to inspect upload dir"})
		return
	}

	infos := make([]map[string]any, 0, len(files))
	for _, fh := range files {
		name := sanitizeFilename(fh.Filename)
		if name == "" {
			continue
		}
		name = claimUniqueFilename(name, seenNames)

		info, err := s.saveUploadedFile(threadID, uploadDir, name, fh)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
			return
		}
		if mdPath, err := generateUploadMarkdownCompanion(filepath.Join(uploadDir, name)); err != nil {
			if s.logger != nil {
				s.logger.Printf("upload markdown conversion failed for %s/%s: %v", threadID, name, err)
			}
		} else if mdPath != "" {
			mdName := filepath.Base(mdPath)
			info["markdown_file"] = mdName
			info["markdown_path"] = "/mnt/user-data/uploads/" + mdName
			info["markdown_virtual_path"] = "/mnt/user-data/uploads/" + mdName
			info["markdown_artifact_url"] = uploadArtifactURL(threadID, mdName)
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
	if err := validateThreadID(threadID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
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
		if isGeneratedMarkdownCompanion(uploadDir, name) {
			continue
		}
		fullPath := filepath.Join(uploadDir, name)
		stat, err := entry.Info()
		if err != nil {
			continue
		}
		info := s.uploadInfo(threadID, fullPath, name, stat.Size(), stat.ModTime().Unix())
		s.attachMarkdownCompanionInfo(threadID, uploadDir, name, info)
		files = append(files, info)
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
	filename := sanitizePathFilename(r.PathValue("filename"))
	if err := validateThreadID(threadID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	if filename == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Invalid path"})
		return
	}

	uploadDir := s.uploadsDir(threadID)
	target := filepath.Join(uploadDir, filename)
	if err := ensureResolvedPathWithinBase(uploadDir, target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	if _, err := os.Lstat(target); err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]any{"detail": fmt.Sprintf("File not found: %s", filename)})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to inspect file"})
		return
	}
	if err := os.Remove(target); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to delete file"})
		return
	}
	if isConvertibleUploadExtension(filename) {
		_ = os.Remove(strings.TrimSuffix(target, filepath.Ext(target)) + ".md")
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Deleted %s", filename),
	})
}

func (s *Server) handleArtifactGet(w http.ResponseWriter, r *http.Request) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	artifactPath := strings.TrimSpace(r.PathValue("artifact_path"))
	if err := validateThreadID(threadID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if artifactPath == "" {
		http.NotFound(w, r)
		return
	}

	if strings.HasSuffix(artifactPath, ".skill") && !downloadRequested(r) {
		if s.handleSkillArchiveRootPreview(w, r, threadID, artifactPath) {
			return
		}
	}

	if strings.Contains(artifactPath, ".skill/") {
		s.handleSkillArchiveArtifactGet(w, r, threadID, artifactPath)
		return
	}

	absPath, err := s.resolveArtifactPath(threadID, artifactPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !info.Mode().IsRegular() {
		http.Error(w, "path is not a file", http.StatusBadRequest)
		return
	}

	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(absPath)))
	if shouldForceAttachment(r, mimeType) {
		w.Header().Set("Content-Disposition", contentDisposition("attachment", filepath.Base(absPath)))
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	serveArtifactContent(w, filepath.Base(absPath), mimeType, data, downloadRequested(r))
}

func (s *Server) handleSkillArchiveRootPreview(w http.ResponseWriter, r *http.Request, threadID, artifactPath string) bool {
	archivePath, err := s.resolveArtifactPath(threadID, artifactPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return true
	}
	content, err := extractSkillArchiveFile(archivePath, "SKILL.md")
	if err != nil {
		return false
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	serveArtifactContent(w, "SKILL.md", "text/markdown", content, false)
	return true
}

func (s *Server) handleSkillArchiveArtifactGet(w http.ResponseWriter, r *http.Request, threadID, artifactPath string) {
	skillPath, internalPath, ok := splitSkillArchiveArtifactPath(artifactPath)
	if !ok {
		http.NotFound(w, r)
		return
	}

	archivePath, err := s.resolveArtifactPath(threadID, skillPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	content, err := extractSkillArchiveFile(archivePath, internalPath)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}

	name := filepath.Base(internalPath)
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	w.Header().Set("Cache-Control", "private, max-age=300")
	if shouldForceAttachment(r, mimeType) {
		w.Header().Set("Content-Disposition", contentDisposition("attachment", name))
	}
	serveArtifactContent(w, name, mimeType, content, downloadRequested(r))
}

func (s *Server) handleSuggestions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		N         int    `json:"n"`
		ModelName string `json:"model_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"suggestions": []string{}})
		return
	}
	if req.N <= 0 {
		req.N = 3
	}
	if req.N > 5 {
		req.N = 5
	}

	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"suggestions": []string{}})
		return
	}

	suggestions := s.generateSuggestions(r.Context(), req.Messages, req.N, req.ModelName)
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": suggestions})
}

func (s *Server) saveUploadedFile(threadID, uploadDir, name string, fh *multipart.FileHeader) (map[string]any, error) {
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

func (s *Server) attachMarkdownCompanionInfo(threadID, uploadDir, name string, info map[string]any) {
	if !isConvertibleUploadExtension(name) {
		return
	}

	mdName := strings.TrimSuffix(name, filepath.Ext(name)) + ".md"
	mdPath := filepath.Join(uploadDir, mdName)
	stat, err := os.Stat(mdPath)
	if err != nil || !stat.Mode().IsRegular() {
		return
	}

	info["markdown_file"] = mdName
	info["markdown_path"] = "/mnt/user-data/uploads/" + mdName
	info["markdown_virtual_path"] = "/mnt/user-data/uploads/" + mdName
	if strings.TrimSpace(threadID) != "" {
		info["markdown_artifact_url"] = uploadArtifactURL(threadID, mdName)
	}
}

func (s *Server) uploadInfo(threadID, fullPath, name string, size int64, modified int64) map[string]any {
	virtualPath := "/mnt/user-data/uploads/" + name
	return map[string]any{
		"filename":     name,
		"size":         size,
		"path":         virtualPath,
		"virtual_path": virtualPath,
		"artifact_url": uploadArtifactURL(threadID, name),
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
	return false
}

func splitSkillArchiveArtifactPath(path string) (string, string, bool) {
	const marker = ".skill/"
	idx := strings.Index(path, marker)
	if idx < 0 {
		return "", "", false
	}
	skillPath := path[:idx+len(".skill")]
	internalPath := strings.TrimPrefix(path[idx+len(marker):], "/")
	if skillPath == "" || internalPath == "" {
		return "", "", false
	}
	cleanInternal := filepath.Clean(internalPath)
	if cleanInternal == "." || cleanInternal == ".." || strings.HasPrefix(cleanInternal, ".."+string(filepath.Separator)) {
		return "", "", false
	}
	return skillPath, filepath.ToSlash(cleanInternal), true
}

func extractSkillArchiveFile(archivePath, internalPath string) ([]byte, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var prefixedMatch *zip.File
	for _, file := range reader.File {
		name := filepath.ToSlash(filepath.Clean(file.Name))
		name = strings.TrimPrefix(name, "./")
		if name == internalPath {
			if file.FileInfo().IsDir() {
				return nil, os.ErrNotExist
			}
			rc, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
		if strings.HasSuffix(name, "/"+internalPath) {
			prefixedMatch = file
		}
	}
	if prefixedMatch != nil {
		if prefixedMatch.FileInfo().IsDir() {
			return nil, os.ErrNotExist
		}
		rc, err := prefixedMatch.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, os.ErrNotExist
}

func (s *Server) resolveArtifactPath(threadID, artifactPath string) (string, error) {
	return s.resolveThreadVirtualPath(threadID, artifactPath)
}

func serveArtifactContent(w http.ResponseWriter, filename, mimeType string, data []byte, download bool) {
	if mimeType == "" && looksLikeText(data) {
		mimeType = "text/plain; charset=utf-8"
	}
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}

	if download {
		if w.Header().Get("Content-Disposition") == "" {
			w.Header().Set("Content-Disposition", contentDisposition("attachment", filename))
		}
		if mimeType != "" {
			w.Header().Set("Content-Type", mimeType)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	if isTextMIMEType(mimeType) && utf8.Valid(data) {
		w.Header().Set("Content-Type", withUTF8Charset(mimeType))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	if looksLikeText(data) && utf8.Valid(data) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	w.Header().Set("Content-Disposition", contentDisposition("inline", filename))
	w.Header().Set("Content-Type", mimeType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func downloadRequested(r *http.Request) bool {
	return strings.EqualFold(r.URL.Query().Get("download"), "true")
}

func shouldForceAttachment(r *http.Request, mimeType string) bool {
	if downloadRequested(r) {
		return true
	}
	base := strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])
	_, active := activeContentMIMETypes[base]
	return active
}

func isTextMIMEType(mimeType string) bool {
	base := strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])
	return strings.HasPrefix(base, "text/")
}

func looksLikeText(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	const sampleSize = 8192
	if len(data) > sampleSize {
		data = data[:sampleSize]
	}
	return !bytesContainsNUL(data)
}

func bytesContainsNUL(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

func withUTF8Charset(mimeType string) string {
	if strings.Contains(strings.ToLower(mimeType), "charset=") {
		return mimeType
	}
	return mimeType + "; charset=utf-8"
}

func contentDisposition(kind, filename string) string {
	filename = strings.ReplaceAll(filename, "\r", "")
	filename = strings.ReplaceAll(filename, "\n", "")
	return fmt.Sprintf("%s; filename*=UTF-8''%s", kind, url.PathEscape(filename))
}

func validateThreadID(threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return errors.New("thread_id is required")
	}
	if !threadIDRE.MatchString(threadID) {
		return fmt.Errorf("invalid thread_id %q: only alphanumeric characters, dots, hyphens, and underscores are allowed", threadID)
	}
	return nil
}

func (s *Server) threadRoot(threadID string) string {
	return filepath.Join(s.threadDir(threadID), "user-data")
}

func (s *Server) threadDir(threadID string) string {
	return filepath.Join(s.dataRoot, "threads", threadID)
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

func sanitizePathFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		return ""
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return ""
	}
	return sanitizeFilename(name)
}

func existingUploadNames(uploadDir string) (map[string]struct{}, error) {
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]struct{}{}, nil
		}
		return nil, err
	}

	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		seen[entry.Name()] = struct{}{}
	}
	return seen, nil
}

func claimUniqueFilename(name string, seen map[string]struct{}) string {
	if seen == nil {
		seen = map[string]struct{}{}
	}
	if _, exists := seen[name]; !exists {
		seen[name] = struct{}{}
		return name
	}

	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s_%d%s", stem, i, ext)
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		return candidate
	}
}

func uploadArtifactURL(threadID, filename string) string {
	return "/api/threads/" + threadID + "/artifacts/mnt/user-data/uploads/" + url.PathEscape(filename)
}

func compactSubject(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	runes := []rune(text)
	if len(runes) > 48 {
		return string(runes[:48])
	}
	return text
}

func (s *Server) generateSuggestions(ctx context.Context, messages []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}, n int, modelName string) []string {
	if len(messages) == 0 || n <= 0 {
		return []string{}
	}

	conversation := formatSuggestionConversation(messages)
	if conversation == "" {
		return []string{}
	}

	return finalizeSuggestions(
		s.generateSuggestionsWithLLM(ctx, conversation, n, modelName),
		fallbackSuggestions(messages, n),
		n,
	)
}

func (s *Server) generateSuggestionsWithLLM(ctx context.Context, conversation string, n int, modelName string) []string {
	provider := s.llmProvider
	if provider == nil {
		return nil
	}

	maxTokens := 128
	resp, err := provider.Chat(ctx, llm.ChatRequest{
		Model: resolveTitleModel(modelName, s.defaultModel),
		Messages: []models.Message{{
			ID:        "suggestions-user",
			SessionID: "suggestions",
			Role:      models.RoleHuman,
			Content:   buildSuggestionsPrompt(conversation, n),
		}},
		MaxTokens: &maxTokens,
	})
	if err != nil {
		return nil
	}

	suggestions := parseJSONStringList(resp.Message.Content)
	if len(suggestions) == 0 {
		return nil
	}
	if len(suggestions) > n {
		suggestions = suggestions[:n]
	}
	return suggestions
}

func buildSuggestionsPrompt(conversation string, n int) string {
	return fmt.Sprintf(
		"You are generating follow-up questions to help the user continue the conversation.\n"+
			"Based on the conversation below, produce EXACTLY %d short questions the user might ask next.\n"+
			"Requirements:\n"+
			"- Questions must be relevant to the conversation.\n"+
			"- Questions must be written in the same language as the user.\n"+
			"- Keep each question concise (ideally <= 20 words / <= 40 Chinese characters).\n"+
			"- Do NOT include numbering, markdown, or any extra text.\n"+
			"- Output MUST be a JSON array of strings only.\n\n"+
			"Conversation:\n%s\n",
		n,
		conversation,
	)
}

func formatSuggestionConversation(messages []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}

		role := strings.ToLower(strings.TrimSpace(msg.Role))
		switch role {
		case "user", "human":
			parts = append(parts, "User: "+content)
		case "assistant", "ai":
			parts = append(parts, "Assistant: "+content)
		default:
			parts = append(parts, strings.TrimSpace(msg.Role)+": "+content)
		}
	}
	return strings.Join(parts, "\n")
}

func parseJSONStringList(raw string) []string {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return nil
	}
	if strings.HasPrefix(candidate, "```") {
		lines := strings.Split(candidate, "\n")
		if len(lines) >= 3 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			candidate = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}

	start := strings.Index(candidate, "[")
	end := strings.LastIndex(candidate, "]")
	if start < 0 || end <= start {
		return nil
	}

	var decoded []any
	if err := json.Unmarshal([]byte(candidate[start:end+1]), &decoded); err != nil {
		return nil
	}

	out := make([]string, 0, len(decoded))
	for _, item := range decoded {
		text, ok := item.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}

func finalizeSuggestions(primary, fallback []string, n int) []string {
	if n <= 0 {
		return []string{}
	}

	out := make([]string, 0, n)
	seen := make(map[string]struct{}, n)
	appendUnique := func(items []string) {
		for _, item := range items {
			text := strings.TrimSpace(strings.ReplaceAll(item, "\n", " "))
			if text == "" {
				continue
			}
			key := strings.ToLower(text)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, text)
			if len(out) == n {
				return
			}
		}
	}

	appendUnique(primary)
	if len(out) < n {
		appendUnique(fallback)
	}
	return out
}

func fallbackSuggestions(messages []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}, n int) []string {
	lastUser := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") || strings.EqualFold(messages[i].Role, "human") {
			lastUser = strings.TrimSpace(messages[i].Content)
			break
		}
	}
	if lastUser == "" {
		return []string{}
	}
	return localizedFallbackSuggestions(lastUser, n)
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
	if err := validateThreadID(threadID); err != nil {
		return "", err
	}
	if path == "" {
		return "", errors.New("thread_id and path are required")
	}
	return s.resolveThreadVirtualPath(threadID, path)
}

func (s *Server) resolveThreadVirtualPath(threadID, virtualPath string) (string, error) {
	if err := validateThreadID(threadID); err != nil {
		return "", err
	}
	stripped := strings.TrimLeft(strings.TrimSpace(virtualPath), "/")
	const prefix = "mnt/user-data"
	if stripped != prefix && !strings.HasPrefix(stripped, prefix+"/") {
		return "", fmt.Errorf("path must start with /%s", prefix)
	}

	relative := strings.TrimLeft(strings.TrimPrefix(stripped, prefix), "/")
	base := filepath.Clean(s.threadRoot(threadID))
	actual := filepath.Clean(filepath.Join(base, filepath.FromSlash(relative)))
	rel, err := filepath.Rel(base, actual)
	if err != nil {
		return "", errors.New("access denied: path traversal detected")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("access denied: path traversal detected")
	}
	if err := ensureResolvedPathWithinBase(base, actual); err != nil {
		return "", err
	}
	return actual, nil
}

func ensureResolvedPathWithinBase(base, actual string) error {
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.New("access denied: path traversal detected")
		}
		resolvedBase = filepath.Clean(base)
	}

	resolvedActual, err := filepath.EvalSymlinks(actual)
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.New("access denied: path traversal detected")
		}
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(actual))
		if parentErr != nil {
			if !os.IsNotExist(parentErr) {
				return errors.New("access denied: path traversal detected")
			}
			parent = filepath.Clean(filepath.Dir(actual))
		}
		resolvedActual = filepath.Join(parent, filepath.Base(actual))
	}

	rel, err := filepath.Rel(resolvedBase, resolvedActual)
	if err != nil {
		return errors.New("access denied: path traversal detected")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("access denied: path traversal detected")
	}
	return nil
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
	metadata, err := validateSkillFrontmatter(string(content))
	if err != nil {
		return gatewaySkill{}, err
	}
	skillName := metadata["name"]

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
		Category:    resolveSkillCategory(metadata["category"], skillCategoryCustom),
		License:     firstNonEmpty(metadata["license"], "Unknown"),
		Enabled:     true,
	}

	s.uiStateMu.Lock()
	if s.skills == nil {
		s.skills = map[string]gatewaySkill{}
	}
	s.skills[skillStorageKey(skill.Category, skill.Name)] = skill
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

func validateSkillFrontmatter(content string) (map[string]string, error) {
	frontmatterText, ok, err := extractSkillFrontmatter(content)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("invalid skill: no YAML frontmatter found")
	}

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(frontmatterText), &raw); err != nil {
		return nil, fmt.Errorf("invalid skill: invalid YAML frontmatter: %w", err)
	}
	if raw == nil {
		return nil, errors.New("invalid skill: frontmatter must be a YAML dictionary")
	}

	allowedKeys := map[string]struct{}{
		"name":          {},
		"description":   {},
		"license":       {},
		"allowed-tools": {},
		"metadata":      {},
		"compatibility": {},
		"version":       {},
		"author":        {},
		"category":      {},
	}
	for key := range raw {
		if _, ok := allowedKeys[strings.ToLower(strings.TrimSpace(key))]; !ok {
			return nil, fmt.Errorf("invalid skill: unexpected key %q in SKILL.md frontmatter", key)
		}
	}

	name, ok := raw["name"].(string)
	if !ok {
		return nil, errors.New("invalid skill: missing name")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("invalid skill: name cannot be empty")
	}
	if !skillFrontmatterNameRE.MatchString(name) || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		return nil, fmt.Errorf("invalid skill: name %q must be hyphen-case", name)
	}
	if len(name) > 64 {
		return nil, fmt.Errorf("invalid skill: name %q is too long", name)
	}

	description, ok := raw["description"].(string)
	if !ok {
		return nil, errors.New("invalid skill: missing description")
	}
	description = strings.TrimSpace(description)
	if strings.ContainsAny(description, "<>") {
		return nil, errors.New("invalid skill: description cannot contain angle brackets")
	}
	if len(description) > 1024 {
		return nil, errors.New("invalid skill: description is too long")
	}

	metadata := map[string]string{
		"name":        name,
		"description": description,
	}
	for _, key := range []string{"category", "license"} {
		if value, ok := raw[key].(string); ok {
			metadata[key] = strings.TrimSpace(value)
		}
	}
	return metadata, nil
}

func extractSkillFrontmatter(content string) (string, bool, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	if !scanner.Scan() {
		return "", false, scanner.Err()
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return "", false, nil
	}
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			return strings.Join(lines, "\n"), true, nil
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return "", false, err
	}
	return "", false, errors.New("invalid skill: invalid frontmatter format")
}

func parseSkillFrontmatter(content string) map[string]string {
	result := map[string]string{}
	frontmatterText, ok, err := extractSkillFrontmatter(content)
	if err != nil || !ok {
		return result
	}
	scanner := bufio.NewScanner(strings.NewReader(frontmatterText))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
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

func (s *Server) loadGatewayState() error {
	path := s.gatewayStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var state gatewayPersistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	s.uiStateMu.Lock()
	defer s.uiStateMu.Unlock()
	if state.Skills != nil {
		s.skills = mergeGatewaySkills(defaultGatewaySkills(), normalizePersistedSkills(state.Skills))
	}
	if state.MCPConfig.MCPServers != nil {
		s.mcpConfig = state.MCPConfig
	}
	if state.Agents != nil {
		s.setAgentsLocked(state.Agents)
	}
	s.setUserProfileLocked(state.UserProfile)
	s.memoryThread = strings.TrimSpace(state.MemoryThread)
	if state.Memory.Version != "" {
		s.setMemoryLocked(state.Memory)
	}
	return nil
}

func (s *Server) persistGatewayState() error {
	s.uiStateMu.RLock()
	state := gatewayPersistedState{
		Skills:       s.skills,
		MCPConfig:    s.mcpConfig,
		Agents:       s.getAgentsLocked(),
		UserProfile:  s.getUserProfileLocked(),
		MemoryThread: s.memoryThread,
		Memory:       s.getMemoryLocked(),
	}
	s.uiStateMu.RUnlock()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.gatewayStatePath(), data, 0o644)
}

func defaultGatewaySkills() map[string]gatewaySkill {
	return map[string]gatewaySkill{
		skillStorageKey(skillCategoryPublic, "deep-research"): {
			Name:        "deep-research",
			Description: "Research and summarize a topic with structured outputs.",
			Category:    skillCategoryPublic,
			License:     "MIT",
			Enabled:     true,
		},
		skillStorageKey(skillCategoryPublic, "code-assist"): {
			Name:        "code-assist",
			Description: "Code reading, patching, and debugging workflows.",
			Category:    skillCategoryPublic,
			License:     "MIT",
			Enabled:     true,
		},
	}
}

func findGatewaySkill(skills map[string]gatewaySkill, name, category string) (gatewaySkill, bool) {
	_, skill, ok := findGatewaySkillEntry(skills, name, category)
	return skill, ok
}

func findGatewaySkillEntry(skills map[string]gatewaySkill, name, category string) (string, gatewaySkill, bool) {
	normalizedName := sanitizeSkillName(name)
	if normalizedName == "" {
		return "", gatewaySkill{}, false
	}

	if normalizedCategory := normalizeSkillCategory(category); normalizedCategory != "" {
		key := skillStorageKey(normalizedCategory, normalizedName)
		skill, ok := skills[key]
		return key, skill, ok
	}

	publicKey := skillStorageKey(skillCategoryPublic, normalizedName)
	if skill, ok := skills[publicKey]; ok {
		return publicKey, skill, true
	}

	customKey := skillStorageKey(skillCategoryCustom, normalizedName)
	if skill, ok := skills[customKey]; ok {
		return customKey, skill, true
	}

	if skill, ok := skills[normalizedName]; ok {
		return normalizedName, normalizeGatewaySkill(skill, normalizedName, ""), true
	}
	return "", gatewaySkill{}, false
}

func normalizePersistedSkills(skills map[string]gatewaySkill) map[string]gatewaySkill {
	if len(skills) == 0 {
		return map[string]gatewaySkill{}
	}

	normalized := make(map[string]gatewaySkill, len(skills))
	for key, skill := range skills {
		fallbackCategory, fallbackName := splitSkillStorageKey(key)
		out := normalizeGatewaySkill(skill, fallbackName, fallbackCategory)
		normalized[skillStorageKey(out.Category, out.Name)] = out
	}
	return normalized
}

func mergeGatewaySkills(base, overlay map[string]gatewaySkill) map[string]gatewaySkill {
	merged := make(map[string]gatewaySkill, len(base)+len(overlay))
	for key, skill := range base {
		merged[key] = skill
	}
	for key, skill := range overlay {
		merged[key] = skill
	}
	return merged
}

func normalizeGatewaySkill(skill gatewaySkill, fallbackName, fallbackCategory string) gatewaySkill {
	skill.Name = sanitizeSkillName(firstNonEmpty(skill.Name, fallbackName))
	if fallbackCategory == "" {
		fallbackCategory = inferSkillCategory(skill.Name)
	}
	skill.Category = resolveSkillCategory(skill.Category, fallbackCategory)
	if skill.Category == "" {
		skill.Category = skillCategoryPublic
	}
	return skill
}

func normalizeSkillCategory(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "":
		return ""
	case skillCategoryPublic:
		return skillCategoryPublic
	case skillCategoryCustom:
		return skillCategoryCustom
	default:
		return ""
	}
}

func resolveSkillCategory(category, fallback string) string {
	if normalized := normalizeSkillCategory(category); normalized != "" {
		return normalized
	}
	if normalizedFallback := normalizeSkillCategory(fallback); normalizedFallback != "" {
		return normalizedFallback
	}
	return ""
}

func inferSkillCategory(name string) string {
	key := skillStorageKey(skillCategoryPublic, name)
	if _, ok := defaultGatewaySkills()[key]; ok {
		return skillCategoryPublic
	}
	return skillCategoryCustom
}

func skillStorageKey(category, name string) string {
	category = normalizeSkillCategory(category)
	name = sanitizeSkillName(name)
	if name == "" {
		return ""
	}
	if category == "" {
		category = skillCategoryPublic
	}
	return category + ":" + name
}

func splitSkillStorageKey(key string) (string, string) {
	key = strings.TrimSpace(key)
	if category, name, ok := strings.Cut(key, ":"); ok {
		return normalizeSkillCategory(category), sanitizeSkillName(name)
	}
	return "", sanitizeSkillName(key)
}

func defaultGatewayMCPConfig() gatewayMCPConfig {
	return gatewayMCPConfig{
		MCPServers: map[string]gatewayMCPServerConfig{
			"default": {
				Type:        "stdio",
				Enabled:     false,
				Description: "Default MCP server placeholder for deerflow-go.",
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

type rawGatewayModel struct {
	ID                      string `json:"id"`
	Name                    string `json:"name"`
	Model                   string `json:"model"`
	DisplayName             string `json:"display_name"`
	Description             string `json:"description"`
	SupportsThinking        *bool  `json:"supports_thinking"`
	SupportsReasoningEffort *bool  `json:"supports_reasoning_effort"`
	SupportsVision          *bool  `json:"supports_vision"`
}

type gatewayConfigFile struct {
	Models []gatewayConfigModel `yaml:"models"`
}

type gatewayConfigModel struct {
	Name                    string `yaml:"name"`
	Model                   string `yaml:"model"`
	DisplayName             string `yaml:"display_name"`
	Description             string `yaml:"description"`
	SupportsThinking        *bool  `yaml:"supports_thinking"`
	SupportsReasoningEffort *bool  `yaml:"supports_reasoning_effort"`
	SupportsVision          *bool  `yaml:"supports_vision"`
}

func configuredGatewayModels(defaultModel string) []gatewayModel {
	if models := configuredGatewayModelsFromJSON(defaultModel); len(models) > 0 {
		return models
	}
	if models := configuredGatewayModelsFromList(defaultModel); len(models) > 0 {
		return models
	}
	if models := configuredGatewayModelsFromConfig(defaultModel); len(models) > 0 {
		return models
	}
	return []gatewayModel{defaultGatewayModel(defaultModel)}
}

func configuredGatewayModelsFromJSON(defaultModel string) []gatewayModel {
	raw := strings.TrimSpace(os.Getenv("DEERFLOW_MODELS_JSON"))
	if raw == "" {
		return nil
	}

	var parsed []rawGatewayModel
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}

	models := make([]gatewayModel, 0, len(parsed))
	seen := map[string]struct{}{}
	for _, item := range parsed {
		model := normalizeGatewayModel(item, defaultModel)
		if model.Name == "" {
			continue
		}
		if _, exists := seen[model.Name]; exists {
			continue
		}
		seen[model.Name] = struct{}{}
		models = append(models, model)
	}
	return models
}

func configuredGatewayModelsFromList(defaultModel string) []gatewayModel {
	raw := strings.TrimSpace(os.Getenv("DEERFLOW_MODELS"))
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	models := make([]gatewayModel, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}

		name := entry
		modelID := entry
		if left, right, ok := strings.Cut(entry, "="); ok {
			name = strings.TrimSpace(left)
			modelID = strings.TrimSpace(right)
		}

		model := normalizeGatewayModel(rawGatewayModel{
			Name:  name,
			Model: modelID,
		}, defaultModel)
		if model.Name == "" {
			continue
		}
		if _, exists := seen[model.Name]; exists {
			continue
		}
		seen[model.Name] = struct{}{}
		models = append(models, model)
	}
	return models
}

func configuredGatewayModelsFromConfig(defaultModel string) []gatewayModel {
	configPath := gatewayModelCatalogConfigPath()
	if configPath == "" {
		return nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}

	var cfg gatewayConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}

	models := make([]gatewayModel, 0, len(cfg.Models))
	seen := map[string]struct{}{}
	for _, item := range cfg.Models {
		model := normalizeGatewayModel(rawGatewayModel{
			Name:                    item.Name,
			Model:                   item.Model,
			DisplayName:             item.DisplayName,
			Description:             item.Description,
			SupportsThinking:        item.SupportsThinking,
			SupportsReasoningEffort: item.SupportsReasoningEffort,
			SupportsVision:          item.SupportsVision,
		}, defaultModel)
		if model.Name == "" {
			continue
		}
		if _, exists := seen[model.Name]; exists {
			continue
		}
		seen[model.Name] = struct{}{}
		models = append(models, model)
	}
	return models
}

func gatewayModelCatalogConfigPath() string {
	if path := strings.TrimSpace(os.Getenv("DEERFLOW_CONFIG_PATH")); path != "" {
		return path
	}

	const defaultConfigPath = "config.yaml"
	if _, err := os.Stat(defaultConfigPath); err == nil {
		return defaultConfigPath
	}
	return ""
}

func defaultGatewayModel(defaultModel string) gatewayModel {
	name := strings.TrimSpace(defaultModel)
	if name == "" {
		name = "qwen/Qwen3.5-9B"
	}
	thinking, reasoning := inferGatewayModelCapabilities(name)
	return gatewayModel{
		ID:                      "default",
		Name:                    name,
		Model:                   name,
		DisplayName:             name,
		Description:             "Default model configured by deerflow-go",
		SupportsThinking:        thinking,
		SupportsReasoningEffort: reasoning,
		SupportsVision:          inferGatewayModelVisionSupport(name),
	}
}

func normalizeGatewayModel(raw rawGatewayModel, defaultModel string) gatewayModel {
	name := strings.TrimSpace(raw.Name)
	modelID := strings.TrimSpace(raw.Model)
	switch {
	case name == "" && modelID != "":
		name = modelID
	case modelID == "" && name != "":
		modelID = name
	case name == "" && modelID == "":
		fallback := defaultGatewayModel(defaultModel)
		name = fallback.Name
		modelID = fallback.Model
	}

	id := strings.TrimSpace(raw.ID)
	if id == "" {
		id = name
	}
	displayName := strings.TrimSpace(raw.DisplayName)
	if displayName == "" {
		displayName = name
	}

	thinking, reasoning := inferGatewayModelCapabilities(firstNonEmpty(modelID, name))
	if raw.SupportsThinking != nil {
		thinking = *raw.SupportsThinking
	}
	if raw.SupportsReasoningEffort != nil {
		reasoning = *raw.SupportsReasoningEffort
	}
	vision := inferGatewayModelVisionSupport(firstNonEmpty(modelID, name))
	if raw.SupportsVision != nil {
		vision = *raw.SupportsVision
	}

	return gatewayModel{
		ID:                      id,
		Name:                    name,
		Model:                   modelID,
		DisplayName:             displayName,
		Description:             strings.TrimSpace(raw.Description),
		SupportsThinking:        thinking,
		SupportsReasoningEffort: reasoning,
		SupportsVision:          vision,
	}
}

func inferGatewayModelVisionSupport(name string) bool {
	return agent.ModelLikelySupportsVision(name)
}

func inferGatewayModelCapabilities(name string) (supportsThinking bool, supportsReasoningEffort bool) {
	model := strings.ToLower(strings.TrimSpace(name))
	if model == "" {
		return false, false
	}

	for _, token := range []string{
		"gpt-5", "o1", "o3", "o4", "qwen3", "qwq", "deepseek-r1", "gemini-2.5", "claude-3.7", "claude-3-7", "claude-sonnet-4", "reasoner", "thinking",
	} {
		if strings.Contains(model, token) {
			return true, true
		}
	}
	return false, false
}

func findConfiguredGatewayModel(defaultModel, modelName string) (gatewayModel, bool) {
	target := strings.TrimSpace(modelName)
	if target == "" {
		return gatewayModel{}, false
	}
	for _, model := range configuredGatewayModels(defaultModel) {
		if model.Name == target {
			return model, true
		}
	}
	return gatewayModel{}, false
}

func findConfiguredGatewayModelByNameOrID(defaultModel, modelName string) (gatewayModel, bool) {
	target := strings.TrimSpace(modelName)
	if target == "" {
		return gatewayModel{}, false
	}
	for _, model := range configuredGatewayModels(defaultModel) {
		if strings.EqualFold(model.Name, target) || strings.EqualFold(model.Model, target) {
			return model, true
		}
	}
	return gatewayModel{}, false
}

func normalizeAgentName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if !agentNameRE.MatchString(name) {
		return "", false
	}
	return strings.ToLower(name), true
}

func nullableString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
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

func (s *Server) gatewayMemoryStoragePath() string {
	if s.memoryStore != nil {
		return "postgres://memories"
	}
	return filepath.Join(s.dataRoot, "memory.json")
}

func (s *Server) refreshGatewayMemoryCache(ctx context.Context) {
	doc, err := s.loadGatewayMemoryDocument(ctx)
	if err != nil {
		return
	}
	s.uiStateMu.Lock()
	s.memoryThread = strings.TrimSpace(doc.SessionID)
	s.setMemoryLocked(gatewayMemoryFromDocument(doc))
	s.uiStateMu.Unlock()
}

func (s *Server) loadGatewayMemoryDocument(ctx context.Context) (memory.Document, error) {
	if s == nil || s.memoryStore == nil {
		return memory.Document{}, memory.ErrNotFound
	}
	threadID := strings.TrimSpace(s.memoryThread)
	if threadID == "" {
		return memory.Document{}, memory.ErrNotFound
	}
	return s.memoryStore.Load(ctx, threadID)
}

func (s *Server) replaceGatewayMemoryDocument(ctx context.Context, doc memory.Document) error {
	if s == nil {
		return errors.New("server is nil")
	}
	threadID := strings.TrimSpace(doc.SessionID)
	if threadID == "" {
		threadID = strings.TrimSpace(s.memoryThread)
	}
	if s.memoryStore != nil && threadID != "" {
		doc.SessionID = threadID
		if strings.TrimSpace(doc.Source) == "" {
			doc.Source = threadID
		}
		if doc.UpdatedAt.IsZero() {
			doc.UpdatedAt = time.Now().UTC()
		}
		if err := s.memoryStore.Save(ctx, doc); err != nil {
			return err
		}
	}
	s.uiStateMu.Lock()
	if threadID != "" {
		s.memoryThread = threadID
	}
	s.setMemoryLocked(gatewayMemoryFromDocument(doc))
	s.uiStateMu.Unlock()
	return nil
}

func gatewayMemoryFromDocument(doc memory.Document) gatewayMemoryResponse {
	resp := defaultGatewayMemory()
	if !doc.UpdatedAt.IsZero() {
		resp.LastUpdated = doc.UpdatedAt.UTC().Format(time.RFC3339)
	}
	resp.User.WorkContext = gatewayMemorySection(doc.User.WorkContext, doc.UpdatedAt)
	resp.User.PersonalContext = gatewayMemorySection(doc.User.PersonalContext, doc.UpdatedAt)
	resp.User.TopOfMind = gatewayMemorySection(doc.User.TopOfMind, doc.UpdatedAt)
	resp.History.RecentMonths = gatewayMemorySection(doc.History.RecentMonths, doc.UpdatedAt)
	resp.History.EarlierContext = gatewayMemorySection(doc.History.EarlierContext, doc.UpdatedAt)
	resp.History.LongTermBackground = gatewayMemorySection("", time.Time{})
	resp.Facts = make([]memoryFact, 0, len(doc.Facts))
	for _, fact := range doc.Facts {
		resp.Facts = append(resp.Facts, memoryFact{
			ID:         fact.ID,
			Content:    fact.Content,
			Category:   fact.Category,
			Confidence: fact.Confidence,
			CreatedAt:  formatMemoryTime(fact.CreatedAt),
			Source:     doc.SessionID,
		})
	}
	return resp
}

func gatewayMemorySection(summary string, updatedAt time.Time) memorySection {
	return memorySection{
		Summary:   strings.TrimSpace(summary),
		UpdatedAt: formatMemoryTime(updatedAt),
	}
}

func formatMemoryTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
