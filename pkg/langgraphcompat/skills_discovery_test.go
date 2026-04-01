package langgraphcompat

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillsDiscoveryKeepsDefaultPublicSkillsWhenDiskSkillsExist(t *testing.T) {
	s, handler := newCompatTestServer(t)

	customDir := filepath.Join(s.dataRoot, "skills", "custom", "team", "release-helper")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", customDir, err)
	}

	if err := os.WriteFile(filepath.Join(customDir, "SKILL.md"), []byte(`---
name: release-helper
description: Prepare release checklists.
license: MIT
---
# Release Helper
`), 0o644); err != nil {
		t.Fatalf("write custom skill: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/skills", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Skills []gatewaySkill `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	found := map[string]gatewaySkill{}
	for _, skill := range payload.Skills {
		found[skillStorageKey(skill.Category, skill.Name)] = skill
	}

	if _, ok := found[skillStorageKey(skillCategoryCustom, "release-helper")]; !ok {
		t.Fatalf("missing discovered custom skill: %#v", payload.Skills)
	}

	defaultSkill, ok := found[skillStorageKey(skillCategoryPublic, "deep-research")]
	if !ok {
		t.Fatalf("missing default public skill after discovery merge: %#v", payload.Skills)
	}
	if !defaultSkill.Enabled {
		t.Fatal("expected default public skill to remain enabled")
	}
}

func TestGatewaySkillRootsDiscoversSiblingDeerflowUISkills(t *testing.T) {
	projectRoot := t.TempDir()
	uiSkillDir := filepath.Join(projectRoot, "..", "deerflow-ui", "skills", "public", "skill-creator")
	if err := os.MkdirAll(uiSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir sibling skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uiSkillDir, "SKILL.md"), []byte(`---
name: skill-creator
description: Create and refine skills.
license: MIT
---
# Skill Creator

Ask focused questions before drafting the skill.
`), 0o644); err != nil {
		t.Fatalf("write sibling skill: %v", err)
	}

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

	s, _ := newCompatTestServer(t)
	skills := s.currentGatewaySkills()

	skill, ok := skills[skillStorageKey(skillCategoryPublic, "skill-creator")]
	if !ok {
		t.Fatalf("expected sibling deerflow-ui skill discovery, got %#v", skills)
	}
	if skill.Description != "Create and refine skills." {
		t.Fatalf("description=%q want=%q", skill.Description, "Create and refine skills.")
	}

	body, ok := s.loadGatewaySkillBody("skill-creator", skillCategoryPublic)
	if !ok {
		t.Fatal("expected to load sibling skill body")
	}
	if got, want := body, "# Skill Creator\n\nAsk focused questions before drafting the skill."; got != want {
		t.Fatalf("skill body=%q want=%q", got, want)
	}
}

func TestGatewaySkillRootsDiscoversWorkspaceSkills(t *testing.T) {
	projectRoot := t.TempDir()
	workspaceSkillDir := filepath.Join(projectRoot, "skills", "public", "workspace-skill")
	if err := os.MkdirAll(workspaceSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir workspace skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceSkillDir, "SKILL.md"), []byte(`---
name: workspace-skill
description: Loaded from the current workspace skills directory.
license: MIT
---
# Workspace Skill

This body comes from the repo-local skills directory.
`), 0o644); err != nil {
		t.Fatalf("write workspace skill: %v", err)
	}

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

	s, _ := newCompatTestServer(t)
	skills := s.currentGatewaySkills()

	skill, ok := skills[skillStorageKey(skillCategoryPublic, "workspace-skill")]
	if !ok {
		t.Fatalf("expected workspace skill discovery, got %#v", skills)
	}
	if skill.Description != "Loaded from the current workspace skills directory." {
		t.Fatalf("description=%q want=%q", skill.Description, "Loaded from the current workspace skills directory.")
	}

	body, ok := s.loadGatewaySkillBody("workspace-skill", skillCategoryPublic)
	if !ok {
		t.Fatal("expected to load workspace skill body")
	}
	if got, want := body, "# Workspace Skill\n\nThis body comes from the repo-local skills directory."; got != want {
		t.Fatalf("skill body=%q want=%q", got, want)
	}
}

func TestSkillsDiscoveryFollowsSymlinkedDirectories(t *testing.T) {
	s, _ := newCompatTestServer(t)

	sourceDir := filepath.Join(t.TempDir(), "shared-skill")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte(`---
name: linked-skill
description: Loaded through a symlinked directory.
license: MIT
---
# Linked Skill

This body comes from a symlink target.
`), 0o644); err != nil {
		t.Fatalf("write source skill: %v", err)
	}

	linkRoot := filepath.Join(s.dataRoot, "skills", "custom", "linked")
	if err := os.MkdirAll(filepath.Dir(linkRoot), 0o755); err != nil {
		t.Fatalf("mkdir link parent: %v", err)
	}
	if err := os.Symlink(sourceDir, linkRoot); err != nil {
		t.Fatalf("symlink source dir: %v", err)
	}

	skills := s.currentGatewaySkills()
	skill, ok := skills[skillStorageKey(skillCategoryCustom, "linked-skill")]
	if !ok {
		t.Fatalf("expected symlinked skill discovery, got %#v", skills)
	}
	if skill.Description != "Loaded through a symlinked directory." {
		t.Fatalf("description=%q", skill.Description)
	}

	body, ok := s.loadGatewaySkillBody("linked-skill", skillCategoryCustom)
	if !ok {
		t.Fatal("expected to load symlinked skill body")
	}
	if got, want := body, "# Linked Skill\n\nThis body comes from a symlink target."; got != want {
		t.Fatalf("skill body=%q want=%q", got, want)
	}
}

func TestSkillsDiscoveryIgnoresSymlinkCycles(t *testing.T) {
	s, _ := newCompatTestServer(t)

	cycleRoot := filepath.Join(s.dataRoot, "skills", "custom", "cycle")
	if err := os.MkdirAll(cycleRoot, 0o755); err != nil {
		t.Fatalf("mkdir cycle root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cycleRoot, "SKILL.md"), []byte(`---
name: cycle-skill
description: Skill inside a cyclic directory graph.
license: MIT
---
# Cycle Skill
`), 0o644); err != nil {
		t.Fatalf("write cycle skill: %v", err)
	}
	if err := os.Symlink(cycleRoot, filepath.Join(cycleRoot, "loop")); err != nil {
		t.Fatalf("symlink cycle dir: %v", err)
	}

	skills := s.currentGatewaySkills()
	if _, ok := skills[skillStorageKey(skillCategoryCustom, "cycle-skill")]; !ok {
		t.Fatalf("expected cyclic symlink skill discovery, got %#v", skills)
	}
}
