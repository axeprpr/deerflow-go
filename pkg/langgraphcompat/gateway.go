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
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
)

type gatewayModel struct {
	ID                      string  `json:"id"`
	Name                    string  `json:"name"`
	Model                   string  `json:"model"`
	DisplayName             string  `json:"display_name"`
	Description             string  `json:"description,omitempty"`
	RequestTimeoutSeconds   float64 `json:"request_timeout,omitempty"`
	SupportsThinking        bool    `json:"supports_thinking,omitempty"`
	SupportsReasoningEffort bool    `json:"supports_reasoning_effort,omitempty"`
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

var forcedDownloadArtifactMIMETypes = map[string]struct{}{
	"text/html":             {},
	"application/xhtml+xml": {},
	"image/svg+xml":         {},
}

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

type memoryFactSourceThread struct {
	ThreadID  string `json:"thread_id"`
	AgentName string `json:"agent_name,omitempty"`
}

type memoryFact struct {
	ID           string                  `json:"id"`
	Content      string                  `json:"content"`
	Category     string                  `json:"category"`
	Confidence   float64                 `json:"confidence"`
	CreatedAt    string                  `json:"createdAt"`
	Source       string                  `json:"source"`
	SourceThread *memoryFactSourceThread `json:"source_thread,omitempty"`
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
	mux.HandleFunc("GET /api/memory/export", s.handleMemoryExport)
	mux.HandleFunc("POST /api/memory/import", s.handleMemoryImport)
	mux.HandleFunc("POST /api/memory/reload", s.handleMemoryReload)
	mux.HandleFunc("DELETE /api/memory", s.handleMemoryClear)
	mux.HandleFunc("POST /api/memory/facts", s.handleMemoryFactCreate)
	mux.HandleFunc("DELETE /api/memory/facts/{fact_id}", s.handleMemoryFactDelete)
	mux.HandleFunc("PATCH /api/memory/facts/{fact_id}", s.handleMemoryFactUpdate)
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

func (s *Server) memoryFactsWithSourceThreads(facts []memoryFact) []memoryFact {
	if len(facts) == 0 {
		return facts
	}
	enriched := make([]memoryFact, 0, len(facts))
	for _, fact := range facts {
		clone := fact
		if source := strings.TrimSpace(fact.Source); source != "" {
			s.sessionsMu.RLock()
			session := s.sessions[source]
			s.sessionsMu.RUnlock()
			clone.SourceThread = &memoryFactSourceThread{ThreadID: source}
			if session != nil {
				clone.SourceThread.AgentName = stringValue(session.Metadata["agent_name"])
			}
		}
		enriched = append(enriched, clone)
	}
	return enriched
}

func gatewayMemoryResponseFromDocument(doc pkgmemory.Document) gatewayMemoryResponse {
	updatedAt := doc.UpdatedAt.UTC().Format(time.RFC3339)
	facts := make([]memoryFact, 0, len(doc.Facts))
	for _, fact := range doc.Facts {
		facts = append(facts, memoryFact{
			ID:         fact.ID,
			Content:    fact.Content,
			Category:   fact.Category,
			Confidence: fact.Confidence,
			CreatedAt:  fact.CreatedAt.UTC().Format(time.RFC3339),
			Source:     fact.Source,
		})
	}
	return gatewayMemoryResponse{
		Version:     "1",
		LastUpdated: updatedAt,
		User: memoryUser{
			WorkContext:     memorySection{Summary: doc.User.WorkContext, UpdatedAt: updatedAt},
			PersonalContext: memorySection{Summary: doc.User.PersonalContext, UpdatedAt: updatedAt},
			TopOfMind:       memorySection{Summary: doc.User.TopOfMind, UpdatedAt: updatedAt},
		},
		History: memoryHistory{
			RecentMonths:       memorySection{Summary: doc.History.RecentMonths, UpdatedAt: updatedAt},
			EarlierContext:     memorySection{Summary: doc.History.EarlierContext, UpdatedAt: updatedAt},
			LongTermBackground: memorySection{Summary: doc.History.LongTermBackground, UpdatedAt: updatedAt},
		},
		Facts: facts,
	}
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
	mdPath, err := generateUploadMarkdownCompanion(dstPath)
	if err != nil {
		return nil, err
	}
	info := s.uploadInfo(threadID, dstPath, name, n, nowUnix())
	if strings.TrimSpace(mdPath) != "" {
		mdName := filepath.Base(mdPath)
		info["markdown_file"] = mdName
		info["markdown_path"] = "/mnt/user-data/uploads/" + mdName
	}
	return info, nil
}

func (s *Server) uploadInfo(threadID, fullPath, name string, size int64, modified int64) map[string]any {
	virtualPath := "/mnt/user-data/uploads/" + name
	info := map[string]any{
		"filename":     name,
		"size":         size,
		"path":         virtualPath,
		"source_path":  fullPath,
		"virtual_path": virtualPath,
		"artifact_url": "/api/threads/" + threadID + "/artifacts" + virtualPath,
		"extension":    strings.ToLower(filepath.Ext(name)),
		"modified":     modified,
	}
	mdName := strings.TrimSuffix(name, filepath.Ext(name)) + ".md"
	if mdName != name {
		if infoStat, err := os.Stat(filepath.Join(filepath.Dir(fullPath), mdName)); err == nil && !infoStat.IsDir() {
			info["markdown_file"] = mdName
			info["markdown_path"] = "/mnt/user-data/uploads/" + mdName
		}
	}
	return info
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
		if filepath.Clean(file.SourcePath) == absPath || filepath.Clean(s.resolveArtifactPath(threadID, file.Path)) == absPath {
			return true
		}
	}
	for _, path := range s.sessionArtifactPaths(session) {
		if filepath.Clean(s.resolveArtifactPath(threadID, path)) == absPath {
			return true
		}
	}
	return false
}

func shouldForceArtifactDownload(mimeType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0]))
	_, ok := forcedDownloadArtifactMIMETypes[normalized]
	return ok
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
	runes := []rune(text)
	if len(runes) > 48 {
		return string(runes[:48])
	}
	return text
}

func nowUnix() int64 { return time.Now().UTC().Unix() }

func (s *Server) resolveSkillArchivePath(threadID, path string) (string, error) {
	threadID = strings.TrimSpace(threadID)
	path = strings.TrimSpace(path)
	if threadID == "" || path == "" {
		return "", errors.New("thread_id and path are required")
	}
	if idx := strings.Index(path, "/artifacts/mnt/user-data/"); idx >= 0 {
		path = "/" + strings.TrimPrefix(path[idx+len("/artifacts/"):], "/")
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
		if strings.Contains(f.Name, "..") || strings.HasPrefix(f.Name, "/") || strings.HasPrefix(f.Name, "\\") || isWindowsAbsoluteArchivePath(f.Name) {
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
	roots := make([]string, 0, 1)
	walkGatewaySkillFiles(tempDir, func(path string) bool {
		roots = append(roots, filepath.Clean(filepath.Dir(path)))
		return false
	})
	switch len(roots) {
	case 0:
		return "", errors.New("invalid skill archive: missing SKILL.md")
	case 1:
		return roots[0], nil
	default:
		return "", errors.New("invalid skill archive: expected exactly one SKILL.md")
	}
}

func readSkillArchivePreview(archivePath string) (string, error) {
	zipReader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer zipReader.Close()

	type skillEntry struct {
		name string
		body string
	}
	candidates := make([]skillEntry, 0)
	for _, f := range zipReader.File {
		if f.FileInfo().IsDir() || !strings.EqualFold(filepath.Base(f.Name), "SKILL.md") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		data, readErr := io.ReadAll(rc)
		rc.Close()
		if readErr != nil {
			return "", readErr
		}
		candidates = append(candidates, skillEntry{name: f.Name, body: string(data)})
	}
	if len(candidates) == 0 {
		return "", errors.New("invalid skill archive: missing SKILL.md")
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].name < candidates[j].name
	})
	return candidates[0].body, nil
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
	return s.primaryAgentsRoot()
}

func (s *Server) agentDir(name string) string {
	if dir, ok := s.existingAgentDir(name); ok {
		return dir
	}
	return filepath.Join(s.agentsRoot(), name)
}

func (s *Server) userProfilePath() string {
	return s.primaryUserProfilePath()
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
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil {
		if nested, ok := wrapper["state"]; ok && len(nested) > 0 {
			data = nested
		} else if nested, ok := wrapper["gateway"]; ok && len(nested) > 0 {
			data = nested
		} else if nested, ok := wrapper["ui_state"]; ok && len(nested) > 0 {
			data = nested
		} else if nested, ok := wrapper["data"]; ok && len(nested) > 0 {
			data = nested
		}
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
	if mcpRaw := mapFromAny(firstNonNil(raw["mcp_config"], raw["mcpConfig"], raw["mcp"])); mcpRaw != nil {
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
	mergeGatewayMemorySnapshot(&state, raw)
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
	s.hydrateGatewayMemoryCacheLocked(state)
	if agents := s.loadAgentsFromFiles(); len(agents) > 0 {
		existing := s.getAgentsLocked()
		for name, agent := range agents {
			if _, ok := existing[name]; ok {
				continue
			}
			existing[name] = agent
		}
		s.setAgentsLocked(existing)
	}
	if content, ok := s.loadUserProfileFromFile(); ok {
		if strings.TrimSpace(s.getUserProfileLocked()) == "" {
			s.setUserProfileLocked(content)
		}
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
		Memory:      s.gatewayMemorySnapshotLocked(),
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
			"github": {
				Enabled:     false,
				Type:        "stdio",
				Command:     "npx",
				Args:        []string{"-y", "@modelcontextprotocol/server-github"},
				Description: "GitHub tools",
			},
			"filesystem": {
				Enabled:     false,
				Type:        "stdio",
				Command:     "npx",
				Args:        []string{"-y", "@modelcontextprotocol/server-filesystem"},
				Description: "File system access",
			},
			"postgres": {
				Enabled:     false,
				Type:        "stdio",
				Command:     "npx",
				Args:        []string{"-y", "@modelcontextprotocol/server-postgres"},
				Description: "PostgreSQL database access",
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
		filepath.Join("third_party", "deerflow-ui", "skills"),
		filepath.Join("..", "deerflow-ui", "skills"),
		"skills",
	}
	if configuredRoot, ok := gatewayConfiguredSkillsRoot(); ok {
		roots = append(roots, configuredRoot)
	}
	roots = append(roots, executableRelativeSkillRoots(gatewayExecutablePath)...)
	roots = append(roots, filepath.Join(s.dataRoot, "skills"))
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
	for _, discovered := range discoverGatewaySkills(s.gatewaySkillRoots()) {
		skill := discovered
		if enabledOverride != nil {
			if value, ok := enabledOverride[skill.Name]; ok {
				skill.Enabled = value
			}
		}
		if existing, ok := skills[skill.Name]; ok {
			if existing.Category == skillCategoryPublic && skill.Category == skillCategoryCustom {
				skills[skill.Name] = skill
			}
			continue
		}
		skills[skill.Name] = skill
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

func mergeGatewayMCPConfig(base, override gatewayMCPConfig) gatewayMCPConfig {
	merged := gatewayMCPConfig{MCPServers: map[string]gatewayMCPServerConfig{}}
	for name, server := range base.MCPServers {
		merged.MCPServers[name] = normalizeGatewayMCPServer(server)
	}
	for name, server := range override.MCPServers {
		merged.MCPServers[name] = normalizeGatewayMCPServer(server)
	}
	return merged
}

func isWindowsAbsoluteArchivePath(path string) bool {
	if len(path) < 3 || path[1] != ':' {
		return false
	}
	drive := path[0]
	if (drive < 'A' || drive > 'Z') && (drive < 'a' || drive > 'z') {
		return false
	}
	return path[2] == '\\' || path[2] == '/'
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
		RequestTimeoutSeconds:   floatValue(firstNonNil(raw["request_timeout"], raw["requestTimeout"], raw["timeout"])),
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
	if s.memoryCache.Version == "" {
		return defaultGatewayMemory()
	}
	return s.memoryCache
}

func (s *Server) setMemoryLocked(memory gatewayMemoryResponse) {
	s.memoryCache = memory
}
