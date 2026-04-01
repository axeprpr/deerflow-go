package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveVirtualPathForSkillFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	target := filepath.Join(root, "public", "demo-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("# Demo"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	t.Setenv("DEERFLOW_SKILLS_ROOT", root)

	got := ResolveVirtualPath(context.Background(), "/mnt/skills/public/demo-skill/SKILL.md")
	if got != target {
		t.Fatalf("ResolveVirtualPath() = %q, want %q", got, target)
	}
}

func TestResolveVirtualPathForSkillGlob(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	targetDir := filepath.Join(root, "public", "demo-skill", "references")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("DEERFLOW_SKILLS_ROOT", root)

	got := ResolveVirtualPath(context.Background(), "/mnt/skills/public/demo-skill/references/*.md")
	want := filepath.Join(targetDir, "*.md")
	if got != want {
		t.Fatalf("ResolveVirtualPath() = %q, want %q", got, want)
	}
}

func TestResolveVirtualPathForExecutableRelativeSkillRoot(t *testing.T) {
	installRoot := t.TempDir()
	target := filepath.Join(installRoot, "skills", "public", "demo-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("# Demo"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	restore := skillsExecutablePath
	skillsExecutablePath = func() (string, error) {
		return filepath.Join(installRoot, "bin", "deerflow-go"), nil
	}
	defer func() {
		skillsExecutablePath = restore
	}()

	projectRoot := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	got := ResolveVirtualPath(context.Background(), "/mnt/skills/public/demo-skill/SKILL.md")
	if got != target {
		t.Fatalf("ResolveVirtualPath() = %q, want %q", got, target)
	}
}
