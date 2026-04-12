package agent

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

const defaultLoopWarnThreshold = 3
const defaultLoopHardLimit = 5
const defaultLoopWindowSize = 20

const loopWarningMessage = "[LOOP DETECTED] You are repeating the same tool calls. Stop calling tools and produce your final answer now. If you cannot complete the task, summarize what you accomplished so far."
const loopHardStopMessage = "[FORCED STOP] Repeated tool calls exceeded the safety limit. Producing final answer with results collected so far."

type RunPolicy struct {
	ToolTurns ToolTurnPolicy
	ToolExec  ToolExecutionPolicy
	Recovery  ToolTurnRecoveryPolicy
	Loop      LoopPolicy
	Retry     RetryPolicy
}

type ToolTurnPolicy interface {
	UseStructuredToolCalls(llm.LLMProvider, llm.ChatRequest) bool
}

type ToolExecutionPolicy interface {
	ShouldPauseAfterToolCall(models.ToolCall, models.ToolResult) bool
}

type ToolTurnRecoveryPolicy interface {
	Recover(context.Context, llm.LLMProvider, llm.ChatRequest, ToolTurnRecoveryState, error, func(AgentEvent)) (ToolTurnRecoveryResult, bool, error)
}

type LoopPolicy interface {
	Evaluate(*ToolLoopState, []models.ToolCall) LoopDecision
}

type RetryPolicy interface {
	RecoverableToolRetryPrompt([]models.Message) string
}

type LoopDecision struct {
	Warning  string
	HardStop bool
}

type ToolLoopState struct {
	History []string
	Warned  map[string]struct{}
}

type DefaultToolTurnPolicy struct{}
type DefaultToolExecutionPolicy struct{}
type DefaultToolTurnRecoveryPolicy struct{}

type DefaultLoopPolicy struct {
	WarnThreshold int
	HardLimit     int
	WindowSize    int
	WarningText   string
}

type DefaultRetryPolicy struct{}

type ToolTurnRecoveryState struct {
	MessageID           string
	PartialText         string
	HasPartialToolCalls bool
}

type ToolTurnRecoveryResult struct {
	Text       string
	ToolCalls  []models.ToolCall
	Usage      *llm.Usage
	StopReason string
}

func DefaultRunPolicy() *RunPolicy {
	return &RunPolicy{
		ToolTurns: DefaultToolTurnPolicy{},
		ToolExec:  DefaultToolExecutionPolicy{},
		Recovery:  DefaultToolTurnRecoveryPolicy{},
		Loop: DefaultLoopPolicy{
			WarnThreshold: defaultLoopWarnThreshold,
			HardLimit:     defaultLoopHardLimit,
			WindowSize:    defaultLoopWindowSize,
			WarningText:   loopWarningMessage,
		},
		Retry: DefaultRetryPolicy{},
	}
}

func newToolLoopState() *ToolLoopState {
	return &ToolLoopState{
		History: make([]string, 0, defaultLoopWindowSize),
		Warned:  make(map[string]struct{}),
	}
}

func (p DefaultToolTurnPolicy) UseStructuredToolCalls(provider llm.LLMProvider, _ llm.ChatRequest) bool {
	return prefersStructuredToolCalls(provider)
}

func (DefaultToolExecutionPolicy) ShouldPauseAfterToolCall(call models.ToolCall, result models.ToolResult) bool {
	switch strings.TrimSpace(call.Name) {
	case "ask_clarification":
		return result.Status != models.CallStatusFailed
	default:
		return false
	}
}

func (DefaultToolTurnRecoveryPolicy) Recover(
	ctx context.Context,
	provider llm.LLMProvider,
	req llm.ChatRequest,
	state ToolTurnRecoveryState,
	streamErr error,
	emit func(AgentEvent),
) (ToolTurnRecoveryResult, bool, error) {
	if len(req.Tools) == 0 || ctx.Err() != nil {
		return ToolTurnRecoveryResult{}, false, nil
	}
	if strings.TrimSpace(state.PartialText) == "" && !state.HasPartialToolCalls {
		return ToolTurnRecoveryResult{}, false, nil
	}
	if provider == nil {
		return ToolTurnRecoveryResult{}, false, nil
	}

	resp, err := provider.Chat(ctx, req)
	if err != nil {
		return ToolTurnRecoveryResult{}, false, fmt.Errorf("tool stream failed: %w (chat fallback failed: %v)", streamErr, err)
	}

	content := resp.Message.Content
	if content != "" {
		switch {
		case state.PartialText == "":
			emit(AgentEvent{Type: AgentEventChunk, MessageID: state.MessageID, Text: content})
			emit(AgentEvent{Type: AgentEventTextChunk, MessageID: state.MessageID, Text: content})
		case strings.HasPrefix(content, state.PartialText):
			suffix := strings.TrimPrefix(content, state.PartialText)
			if suffix != "" {
				emit(AgentEvent{Type: AgentEventChunk, MessageID: state.MessageID, Text: suffix})
				emit(AgentEvent{Type: AgentEventTextChunk, MessageID: state.MessageID, Text: suffix})
			}
		}
	}

	result := ToolTurnRecoveryResult{
		Text:       content,
		ToolCalls:  append([]models.ToolCall(nil), resp.Message.ToolCalls...),
		StopReason: resp.Stop,
	}
	if resp.Usage != (llm.Usage{}) {
		usage := resp.Usage
		result.Usage = &usage
	}
	return result, true, nil
}

func (p DefaultLoopPolicy) Evaluate(state *ToolLoopState, calls []models.ToolCall) LoopDecision {
	if state == nil {
		state = newToolLoopState()
	}
	callHash := hashToolCalls(calls)
	if callHash == "" {
		return LoopDecision{}
	}

	windowSize := p.WindowSize
	if windowSize <= 0 {
		windowSize = defaultLoopWindowSize
	}
	state.History = append(state.History, callHash)
	if len(state.History) > windowSize {
		state.History = state.History[len(state.History)-windowSize:]
	}

	count := 0
	for _, previous := range state.History {
		if previous == callHash {
			count++
		}
	}
	hardLimit := p.HardLimit
	if hardLimit <= 0 {
		hardLimit = defaultLoopHardLimit
	}
	if count >= hardLimit {
		return LoopDecision{HardStop: true}
	}
	warnThreshold := p.WarnThreshold
	if warnThreshold <= 0 {
		warnThreshold = defaultLoopWarnThreshold
	}
	if count >= warnThreshold {
		if _, ok := state.Warned[callHash]; !ok {
			state.Warned[callHash] = struct{}{}
			return LoopDecision{Warning: firstNonEmpty(p.WarningText, loopWarningMessage)}
		}
	}
	return LoopDecision{}
}

func (DefaultRetryPolicy) RecoverableToolRetryPrompt(messages []models.Message) string {
	return recoverableToolRetryPrompt(messages)
}

func resolveRunPolicy(policy *RunPolicy) *RunPolicy {
	if policy == nil {
		return DefaultRunPolicy()
	}
	if policy.ToolTurns == nil {
		policy.ToolTurns = DefaultToolTurnPolicy{}
	}
	if policy.ToolExec == nil {
		policy.ToolExec = DefaultToolExecutionPolicy{}
	}
	if policy.Recovery == nil {
		policy.Recovery = DefaultToolTurnRecoveryPolicy{}
	}
	if policy.Loop == nil {
		policy.Loop = DefaultLoopPolicy{
			WarnThreshold: defaultLoopWarnThreshold,
			HardLimit:     defaultLoopHardLimit,
			WindowSize:    defaultLoopWindowSize,
			WarningText:   loopWarningMessage,
		}
	}
	if policy.Retry == nil {
		policy.Retry = DefaultRetryPolicy{}
	}
	return policy
}

func hashToolCalls(calls []models.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	type normalizedToolCall struct {
		Name string         `json:"name"`
		Args map[string]any `json:"args,omitempty"`
	}
	normalized := make([]normalizedToolCall, 0, len(calls))
	for _, call := range calls {
		call, ok := models.NormalizeToolCall(call)
		if !ok {
			continue
		}
		normalized = append(normalized, normalizedToolCall{
			Name: call.Name,
			Args: call.Arguments,
		})
	}
	if len(normalized) == 0 {
		return ""
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Name != normalized[j].Name {
			return normalized[i].Name < normalized[j].Name
		}
		return marshalLoopArgs(normalized[i].Args) < marshalLoopArgs(normalized[j].Args)
	})
	raw, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	sum := md5.Sum(raw)
	return fmt.Sprintf("%x", sum[:6])
}

func marshalLoopArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return string(raw)
}
