package agent

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"sort"

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
	Loop      LoopPolicy
	Retry     RetryPolicy
}

type ToolTurnPolicy interface {
	UseStructuredToolCalls(llm.LLMProvider, llm.ChatRequest) bool
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

type DefaultLoopPolicy struct {
	WarnThreshold int
	HardLimit     int
	WindowSize    int
	WarningText   string
}

type DefaultRetryPolicy struct{}

func DefaultRunPolicy() *RunPolicy {
	return &RunPolicy{
		ToolTurns: DefaultToolTurnPolicy{},
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
