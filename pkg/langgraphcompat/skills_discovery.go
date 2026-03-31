package langgraphcompat

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Server) currentGatewaySkills() map[string]gatewaySkill {
	discovered := mergeGatewaySkills(defaultGatewaySkills(), discoverGatewaySkills(s.gatewaySkillRoots()))

	s.uiStateMu.RLock()
	persisted := normalizePersistedSkills(s.skills)
	s.uiStateMu.RUnlock()

	return mergeGatewaySkillState(discovered, persisted)
}

func (s *Server) gatewaySkillRoots() []string {
	roots := make([]string, 0, 3)
	for _, raw := range filepath.SplitList(strings.TrimSpace(os.Getenv("DEERFLOW_SKILLS_ROOT"))) {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			roots = append(roots, trimmed)
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, filepath.Join(cwd, "skills"))
	}
	roots = append(roots, filepath.Join(s.dataRoot, "skills"))
	return uniqueCleanPaths(roots)
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
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "__MACOSX") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "SKILL.md" {
			return nil
		}
		skill, ok := parseGatewaySkillFile(path, category)
		if !ok {
			return nil
		}
		skills[skillStorageKey(skill.Category, skill.Name)] = skill
		return nil
	})
	return skills
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
	sort.Strings(out)
	return out
}
