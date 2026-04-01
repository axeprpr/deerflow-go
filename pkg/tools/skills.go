package tools

import (
	"os"
	"path/filepath"
	"strings"
)

const skillsVirtualPath = "/mnt/skills"

var skillsExecutablePath = os.Executable

func resolveSkillsVirtualPath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	if path != skillsVirtualPath && !strings.HasPrefix(path, skillsVirtualPath+"/") {
		return "", false
	}

	relative := strings.TrimPrefix(path, skillsVirtualPath)
	relative = strings.TrimPrefix(relative, "/")
	cleanRelative := filepath.Clean(filepath.FromSlash(relative))
	if cleanRelative == "." {
		cleanRelative = ""
	}
	if cleanRelative == ".." || strings.HasPrefix(cleanRelative, ".."+string(filepath.Separator)) {
		return "", false
	}

	roots := skillRoots()
	if len(roots) == 0 {
		return "", false
	}

	candidates := make([]string, 0, len(roots))
	for _, root := range roots {
		candidate := filepath.Join(root, cleanRelative)
		candidates = append(candidates, candidate)
		if skillPathExists(candidate) {
			return candidate, true
		}
	}

	return candidates[0], true
}

func skillRoots() []string {
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
		roots = append(roots, executableRelativeSkillRoots(skillsExecutablePath)...)
		if cwd, err := os.Getwd(); err == nil {
			roots = append(roots, filepath.Join(cwd, "skills"))
			roots = append(roots, filepath.Join(cwd, "..", "deerflow-ui", "skills"))
			roots = append(roots, filepath.Join(cwd, "..", "..", "deerflow-ui", "skills"))
		}
	}

	dataRoot := strings.TrimSpace(os.Getenv("DEERFLOW_DATA_ROOT"))
	if dataRoot == "" {
		dataRoot = filepath.Join(os.TempDir(), "deerflow-go-data")
	}
	roots = append(roots, filepath.Join(dataRoot, "skills"))

	seen := make(map[string]struct{}, len(roots))
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		clean := filepath.Clean(root)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
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

func skillPathExists(path string) bool {
	if strings.ContainsAny(path, "*?[") {
		parent := filepath.Dir(path)
		info, err := os.Stat(parent)
		return err == nil && info.IsDir()
	}
	_, err := os.Stat(path)
	return err == nil
}
