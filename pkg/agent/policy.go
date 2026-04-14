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
const defaultTodoReminderThreshold = 3

const loopWarningMessage = "[LOOP DETECTED] You are repeating the same tool calls. Stop calling tools and produce your final answer now. If you cannot complete the task, summarize what you accomplished so far."
const loopHardStopMessage = "[FORCED STOP] Repeated tool calls exceeded the safety limit. Producing final answer with results collected so far."
const todoReminderPrefix = "[todo reminder]"
const todoReminderMessage = todoReminderPrefix + "\nYou have been doing multi-step work for several rounds without updating the plan. Call write_todos to reflect progress (completed/in_progress/pending) before continuing. If the task must produce final files, include `expected_outputs` with absolute `/mnt/user-data/outputs/...` paths so completion can be verified."

type RunPolicy struct {
	ToolTurns ToolTurnPolicy
	ToolExec  ToolExecutionPolicy
	Recovery  ToolTurnRecoveryPolicy
	Loop      LoopPolicy
	Retry     RetryPolicy
	Task      TaskProgressPolicy
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

type TaskProgressPolicy interface {
	Reminder(*TaskProgressState, []models.Message) string
	ObserveToolCalls(*TaskProgressState, []models.ToolCall)
}

type LoopDecision struct {
	Warning  string
	HardStop bool
}

type ToolLoopState struct {
	History []string
	Warned  map[string]struct{}
}

type TaskProgressState struct {
	RoundsSinceUpdate int
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
type DefaultTaskProgressPolicy struct {
	ReminderThreshold int
	ReminderText      string
}

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
		Task: DefaultTaskProgressPolicy{
			ReminderThreshold: defaultTodoReminderThreshold,
			ReminderText:      todoReminderMessage,
		},
	}
}

func newToolLoopState() *ToolLoopState {
	return &ToolLoopState{
		History: make([]string, 0, defaultLoopWindowSize),
		Warned:  make(map[string]struct{}),
	}
}

func newTaskProgressState() *TaskProgressState {
	return &TaskProgressState{}
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

func (p DefaultTaskProgressPolicy) Reminder(state *TaskProgressState, messages []models.Message) string {
	if state == nil {
		state = newTaskProgressState()
	}
	threshold := p.ReminderThreshold
	if threshold <= 0 {
		threshold = defaultTodoReminderThreshold
	}
	if state.RoundsSinceUpdate < threshold {
		return ""
	}
	if !hasRecentToolActivity(messages, threshold) {
		return ""
	}
	if hasRecentTaskReminder(messages) {
		return ""
	}
	return firstNonEmpty(p.ReminderText, todoReminderMessage)
}

func (DefaultTaskProgressPolicy) ObserveToolCalls(state *TaskProgressState, calls []models.ToolCall) {
	if state == nil || len(calls) == 0 {
		return
	}
	for _, call := range calls {
		if strings.EqualFold(strings.TrimSpace(call.Name), "write_todos") {
			state.RoundsSinceUpdate = 0
			return
		}
	}
	state.RoundsSinceUpdate++
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
	if policy.Task == nil {
		policy.Task = DefaultTaskProgressPolicy{
			ReminderThreshold: defaultTodoReminderThreshold,
			ReminderText:      todoReminderMessage,
		}
	}
	return policy
}

func hasRecentToolActivity(messages []models.Message, rounds int) bool {
	if rounds <= 0 {
		return false
	}
	seen := 0
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == models.RoleAI && len(msg.ToolCalls) > 0 {
			seen++
			if seen >= rounds {
				return true
			}
		}
	}
	return false
}

func hasRecentTaskReminder(messages []models.Message) bool {
	for i := len(messages) - 1; i >= 0 && i >= len(messages)-4; i-- {
		msg := messages[i]
		if msg.Role != models.RoleSystem {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(msg.Content), todoReminderPrefix) {
			return true
		}
	}
	return false
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
