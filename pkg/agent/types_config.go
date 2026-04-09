package agent

import (
	"fmt"
	"strings"
)

type AgentType string

const (
	AgentTypeGeneral  AgentType = "general-purpose"
	AgentTypeResearch AgentType = "researcher"
	AgentTypeCoder    AgentType = "coder"
	AgentTypeAnalyst  AgentType = "analyst"
)

type AgentTypeConfig struct {
	Type         AgentType `json:"type"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	SystemPrompt string    `json:"system_prompt"`
	DefaultTools []string  `json:"default_tools,omitempty"`
	MaxTurns     int       `json:"max_turns"`
	Temperature  float64   `json:"temperature"`
}

const (
	generalPurposeSystemPrompt = "<role>\nYou are an open-source super agent.\n</role>"
	researcherSystemPrompt     = "<role>\nYou are an open-source research agent.\n</role>"
	coderSystemPrompt          = "<role>\nYou are an open-source coding agent.\n</role>"
	analystSystemPrompt        = "<role>\nYou are an open-source analysis agent.\n</role>"
)

var BuiltinAgentTypes = map[AgentType]AgentTypeConfig{
	AgentTypeGeneral: {
		Type:         AgentTypeGeneral,
		Name:         "General Purpose",
		Description:  "Balanced assistant profile for general tasks.",
		SystemPrompt: generalPurposeSystemPrompt,
		DefaultTools: nil,
		MaxTurns:     defaultMaxTurns,
		Temperature:  0.2,
	},
	AgentTypeResearch: {
		Type:         AgentTypeResearch,
		Name:         "Researcher",
		Description:  "Profile for research, reading, and synthesis tasks.",
		SystemPrompt: researcherSystemPrompt,
		DefaultTools: []string{"web_search", "web_fetch", "image_search", "ls", "read_file", "present_files", "ask_clarification", "task"},
		MaxTurns:     10,
		Temperature:  0.1,
	},
	AgentTypeCoder: {
		Type:         AgentTypeCoder,
		Name:         "Coder",
		Description:  "Profile for code generation, debugging, and implementation tasks.",
		SystemPrompt: coderSystemPrompt,
		DefaultTools: []string{"bash", "ls", "read_file", "write_file", "str_replace", "present_files", "ask_clarification", "task"},
		MaxTurns:     12,
		Temperature:  0.1,
	},
	AgentTypeAnalyst: {
		Type:         AgentTypeAnalyst,
		Name:         "Analyst",
		Description:  "Profile for structured analysis and artifact generation.",
		SystemPrompt: analystSystemPrompt,
		DefaultTools: []string{"ls", "read_file", "write_file", "str_replace", "present_files", "ask_clarification"},
		MaxTurns:     10,
		Temperature:  0.15,
	},
}

func GetAgentTypeConfig(t AgentType) AgentTypeConfig {
	t = normalizeAgentType(t)
	if cfg, ok := BuiltinAgentTypes[t]; ok {
		return cfg
	}
	return BuiltinAgentTypes[AgentTypeGeneral]
}

func ApplyAgentType(cfg *AgentConfig, t AgentType) error {
	if cfg == nil {
		return fmt.Errorf("agent config is nil")
	}

	t = normalizeAgentType(t)
	if t == "" {
		t = AgentTypeGeneral
	}
	if _, ok := BuiltinAgentTypes[t]; !ok {
		return fmt.Errorf("unsupported agent type %q", t)
	}

	profile := GetAgentTypeConfig(t)
	cfg.AgentType = profile.Type
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = profile.MaxTurns
	}
	if strings.TrimSpace(cfg.SystemPrompt) == "" {
		cfg.SystemPrompt = profile.SystemPrompt
	}
	if cfg.Temperature == nil {
		temp := profile.Temperature
		cfg.Temperature = &temp
	}
	if cfg.Tools != nil && len(profile.DefaultTools) > 0 {
		cfg.Tools = cfg.Tools.Restrict(profile.DefaultTools)
	}
	return nil
}

func normalizeAgentType(t AgentType) AgentType {
	return AgentType(strings.TrimSpace(string(t)))
}
