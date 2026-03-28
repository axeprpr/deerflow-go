package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"github.com/cloudwego/eino/adk"
	einoSchema "github.com/cloudwego/eino/schema"
)

const defaultMaxTurns = 8

var messageSeq uint64

// Agent runs our custom ReAct loop while delegating model streaming and tool schemas to Eino.
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
	return &Agent{
		llm:      cfg.LLMProvider,
		tools:    registry,
		sandbox:  cfg.Sandbox,
		model:    resolveModel(cfg.Model),
		maxTurns: maxTurns,
		events:   make(chan AgentEvent, 128),
	}
}

func (a *Agent) Events() <-chan AgentEvent {
	return a.events
}

func (a *Agent) EinoAgent() adk.Agent {
	return &einoAgentAdapter{agent: a}
}

func (a *Agent) Run(ctx context.Context, sessionID string, messages []models.Message) (*RunResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a == nil || a.llm == nil {
		return nil, fmt.Errorf("agent llm provider is required")
	}

	runMessages := append([]models.Message(nil), messages...)
	usage := &Usage{}

	defer close(a.events)

	for turn := 0; turn < a.maxTurns; turn++ {
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

		var (
			textBuilder strings.Builder
			toolCalls   []models.ToolCall
			streamUsage *llm.Usage
			stopReason  string
		)

		for chunk := range stream {
			if chunk.Err != nil {
				a.emit(AgentEvent{Type: AgentEventError, SessionID: sessionID, Err: chunk.Err.Error()})
				return nil, chunk.Err
			}
			if chunk.Delta != "" {
				textBuilder.WriteString(chunk.Delta)
				a.emit(AgentEvent{Type: AgentEventTextChunk, SessionID: sessionID, Text: chunk.Delta})
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
			a.emit(AgentEvent{
				Type:      AgentEventEnd,
				SessionID: sessionID,
				Text:      assistantMessage.Content,
				Usage:     cloneUsage(usage),
			})
			return &RunResult{
				Messages:    runMessages,
				FinalOutput: assistantMessage.Content,
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

			runMessages = append(runMessages, models.Message{
				ID:         newMessageID("tool"),
				SessionID:  sessionID,
				Role:       models.RoleTool,
				Content:    toolMessageContent(result),
				ToolResult: &result,
				CreatedAt:  time.Now().UTC(),
			})
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

func (a *Agent) BuildSystemPrompt(_ context.Context, _ string) string {
	sections := []string{
		"You are a ReAct-style agent. Think step by step, call tools when necessary, and stop when you have a complete answer.",
	}
	if toolDescriptions := a.tools.Descriptions(); strings.TrimSpace(toolDescriptions) != "" {
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

func toolMessageContent(result models.ToolResult) string {
	if result.Error != "" {
		return result.Error
	}
	return result.Content
}

type einoAgentAdapter struct {
	agent *Agent
}

func (a *einoAgentAdapter) Name(context.Context) string {
	return "react"
}

func (a *einoAgentAdapter) Description(context.Context) string {
	return "Custom ReAct agent that uses Eino chat-model and tool-calling primitives."
}

func (a *einoAgentAdapter) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		sessionID := fmt.Sprintf("adk-%d", time.Now().UTC().UnixNano())
		messages := make([]models.Message, 0, len(input.Messages))
		for i, msg := range input.Messages {
			if msg == nil {
				continue
			}
			messages = append(messages, models.Message{
				ID:        fmt.Sprintf("adk_%d", i),
				SessionID: sessionID,
				Role:      fromEinoRole(msg.Role),
				Content:   msg.Content,
				CreatedAt: time.Now().UTC(),
			})
		}

		result, err := a.agent.Run(ctx, sessionID, messages)
		if err != nil {
			gen.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: err})
			return
		}

		gen.Send(adk.EventFromMessage(&einoSchema.Message{
			Role:    einoSchema.Assistant,
			Content: result.FinalOutput,
		}, nil, einoSchema.Assistant, ""))
	}()

	return iter
}

func fromEinoRole(role einoSchema.RoleType) models.Role {
	switch role {
	case einoSchema.User:
		return models.RoleHuman
	case einoSchema.System:
		return models.RoleSystem
	case einoSchema.Tool:
		return models.RoleTool
	default:
		return models.RoleAI
	}
}
