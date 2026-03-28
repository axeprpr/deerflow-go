package agent

import (
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

// Usage tracks token counts for a run.
type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

// RunResult is the normalized outcome of an agent run.
type RunResult struct {
	Messages    []models.Message `json:"messages"`
	FinalOutput string           `json:"final_output"`
	Usage       *Usage           `json:"usage,omitempty"`
}

// AgentConfig holds the dependencies required to construct an agent.
type AgentConfig struct {
	LLMProvider llm.LLMProvider
	Tools       *tools.Registry
	MaxTurns    int
	Model       string
	Sandbox     *sandbox.Sandbox
}

type AgentEventType string

const (
	AgentEventTextChunk  AgentEventType = "text_chunk"
	AgentEventToolCall   AgentEventType = "tool_call"
	AgentEventToolResult AgentEventType = "tool_result"
	AgentEventEnd        AgentEventType = "end"
	AgentEventError      AgentEventType = "error"
)

// AgentEvent is emitted while the agent is running.
type AgentEvent struct {
	Type      AgentEventType     `json:"type"`
	SessionID string             `json:"session_id,omitempty"`
	Text      string             `json:"text,omitempty"`
	ToolCall  *models.ToolCall   `json:"tool_call,omitempty"`
	Result    *models.ToolResult `json:"result,omitempty"`
	Usage     *Usage             `json:"usage,omitempty"`
	Err       string             `json:"error,omitempty"`
}
