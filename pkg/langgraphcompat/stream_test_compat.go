package langgraphcompat

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
)

func usagePayloadFromAgentUsage(usage *agent.Usage) map[string]int {
	out := map[string]int{
		"input_tokens":        0,
		"output_tokens":       0,
		"total_tokens":        0,
		"reasoning_tokens":    0,
		"cached_input_tokens": 0,
	}
	if usage == nil {
		return out
	}
	out["input_tokens"] = usage.InputTokens
	out["output_tokens"] = usage.OutputTokens
	out["total_tokens"] = usage.TotalTokens
	out["reasoning_tokens"] = usage.ReasoningTokens
	out["cached_input_tokens"] = usage.CachedInputTokens
	return out
}

func toolMayAffectArtifacts(name string) bool {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "bash", "write_file", "str_replace", "task", "invoke_acp_agent":
		return true
	default:
		return false
	}
}

func resolvedToolNameForArtifacts(evt agent.AgentEvent) string {
	if evt.Result != nil {
		if name := strings.TrimSpace(evt.Result.ToolName); name != "" {
			return name
		}
	}
	if evt.ToolEvent != nil {
		return strings.TrimSpace(evt.ToolEvent.Name)
	}
	return ""
}

func threadMetadataFromRuntimeContext(runtimeContext map[string]any, cfg runConfig) map[string]any {
	out := map[string]any{}
	if runtimeContext != nil {
		if agentName := stringFromAny(runtimeContext["agent_name"]); agentName != "" {
			out["agent_name"] = agentName
		}
	}
	if out["agent_name"] == nil && strings.TrimSpace(cfg.AgentName) != "" {
		out["agent_name"] = cfg.AgentName
	}
	if strings.TrimSpace(string(cfg.AgentType)) != "" {
		out["agent_type"] = string(cfg.AgentType)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func gatewayMemoryFromDocument(doc pkgmemory.Document) gatewayMemoryResponse {
	return gatewayMemoryResponseFromDocument(doc)
}
