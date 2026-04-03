package langgraphcompat

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var gatewayExecutablePath = os.Executable

func (s *Server) currentGatewaySkills() map[string]gatewaySkill {
	discovered := mergeGatewaySkills(defaultGatewaySkills(), discoverGatewaySkills(s.gatewaySkillRoots()))

	s.uiStateMu.RLock()
	persisted := normalizePersistedSkills(s.skills)
	s.uiStateMu.RUnlock()

	return mergeGatewaySkillState(discovered, persisted)
}

func (s *Server) gatewaySkillRoots() []string {
	roots := make([]string, 0, 8)
	explicitRoots := filepath.SplitList(strings.TrimSpace(os.Getenv("DEERFLOW_SKILLS_ROOT")))
	for _, raw := range explicitRoots {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			roots = append(roots, trimmed)
		}
	}
	if len(roots) == 0 {
		roots = append(roots, filepath.Join(os.TempDir(), "deerflow-ui", "skills"))
		if uiRoot := strings.TrimSpace(os.Getenv("DEERFLOW_UI_ROOT")); uiRoot != "" {
			roots = append(roots, filepath.Join(uiRoot, "skills"))
		}
		roots = append(roots, executableRelativeSkillRoots(gatewayExecutablePath)...)
		if cwd, err := os.Getwd(); err == nil {
			roots = append(roots, filepath.Join(cwd, "skills"))
			roots = append(roots, filepath.Join(cwd, "..", "skills"))
			roots = append(roots, filepath.Join(cwd, "..", "..", "skills"))
			roots = append(roots, filepath.Join(cwd, "..", "deerflow-ui", "skills"))
			roots = append(roots, filepath.Join(cwd, "..", "..", "deerflow-ui", "skills"))
		}
	}
	roots = append(roots, filepath.Join(s.dataRoot, "skills"))
	if configuredRoot, ok := gatewayConfiguredSkillsRoot(); ok {
		roots = append(roots, configuredRoot)
	}
	return uniqueCleanPaths(roots)
}

func gatewayConfiguredSkillsRoot() (string, bool) {
	configPath, ok := resolveGatewayConfigPath()
	if !ok {
		return "", false
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", false
	}
	var raw struct {
		Skills struct {
			Path string `yaml:"path"`
		} `yaml:"skills"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return "", false
	}
	root := strings.TrimSpace(raw.Skills.Path)
	if root == "" {
		return "", false
	}
	if !filepath.IsAbs(root) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", false
		}
		root = filepath.Join(cwd, root)
	}
	return filepath.Clean(root), true
}

func (s *Server) gatewayCustomSkillsRoot() string {
	if configuredRoot, ok := gatewayConfiguredSkillsRoot(); ok {
		return filepath.Join(configuredRoot, skillCategoryCustom)
	}
	return filepath.Join(s.dataRoot, "skills", skillCategoryCustom)
}

func executableRelativeSkillRoots(resolveExe func() (string, error)) []string {
	if resolveExe == nil {
		return nil
	}
	exePath, err := resolveExe()
	if err != nil || strings.TrimSpace(exePath) == "" {
		return nil
	}
	exeDir := filepath.Dir(exePath)
	return []string{
		filepath.Join(exeDir, "skills"),
		filepath.Join(exeDir, "..", "skills"),
		filepath.Join(exeDir, "..", "..", "skills"),
	}
}

func discoverGatewaySkills(roots []string) map[string]gatewaySkill {
	discovered := map[string]gatewaySkill{}
	for _, root := range roots {
		for _, category := range []string{skillCategoryPublic, skillCategoryCustom} {
			categoryRoot := filepath.Join(root, category)
			for key, skill := range scanGatewaySkillCategory(categoryRoot, category) {
				discovered[key] = skill
			}
		}
	}
	return discovered
}

func scanGatewaySkillCategory(root, category string) map[string]gatewaySkill {
	skills := map[string]gatewaySkill{}
	walkGatewaySkillFiles(root, func(path string) bool {
		skill, ok := parseGatewaySkillFile(path, category)
		if !ok {
			return false
		}
		skills[skillStorageKey(skill.Category, skill.Name)] = skill
		return false
	})
	return skills
}

func (s *Server) loadGatewaySkillBody(name, category string) (string, bool) {
	normalizedName := sanitizeSkillName(name)
	if normalizedName == "" {
		return "", false
	}

	bodies := map[string]string{}
	for _, candidateCategory := range preferredSkillCategories(category) {
		for _, root := range s.gatewaySkillRoots() {
			content, ok := loadGatewaySkillBodyFromCategory(filepath.Join(root, candidateCategory), normalizedName)
			if ok {
				bodies[candidateCategory] = content
			}
		}
	}

	if normalizedCategory := normalizeSkillCategory(category); normalizedCategory != "" {
		body := strings.TrimSpace(bodies[normalizedCategory])
		return body, body != ""
	}

	for _, candidateCategory := range []string{skillCategoryPublic, skillCategoryCustom} {
		body := strings.TrimSpace(bodies[candidateCategory])
		if body != "" {
			return body, true
		}
	}
	return "", false
}

func preferredSkillCategories(category string) []string {
	if normalized := normalizeSkillCategory(category); normalized != "" {
		return []string{normalized}
	}
	return []string{skillCategoryCustom, skillCategoryPublic}
}

func loadGatewaySkillBodyFromCategory(root, skillName string) (string, bool) {
	var body string
	walkGatewaySkillFiles(root, func(path string) bool {
		skill, ok := parseGatewaySkillFile(path, "")
		if !ok || skill.Name != skillName {
			return false
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		body = stripSkillFrontmatter(string(content))
		return true
	})
	body = strings.TrimSpace(body)
	return body, body != ""
}

func walkGatewaySkillFiles(root string, visit func(path string) bool) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || visit == nil {
		return
	}

	visited := make(map[string]struct{})
	var walk func(dir string) bool
	walk = func(dir string) bool {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return false
		}

		realDir := dir
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			realDir = resolved
		}
		realDir = filepath.Clean(realDir)
		if _, ok := visited[realDir]; ok {
			return false
		}
		visited[realDir] = struct{}{}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

		for _, entry := range entries {
			name := entry.Name()
			path := filepath.Join(dir, name)
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			if info.IsDir() {
				if strings.HasPrefix(name, ".") || name == "__MACOSX" {
					continue
				}
				if walk(path) {
					return true
				}
				continue
			}
			if name == "SKILL.md" && visit(path) {
				return true
			}
		}
		return false
	}

	_ = walk(root)
}

func stripSkillFrontmatter(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return strings.TrimSpace(content)
	}
	rest := strings.TrimPrefix(content, "---\n")
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(rest[idx+len("\n---\n"):])
}

func parseGatewaySkillFile(path, category string) (gatewaySkill, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return gatewaySkill{}, false
	}
	metadata := parseSkillFrontmatter(string(content))
	name := firstNonEmpty(metadata["name"], filepath.Base(filepath.Dir(path)))
	name = sanitizeSkillName(name)
	description := strings.TrimSpace(metadata["description"])
	if name == "" || description == "" {
		return gatewaySkill{}, false
	}
	return gatewaySkill{
		Name:        name,
		Description: description,
		Category:    resolveSkillCategory(metadata["category"], category),
		License:     firstNonEmpty(strings.TrimSpace(metadata["license"]), "Unknown"),
		Enabled:     true,
	}, true
}

func mergeGatewaySkillState(discovered, persisted map[string]gatewaySkill) map[string]gatewaySkill {
	merged := make(map[string]gatewaySkill, len(discovered)+len(persisted))
	for key, skill := range discovered {
		merged[key] = skill
	}
	for key, skill := range persisted {
		if discoveredSkill, ok := merged[key]; ok {
			discoveredSkill.Enabled = skill.Enabled
			if discoveredSkill.Description == "" {
				discoveredSkill.Description = skill.Description
			}
			if discoveredSkill.License == "" {
				discoveredSkill.License = skill.License
			}
			merged[key] = discoveredSkill
			continue
		}
		merged[key] = skill
	}
	return merged
}

func uniqueCleanPaths(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}
