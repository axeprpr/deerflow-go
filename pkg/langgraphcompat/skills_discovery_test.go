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
