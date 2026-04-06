package langgraphcompat

import (
	"testing"
)

func TestLoadACPAgentsFromEnvSupportsCamelCaseModelAlias(t *testing.T) {
	t.Setenv("DEERFLOW_ACP_AGENTS_JSON", `{
		"codex": {
			"command": "npx",
			"args": ["-y", "@zed-industries/codex-acp"],
			"description": "Codex ACP",
			"modelName": "gpt-5"
		}
	}`)

	cfg := loadACPAgentsFromEnv()
	agent, ok := cfg["codex"]
	if !ok {
		t.Fatal("expected codex ACP agent")
	}
	if agent.Model != "gpt-5" {
		t.Fatalf("Model=%q want=gpt-5", agent.Model)
	}
}
