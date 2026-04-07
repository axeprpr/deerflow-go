package langgraphcompat

import (
	"sort"
	"strings"
)

const (
	skillCategoryPublic = "public"
	skillCategoryCustom = "custom"
)

func normalizeSkillCategory(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case skillCategoryPublic:
		return skillCategoryPublic
	case skillCategoryCustom:
		return skillCategoryCustom
	default:
		return ""
	}
}

func resolveSkillCategory(category string, fallback string) string {
	if normalized := normalizeSkillCategory(category); normalized != "" {
		return normalized
	}
	if normalized := normalizeSkillCategory(fallback); normalized != "" {
		return normalized
	}
	return skillCategoryCustom
}

func inferSkillCategory(name string) string {
	name = sanitizeSkillName(name)
	if name == "" {
		return skillCategoryCustom
	}
	if category := bundledGatewaySkillCategory(name); category != "" {
		return category
	}
	return skillCategoryCustom
}

func skillStorageKey(category, name string) string {
	name = sanitizeSkillName(name)
	if name == "" {
		return ""
	}
	category = resolveSkillCategory(category, inferSkillCategory(name))
	return category + ":" + name
}

func splitSkillStorageKey(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if idx := strings.Index(raw, ":"); idx >= 0 {
		category := resolveSkillCategory(raw[:idx], "")
		name := sanitizeSkillName(raw[idx+1:])
		return category, name
	}
	return "", sanitizeSkillName(raw)
}

func mergeGatewaySkills(base, override map[string]gatewaySkill) map[string]gatewaySkill {
	out := make(map[string]gatewaySkill, len(base)+len(override))
	for key, skill := range base {
		if normalized, ok := normalizeGatewaySkill(skill); ok {
			out[skillStorageKey(normalized.Category, normalized.Name)] = normalized
		} else {
			out[key] = skill
		}
	}
	for key, skill := range override {
		if normalized, ok := normalizeGatewaySkill(skill); ok {
			out[skillStorageKey(normalized.Category, normalized.Name)] = normalized
		} else {
			out[key] = skill
		}
	}
	return out
}

func normalizeGatewaySkill(skill gatewaySkill) (gatewaySkill, bool) {
	skill.Name = sanitizeSkillName(skill.Name)
	if skill.Name == "" {
		return gatewaySkill{}, false
	}
	skill.Category = resolveSkillCategory(skill.Category, inferSkillCategory(skill.Name))
	if strings.TrimSpace(skill.License) == "" {
		skill.License = "Unknown"
	}
	return skill, true
}

func normalizePersistedSkills(skills map[string]gatewaySkill) map[string]gatewaySkill {
	if len(skills) == 0 {
		return map[string]gatewaySkill{}
	}
	out := make(map[string]gatewaySkill, len(skills))
	keys := make([]string, 0, len(skills))
	for key := range skills {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		skill := skills[key]
		if category, name := splitSkillStorageKey(key); name != "" {
			if strings.TrimSpace(skill.Name) == "" {
				skill.Name = name
			}
			if strings.TrimSpace(skill.Category) == "" {
				skill.Category = category
			}
		}
		normalized, ok := normalizeGatewaySkill(skill)
		if !ok {
			continue
		}
		out[skillStorageKey(normalized.Category, normalized.Name)] = normalized
	}
	return out
}

func findGatewaySkill(skills map[string]gatewaySkill, name, category string) (gatewaySkill, bool) {
	name = sanitizeSkillName(name)
	if name == "" {
		return gatewaySkill{}, false
	}
	if normalizedCategory := normalizeSkillCategory(category); normalizedCategory != "" {
		skill, ok := skills[skillStorageKey(normalizedCategory, name)]
		return skill, ok
	}
	for _, candidateCategory := range []string{skillCategoryCustom, skillCategoryPublic} {
		if skill, ok := skills[skillStorageKey(candidateCategory, name)]; ok {
			return skill, true
		}
	}
	for _, skill := range skills {
		if skill.Name == name {
			return skill, true
		}
	}
	return gatewaySkill{}, false
}

func defaultGatewaySkills() map[string]gatewaySkill {
	return map[string]gatewaySkill{
		skillCategoryPublic + ":deep-research": {
			Name:        "deep-research",
			Description: "Research and summarize a topic with structured outputs.",
			Category:    skillCategoryPublic,
			License:     "MIT",
			Enabled:     true,
		},
	}
}

func bundledGatewaySkillCategory(name string) string {
	switch sanitizeSkillName(name) {
	case "deep-research":
		return skillCategoryPublic
	default:
		return ""
	}
}
