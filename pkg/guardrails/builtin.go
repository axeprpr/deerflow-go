package guardrails

import "strings"

// AllowlistProvider implements a simple allowlist/denylist policy.
type AllowlistProvider struct {
	allowed map[string]struct{}
	denied  map[string]struct{}
}

func NewAllowlistProvider(allowedTools, deniedTools []string) *AllowlistProvider {
	provider := &AllowlistProvider{
		denied: make(map[string]struct{}),
	}
	if len(allowedTools) > 0 {
		provider.allowed = make(map[string]struct{}, len(allowedTools))
		for _, tool := range allowedTools {
			if name := normalizeToolName(tool); name != "" {
				provider.allowed[name] = struct{}{}
			}
		}
		if len(provider.allowed) == 0 {
			provider.allowed = nil
		}
	}
	for _, tool := range deniedTools {
		if name := normalizeToolName(tool); name != "" {
			provider.denied[name] = struct{}{}
		}
	}
	return provider
}

func (p *AllowlistProvider) Name() string {
	return "allowlist"
}

func (p *AllowlistProvider) Evaluate(req Request) (Decision, error) {
	toolName := normalizeToolName(req.ToolName)
	if toolName == "" {
		return Decision{Allow: true, Reasons: []Reason{{Code: "oap.allowed"}}}, nil
	}
	if p.allowed != nil {
		if _, ok := p.allowed[toolName]; !ok {
			return Decision{
				Allow: false,
				Reasons: []Reason{{
					Code:    "oap.tool_not_allowed",
					Message: "tool '" + toolName + "' not in allowlist",
				}},
			}, nil
		}
	}
	if _, ok := p.denied[toolName]; ok {
		return Decision{
			Allow: false,
			Reasons: []Reason{{
				Code:    "oap.tool_not_allowed",
				Message: "tool '" + toolName + "' is denied",
			}},
		}, nil
	}
	return Decision{Allow: true, Reasons: []Reason{{Code: "oap.allowed"}}}, nil
}

func normalizeToolName(name string) string {
	return strings.TrimSpace(name)
}
