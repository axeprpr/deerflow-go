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
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
}

type gatewayMCPConfig struct {
	MCPServers map[string]gatewayMCPServerConfig `json:"mcp_servers"`
}

type gatewayPersistedState struct {
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
	model := strings.TrimSpace(s.defaultModel)
	if model == "" {
		model = "qwen/Qwen3.5-9B"
	}
	models := []gatewayModel{
		{
			ID:                      "default",
			Name:                    model,
			Model:                   model,
			DisplayName:             model,
			Description:             "Default model configured by deerflow-go",
			SupportsThinking:        true,
			SupportsReasoningEffort: true,
		},
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (s *Server) handleModelGet(w http.ResponseWriter, r *http.Request) {
	modelName := strings.TrimSpace(r.PathValue("model_name"))
	model := strings.TrimSpace(s.defaultModel)
	if model == "" {
		model = "qwen/Qwen3.5-9B"
	}
	if modelName == "" || modelName != model {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": fmt.Sprintf("Model '%s' not found", modelName)})
		return
	}
	writeJSON(w, http.StatusOK, gatewayModel{
		ID:                      "default",
		Name:                    model,
		Model:                   model,
		DisplayName:             model,
		Description:             "Default model configured by deerflow-go",
		SupportsThinking:        true,
		SupportsReasoningEffort: true,
	})
}

func (s *Server) handleSkillsList(w http.ResponseWriter, r *http.Request) {
	s.uiStateMu.RLock()
	defer s.uiStateMu.RUnlock()
	skills := make([]gatewaySkill, 0, len(s.skills))
	for _, skill := range s.skills {
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
	skill, ok := s.skills[name]
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
	skill, ok := s.skills[name]
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
	if err := s.persistGatewayState(); err != nil {
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
	if err := s.persistGatewayState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": nullableString(req.Content)})
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
	if err := s.persistGatewayState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to persist state"})
		return
	}
	writeJSON(w, http.StatusOK, mem)
}

func (s *Server) handleMemoryConfigGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":                   true,
		"storage_path":              filepath.Join(s.dataRoot, "memory.json"),
		"debounce_seconds":          30,
		"max_facts":                 100,
		"fact_confidence_threshold": 0.7,
		"injection_enabled":         true,
		"max_injection_tokens":      2000,
	})
}

func (s *Server) handleMemoryStatusGet(w http.ResponseWriter, r *http.Request) {
	s.uiStateMu.RLock()
	mem := s.getMemoryLocked()
	s.uiStateMu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"config": map[string]any{
			"enabled":                   true,
			"storage_path":              filepath.Join(s.dataRoot, "memory.json"),
			"debounce_seconds":          30,
			"max_facts":                 100,
			"fact_confidence_threshold": 0.7,
			"injection_enabled":         true,
			"max_injection_tokens":      2000,
		},
		"data": mem,
	})
}

func (s *Server) handleChannelsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service_running": false,
		"channels":        map[string]any{},
	})
}

func (s *Server) handleChannelRestart(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
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
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		N int `json:"n"`
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
	if s.skills == nil {
		s.skills = map[string]gatewaySkill{}
	}
	s.skills[skillName] = skill
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
		s.skills = state.Skills
	}
	if state.MCPConfig.MCPServers != nil {
		s.mcpConfig = state.MCPConfig
	}
	if state.Agents != nil {
		s.setAgentsLocked(state.Agents)
	}
	s.setUserProfileLocked(state.UserProfile)
	if state.Memory.Version != "" {
		s.setMemoryLocked(state.Memory)
	}
	return nil
}

func (s *Server) persistGatewayState() error {
	s.uiStateMu.RLock()
	state := gatewayPersistedState{
		Skills:      s.skills,
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

func defaultGatewaySkills() map[string]gatewaySkill {
	return map[string]gatewaySkill{
		"deep-research": {
			Name:        "deep-research",
			Description: "Research and summarize a topic with structured outputs.",
			Category:    "research",
			License:     "MIT",
			Enabled:     true,
		},
		"code-assist": {
			Name:        "code-assist",
			Description: "Code reading, patching, and debugging workflows.",
			Category:    "engineering",
			License:     "MIT",
			Enabled:     true,
		},
	}
}

func defaultGatewayMCPConfig() gatewayMCPConfig {
	return gatewayMCPConfig{
		MCPServers: map[string]gatewayMCPServerConfig{
			"default": {
				Enabled:     true,
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
