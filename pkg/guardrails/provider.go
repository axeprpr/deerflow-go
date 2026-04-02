package guardrails

import "time"

// Request captures the context evaluated before a tool executes.
type Request struct {
	ToolName   string
	ToolInput  map[string]any
	AgentID    string
	ThreadID   string
	IsSubagent bool
	Timestamp  time.Time
}

// Reason is a structured explanation for a guardrail decision.
type Reason struct {
	Code    string
	Message string
}

// Decision is the allow/deny verdict returned by a provider.
type Decision struct {
	Allow    bool
	Reasons  []Reason
	PolicyID string
	Metadata map[string]any
}

// Provider authorizes or blocks tool calls before execution.
type Provider interface {
	Name() string
	Evaluate(Request) (Decision, error)
}
