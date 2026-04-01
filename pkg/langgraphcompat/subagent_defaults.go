package langgraphcompat

import "github.com/axeprpr/deerflow-go/pkg/subagent"

const (
	defaultGatewayGeneralPurposeSubagentMaxTurns = 50
	defaultGatewayBashSubagentMaxTurns           = 30
)

func gatewayDefaultSubagentConfigs(appCfg subagentsAppConfig) map[subagent.SubagentType]subagent.SubagentConfig {
	return map[subagent.SubagentType]subagent.SubagentConfig{
		subagent.SubagentGeneralPurpose: {
			Type:            subagent.SubagentGeneralPurpose,
			MaxTurns:        defaultGatewayGeneralPurposeSubagentMaxTurns,
			Timeout:         appCfg.timeoutFor(subagent.SubagentGeneralPurpose),
			SystemPrompt:    generalPurposeSubagentPrompt,
			DisallowedTools: []string{"task", "ask_clarification", "present_file", "present_files"},
		},
		subagent.SubagentBash: {
			Type:            subagent.SubagentBash,
			MaxTurns:        defaultGatewayBashSubagentMaxTurns,
			Timeout:         appCfg.timeoutFor(subagent.SubagentBash),
			SystemPrompt:    bashSubagentPrompt,
			Tools:           []string{"bash", "ls", "read_file", "write_file", "str_replace"},
			DisallowedTools: []string{"task", "ask_clarification", "present_file", "present_files"},
		},
	}
}
