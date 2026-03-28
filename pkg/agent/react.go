package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

const defaultMaxTurns = 8

var messageSeq uint64

// Agent runs a ReAct-style loop over an LLM and tool registry.
type Agent struct {
	llm      llm.LLMProvider
	tools    *tools.Registry
	sandbox  *sandbox.Sandbox
	model    string
	maxTurns int
	events   chan AgentEvent
}

func New(cfg AgentConfig) *Agent {
	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}
	registry := cfg.Tools
	if registry == nil {
		registry = tools.NewRegistry()
	}
	sb := cfg.Sandbox
	return &Agent{
		llm:      cfg.LLMProvider,
		tools:    registry,
		sandbox:  sb,
		model:    resolveModel(cfg.Model),
		maxTurns: maxTurns,
		events:   make(chan AgentEvent, 128),
	}
}

func (a *Agent) Events() <-chan AgentEvent {
	return a.events
}

func (a *Agent) Run(ctx context.Context, sessionID string, messages []models.Message) (*RunResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a == nil || a.llm == nil {
		return nil, errors.New("agent llm provider is required")
	}

	runMessages := append([]models.Message(nil), messages...)
	usage := &Usage{}
	var finalOutput string

	defer close(a.events)

	for turn := 0; turn < a.maxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			a.emit(AgentEvent{Type: AgentEventError, SessionID: sessionID, Err: err.Error()})
			return nil, err
		}

		req := llm.ChatRequest{
			Model:        a.model,
			Messages:     runMessages,
			Tools:        a.tools.List(),
			SystemPrompt: a.BuildSystemPrompt(ctx, sessionID),
		}

		stream, err := a.llm.Stream(ctx, req)
		if err != nil {
			a.emit(AgentEvent{Type: AgentEventError, SessionID: sessionID, Err: err.Error()})
			return nil, err
		}

		var textBuilder strings.Builder
		var toolCalls []models.ToolCall
		var streamUsage *llm.Usage
		var stopReason string

		for chunk := range stream {
			if chunk.Err != nil {
				a.emit(AgentEvent{Type: AgentEventError, SessionID: sessionID, Err: chunk.Err.Error()})
				return nil, chunk.Err
			}
			if chunk.Delta != "" {
				textBuilder.WriteString(chunk.Delta)
				a.emit(AgentEvent{
					Type:      AgentEventTextChunk,
					SessionID: sessionID,
					Text:      chunk.Delta,
				})
			}
			if len(chunk.ToolCalls) > 0 {
				toolCalls = mergeToolCalls(toolCalls, chunk.ToolCalls)
			}
			if chunk.Usage != nil {
				streamUsage = chunk.Usage
			}
			if chunk.Done {
				stopReason = chunk.Stop
				if chunk.Message != nil {
					if textBuilder.Len() == 0 && chunk.Message.Content != "" {
						textBuilder.WriteString(chunk.Message.Content)
					}
					if len(toolCalls) == 0 && len(chunk.Message.ToolCalls) > 0 {
						toolCalls = append(toolCalls, chunk.Message.ToolCalls...)
					}
				}
			}
		}

		if streamUsage != nil {
			accumulateUsage(usage, streamUsage)
		}

		assistantMessage := models.Message{
			ID:        newMessageID("ai"),
			SessionID: sessionID,
			Role:      models.RoleAI,
			Content:   textBuilder.String(),
			ToolCalls: toolCalls,
			Metadata:  map[string]string{"stop_reason": stopReason},
			CreatedAt: time.Now().UTC(),
		}
		if assistantMessage.Content != "" || len(assistantMessage.ToolCalls) > 0 {
			runMessages = append(runMessages, assistantMessage)
		}

		if len(toolCalls) == 0 {
			finalOutput = assistantMessage.Content
			a.emit(AgentEvent{
				Type:      AgentEventEnd,
				SessionID: sessionID,
				Text:      finalOutput,
				Usage:     cloneUsage(usage),
			})
			return &RunResult{
				Messages:    runMessages,
				FinalOutput: finalOutput,
				Usage:       usage,
			}, nil
		}

		for _, call := range toolCalls {
			a.emit(AgentEvent{Type: AgentEventToolCall, SessionID: sessionID, ToolCall: &call})

			result, err := a.tools.Execute(tools.WithSandbox(ctx, a.sandbox), call)
			if err != nil {
				result = models.ToolResult{
					CallID:      call.ID,
					ToolName:    call.Name,
					Status:      models.CallStatusFailed,
					Error:       err.Error(),
					CompletedAt: time.Now().UTC(),
				}
			}

			resultMessage := models.Message{
				ID:         newMessageID("tool"),
				SessionID:  sessionID,
				Role:       models.RoleTool,
				Content:    result.Content,
				ToolResult: &result,
				CreatedAt:  time.Now().UTC(),
			}
			runMessages = append(runMessages, resultMessage)
			a.emit(AgentEvent{Type: AgentEventToolResult, SessionID: sessionID, Result: &result})

			if err := ctx.Err(); err != nil {
				a.emit(AgentEvent{Type: AgentEventError, SessionID: sessionID, Err: err.Error()})
				return nil, err
			}
		}
	}

	err := fmt.Errorf("agent exceeded max turns (%d)", a.maxTurns)
	a.emit(AgentEvent{Type: AgentEventError, SessionID: sessionID, Err: err.Error()})
	return nil, err
}

func (a *Agent) BuildSystemPrompt(ctx context.Context, sessionID string) string {
	var sections []string
	sections = append(sections,
		"You are a ReAct-style agent. Think step by step, call tools when necessary, and stop when you have a complete answer.",
	)

	toolDescriptions := a.tools.Descriptions()
	if strings.TrimSpace(toolDescriptions) != "" {
		sections = append(sections, "Available Tools:\n"+toolDescriptions)
	}

	return strings.Join(sections, "\n\n")
}

func (a *Agent) emit(evt AgentEvent) {
	select {
	case a.events <- evt:
	default:
	}
}

func resolveModel(model string) string {
	if model = strings.TrimSpace(model); model != "" {
		return model
	}
	if model := strings.TrimSpace(os.Getenv("DEFAULT_LLM_MODEL")); model != "" {
		return model
	}
	return "gpt-4.1-mini"
}

func newMessageID(prefix string) string {
	seq := atomic.AddUint64(&messageSeq, 1)
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UTC().UnixNano(), seq)
}

func mergeToolCalls(existing, incoming []models.ToolCall) []models.ToolCall {
	if len(existing) == 0 {
		return append([]models.ToolCall(nil), incoming...)
	}

	indexByID := make(map[string]int, len(existing))
	for i, call := range existing {
		indexByID[call.ID] = i
	}

	for _, call := range incoming {
		if idx, ok := indexByID[call.ID]; ok {
			if existing[idx].Name == "" {
				existing[idx].Name = call.Name
			}
			if len(call.Arguments) > 0 {
				existing[idx].Arguments = call.Arguments
			}
			if call.Status != "" {
				existing[idx].Status = call.Status
			}
			continue
		}
		indexByID[call.ID] = len(existing)
		existing = append(existing, call)
	}

	return existing
}

func accumulateUsage(dst *Usage, src *llm.Usage) {
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.TotalTokens += src.TotalTokens
}

func cloneUsage(src *Usage) *Usage {
	if src == nil {
		return nil
	}
	out := *src
	return &out
}

func formatToolSchema(schema map[string]any) string {
	if len(schema) == 0 {
		return ""
	}
	raw, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return ""
	}
	return string(raw)
}
