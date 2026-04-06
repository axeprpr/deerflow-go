package langgraphcompat

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const compatRootEnv = "DEERFLOW_COMPAT_ROOT"

func (s *Server) agentsDir() string {
	return s.primaryAgentsRoot()
}

func (s *Server) compatRoot() string {
	if root := strings.TrimSpace(os.Getenv(compatRootEnv)); root != "" {
		return root
	}
	for _, key := range []string{"DEERFLOW_DATA_ROOT", "DEER_FLOW_HOME"} {
		if root := strings.TrimSpace(os.Getenv(key)); root != "" {
			return root
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return strings.TrimSpace(s.dataRoot)
	}
	if root, ok := findGoModuleRoot(cwd); ok {
		return root
	}
	return cwd
}

func findGoModuleRoot(start string) (string, bool) {
	start = strings.TrimSpace(start)
	if start == "" {
		return "", false
	}
	dir := filepath.Clean(start)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func (s *Server) agentsRoots() []string {
	roots := []string{}
	if compatRoot := strings.TrimSpace(s.compatRoot()); compatRoot != "" {
		roots = append(roots, filepath.Join(compatRoot, "agents"))
	}
	if dataRoot := strings.TrimSpace(s.dataRoot); dataRoot != "" {
		roots = append(roots, filepath.Join(dataRoot, "agents"))
	}
	return dedupeCleanPaths(roots)
}

func (s *Server) primaryAgentsRoot() string {
	roots := s.agentsRoots()
	if len(roots) == 0 {
		return filepath.Join("agents")
	}
	return roots[0]
}

func (s *Server) existingAgentDir(name string) (string, bool) {
	for _, root := range s.agentsRoots() {
		dir := filepath.Join(root, name)
		info, err := os.Stat(dir)
		if err == nil && info.IsDir() {
			return dir, true
		}
	}
	return "", false
}

func (s *Server) userProfilePaths() []string {
	paths := []string{}
	if compatRoot := strings.TrimSpace(s.compatRoot()); compatRoot != "" {
		paths = append(paths, filepath.Join(compatRoot, "USER.md"))
	}
	if dataRoot := strings.TrimSpace(s.dataRoot); dataRoot != "" {
		paths = append(paths, filepath.Join(dataRoot, "USER.md"))
	}
	return dedupeCleanPaths(paths)
}

func (s *Server) primaryUserProfilePath() string {
	paths := s.userProfilePaths()
	if len(paths) == 0 {
		return "USER.md"
	}
	return paths[0]
}

func dedupeCleanPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func (s *Server) loadGatewayCompatFiles() error {
	s.uiStateMu.Lock()
	defer s.uiStateMu.Unlock()
	s.compatFSManaged = true
	s.syncGatewayCompatFilesLocked()
	return nil
}

func (s *Server) refreshGatewayCompatFiles() {
	if s == nil {
		return
	}
	s.uiStateMu.Lock()
	defer s.uiStateMu.Unlock()
	if !s.compatFSManaged && strings.TrimSpace(os.Getenv(compatRootEnv)) == "" {
		return
	}
	s.compatFSManaged = true
	s.syncGatewayCompatFilesLocked()
}

func (s *Server) syncGatewayCompatFilesLocked() {
	if agents, ok := s.loadAgentsFromDiskLocked(); ok {
		s.setAgentsLocked(agents)
	} else {
		s.setAgentsLocked(nil)
	}
	if profile, ok := s.loadUserProfileFromDiskLocked(); ok {
		s.setUserProfileLocked(profile)
	} else {
		s.setUserProfileLocked("")
	}
}

func (s *Server) loadAgentsFromDiskLocked() (map[string]gatewayAgent, bool) {
	agents := map[string]gatewayAgent{}
	foundAny := false
	for _, agentsDir := range s.agentsRoots() {
		entries, err := os.ReadDir(agentsDir)
		if err != nil {
			continue
		}
		foundAny = true
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			agent, ok := s.loadAgentFromDisk(filepath.Join(agentsDir, entry.Name()), entry.Name())
			if !ok {
				continue
			}
			if _, exists := agents[agent.Name]; exists {
				continue
			}
			agents[agent.Name] = agent
		}
	}
	if !foundAny {
		return nil, false
	}
	return agents, true
}

func (s *Server) loadAgentFromDisk(dir string, fallbackName string) (gatewayAgent, bool) {
	normalized, ok := normalizeAgentName(fallbackName)
	if !ok {
		return gatewayAgent{}, false
	}

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
	for _, path := range s.userProfilePaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return strings.TrimSpace(string(data)), true
	}
	return "", false
}
