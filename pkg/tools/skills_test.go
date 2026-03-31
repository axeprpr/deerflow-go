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
