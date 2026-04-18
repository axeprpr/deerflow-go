package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/guardrails"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"github.com/cloudwego/eino/adk"
	einoSchema "github.com/cloudwego/eino/schema"
)

const defaultMaxTurns = 8
const defaultRequestTimeout = 10 * time.Minute
const defaultMaxConcurrentSubagents = 3
const minMaxConcurrentSubagents = 2
const maxMaxConcurrentSubagents = 4
const defaultReadFileMaxCallsPerRun = 24
const defaultReadFileMaxCharsPerCall = 20000
const defaultReadFileMaxCharsPerRun = 180000
const defaultSchemaRepairMaxOutputChars = 16000

var messageSeq uint64
var agentRequestSeq uint64

// Agent runs our custom ReAct loop while delegating model streaming and tool schemas to Eino.
type Agent struct {
	llm                    llm.LLMProvider
	tools                  *tools.Registry
	deferredTools          *tools.DeferredToolRegistry
	sandbox                sandbox.Session
	agentType              AgentType
	model                  string
	reasoningEffort        string
	systemPrompt           string
	temperature            *float64
	maxTokens              *int
	maxTurns               int
	maxConcurrentSubagents int
	requestTimeout         time.Duration
	runPolicy              *RunPolicy
	readFileBudget         readFileBudgetPolicy
	runFactStore           runFactStorePolicy
	schemaRepair           schemaRepairPolicy
	pinnedFacts            map[string]string
	guardrailProvider      guardrails.Provider
	guardrailFailClosed    bool
	guardrailPassport      string
	events                 chan AgentEvent
	requests               sync.Map
	runMu                  sync.Mutex
	eventsMu               sync.RWMutex
	eventsClosed           bool
	started                bool
}

type structuredToolCallProvider interface {
	PrefersStructuredToolCalls() bool
}

type modelStructuredToolCallProvider interface {
	PrefersStructuredToolCallsForModel(model string) bool
}

func New(cfg AgentConfig) *Agent {
	if err := ApplyAgentType(&cfg, cfg.AgentType); err != nil {
		cfg.AgentType = AgentTypeGeneral
		_ = ApplyAgentType(&cfg, AgentTypeGeneral)
	}
	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}
	registry := cfg.Tools
	if registry == nil {
		registry = tools.NewRegistry()
	}
	if cfg.PresentFiles != nil {
		registry = cloneRegistryWithPresentFileTool(registry, cfg.PresentFiles)
	}
	requestTimeout := cfg.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}
	guardrailProvider, guardrailFailClosed, guardrailPassport := resolveGuardrails(cfg)
	return &Agent{
		llm:                    cfg.LLMProvider,
		tools:                  registry,
		deferredTools:          tools.NewDeferredToolRegistry(cfg.DeferredTools),
		sandbox:                cfg.Sandbox,
		agentType:              cfg.AgentType,
		model:                  resolveModel(cfg.Model),
		reasoningEffort:        strings.TrimSpace(cfg.ReasoningEffort),
		systemPrompt:           strings.TrimSpace(cfg.SystemPrompt),
		temperature:            cfg.Temperature,
		maxTokens:              cfg.MaxTokens,
		maxTurns:               maxTurns,
		maxConcurrentSubagents: clampMaxConcurrentSubagents(cfg.MaxConcurrentSubagents),
		requestTimeout:         requestTimeout,
		runPolicy:              resolveRunPolicy(cfg.RunPolicy),
		readFileBudget:         resolveReadFileBudgetPolicy(),
		runFactStore:           resolveRunFactStorePolicy(),
		schemaRepair:           resolveSchemaRepairPolicy(),
		pinnedFacts:            cloneStringMap(cfg.PinnedFacts),
		guardrailProvider:      guardrailProvider,
		guardrailFailClosed:    guardrailFailClosed,
		guardrailPassport:      guardrailPassport,
		events:                 make(chan AgentEvent, 128),
	}
}

func cloneRegistryWithPresentFileTool(base *tools.Registry, presentFiles *tools.PresentFileRegistry) *tools.Registry {
	cloned := tools.NewRegistry()
	insertedPresentFiles := false
	if base != nil {
		for _, tool := range base.List() {
			if tool.Name == "present_file" || tool.Name == "present_files" {
				continue
			}
			if !insertedPresentFiles && tool.Name == "ask_clarification" {
				_ = cloned.Register(tools.PresentFilesTool(presentFiles))
				insertedPresentFiles = true
			}
			_ = cloned.Register(tool)
		}
	}
	if !insertedPresentFiles {
		_ = cloned.Register(tools.PresentFilesTool(presentFiles))
	}
	_ = cloned.Register(tools.PresentFileTool(presentFiles))
	return cloned
}

func (a *Agent) Events() <-chan AgentEvent {
	return a.events
}

func (a *Agent) EinoAgent() adk.Agent {
	return &einoAgentAdapter{agent: a}
}

func (a *Agent) Run(ctx context.Context, sessionID string, messages []models.Message) (*RunResult, error) {
	if a == nil {
		return nil, fmt.Errorf("agent is nil")
	}
	a.runMu.Lock()
	if a.started {
		a.runMu.Unlock()
		return nil, errors.New("agent instances are single-use")
	}
	a.started = true
	a.runMu.Unlock()
	defer a.closeEvents()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.llm == nil {
		return nil, fmt.Errorf("agent llm provider is required")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.requestTimeout)
		defer cancel()
	}

	requestID := newAgentRequestID()
	a.requests.Store(requestID, sessionID)
	defer a.requests.Delete(requestID)

	deferredState := newDeferredToolState(a.deferredTools)

	emit := func(evt AgentEvent) {
		evt.RequestID = requestID
		if evt.SessionID == "" {
			evt.SessionID = sessionID
		}
		a.emit(evt)
	}

	runMessages := append([]models.Message(nil), messages...)
	runMessages = patchDanglingToolCalls(runMessages)
	usage := &Usage{}
	loopState := newToolLoopState()
	taskState := newTaskProgressState()
	readFileBudgetState := newReadFileBudgetState()
	runFactStoreState := newRunFactStoreState(a.runFactStore, a.pinnedFacts)

	for turn := 0; turn < a.maxTurns; turn++ {
		visibleTools := a.visibleTools(deferredState)
		if hasTodoTool(visibleTools) {
			if reminder := a.runPolicy.Task.Reminder(taskState, runMessages); reminder != "" {
				runMessages = append(runMessages, models.Message{
					ID:        newMessageID("system"),
					SessionID: sessionID,
					Role:      models.RoleSystem,
					Content:   reminder,
					CreatedAt: time.Now().UTC(),
				})
			}
		}
		req := llm.ChatRequest{
			Model:           a.model,
			Messages:        runMessages,
			Tools:           visibleTools,
			ReasoningEffort: a.reasoningEffort,
			Temperature:     a.temperature,
			MaxTokens:       a.maxTokens,
			SystemPrompt:    a.buildSystemPrompt(ctx, sessionID, deferredState, runFactStoreState),
		}

		var (
			aiMessageID = newMessageID("ai")
			textBuilder strings.Builder
			toolCalls   []models.ToolCall
			streamUsage *llm.Usage
			stopReason  string
		)

		if len(req.Tools) > 0 && a.runPolicy.ToolTurns.UseStructuredToolCalls(a.llm, req) {
			resp, chatErr := a.llm.Chat(ctx, req)
			if chatErr != nil {
				err := normalizeRunError(ctx, chatErr, a.requestTimeout)
				emit(AgentEvent{Type: AgentEventError, Err: err.Error(), Error: newAgentError(err)})
				return nil, err
			}
			if resp.Message.Content != "" {
				textBuilder.WriteString(resp.Message.Content)
				emit(AgentEvent{Type: AgentEventChunk, MessageID: aiMessageID, Text: resp.Message.Content})
				emit(AgentEvent{Type: AgentEventTextChunk, MessageID: aiMessageID, Text: resp.Message.Content})
			}
			if len(resp.Message.ToolCalls) > 0 {
				toolCalls = mergeToolCalls(toolCalls, resp.Message.ToolCalls)
			}
			if resp.Usage != (llm.Usage{}) {
				streamUsage = &resp.Usage
			}
			stopReason = resp.Stop
		} else {
			stream, streamErr := a.llm.Stream(ctx, req)
			if streamErr != nil {
				if recovered, recoverErr := a.recoverToolTurn(ctx, req, aiMessageID, textBuilder.String(), len(toolCalls) > 0, &textBuilder, &toolCalls, &streamUsage, &stopReason, emit, streamErr); recovered {
					goto llmTurnComplete
				} else if recoverErr != nil {
					err := normalizeRunError(ctx, recoverErr, a.requestTimeout)
					emit(AgentEvent{Type: AgentEventError, Err: err.Error(), Error: newAgentError(err)})
					return nil, err
				}
				err := normalizeRunError(ctx, streamErr, a.requestTimeout)
				emit(AgentEvent{Type: AgentEventError, Err: err.Error(), Error: newAgentError(err)})
				return nil, err
			}

			for chunk := range stream {
				if chunk.Err != nil {
					if recovered, recoverErr := a.recoverToolTurn(ctx, req, aiMessageID, textBuilder.String(), len(toolCalls) > 0, &textBuilder, &toolCalls, &streamUsage, &stopReason, emit, chunk.Err); recovered {
						break
					} else if recoverErr != nil {
						err := normalizeRunError(ctx, recoverErr, a.requestTimeout)
						emit(AgentEvent{Type: AgentEventError, Err: err.Error(), Error: newAgentError(err)})
						return nil, err
					}
					err := normalizeRunError(ctx, chunk.Err, a.requestTimeout)
					emit(AgentEvent{Type: AgentEventError, Err: err.Error(), Error: newAgentError(err)})
					return nil, err
				}
				if chunk.Delta != "" {
					textBuilder.WriteString(chunk.Delta)
					emit(AgentEvent{Type: AgentEventChunk, MessageID: aiMessageID, Text: chunk.Delta})
					emit(AgentEvent{Type: AgentEventTextChunk, MessageID: aiMessageID, Text: chunk.Delta})
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
						if len(chunk.Message.ToolCalls) > 0 {
							toolCalls = mergeToolCalls(toolCalls, chunk.Message.ToolCalls)
						}
					}
				}
			}
		}
	llmTurnComplete:
		if err := ctx.Err(); err != nil {
			err = normalizeRunError(ctx, err, a.requestTimeout)
			emit(AgentEvent{Type: AgentEventError, Err: err.Error(), Error: newAgentError(err)})
			return nil, err
		}

		if streamUsage != nil {
			accumulateUsage(usage, streamUsage)
		}
		toolCalls = truncateTaskToolCalls(toolCalls, a.maxConcurrentSubagents)
		toolCalls = normalizeToolCalls(toolCalls)
		toolCalls = rewriteSkillToolAliases(ctx, toolCalls)

		assistantMetadata := map[string]string{"stop_reason": stopReason}
		if streamUsage != nil {
			if raw, err := json.Marshal(streamUsage); err == nil {
				assistantMetadata["usage_metadata"] = string(raw)
			}
		}
		assistantMessage := models.Message{
			ID:        aiMessageID,
			SessionID: sessionID,
			Role:      models.RoleAI,
			Content:   textBuilder.String(),
			ToolCalls: toolCalls,
			Metadata:  assistantMetadata,
			CreatedAt: time.Now().UTC(),
		}
		assistantMessage = llm.NormalizeAssistantMessage(assistantMessage)
		if assistantMessage.Content != "" || len(assistantMessage.ToolCalls) > 0 || llm.HasReasoningContent(assistantMessage) {
			runMessages = append(runMessages, assistantMessage)
			runFactStoreState.observeAssistantMessage(assistantMessage)
		}

		if len(toolCalls) == 0 {
			if retryPrompt := missingWebSearchToolCallRetryPrompt(runMessages, assistantMessage, req.Tools); retryPrompt != "" && turn+1 < a.maxTurns {
				runMessages = append(runMessages, models.Message{
					ID:        newMessageID("human"),
					SessionID: sessionID,
					Role:      models.RoleHuman,
					Content:   retryPrompt,
					CreatedAt: time.Now().UTC(),
				})
				continue
			}
			if retryPrompt := missingReadFileToolCallRetryPrompt(runMessages, assistantMessage, req.Tools); retryPrompt != "" && turn+1 < a.maxTurns {
				runMessages = append(runMessages, models.Message{
					ID:        newMessageID("human"),
					SessionID: sessionID,
					Role:      models.RoleHuman,
					Content:   retryPrompt,
					CreatedAt: time.Now().UTC(),
				})
				continue
			}
			if strings.TrimSpace(assistantMessage.Content) == "" {
				if retryPrompt := a.runPolicy.Retry.RecoverableToolRetryPrompt(runMessages); retryPrompt != "" && turn+1 < a.maxTurns {
					runMessages = append(runMessages, models.Message{
						ID:        newMessageID("human"),
						SessionID: sessionID,
						Role:      models.RoleHuman,
						Content:   retryPrompt,
						CreatedAt: time.Now().UTC(),
					})
					continue
				}
			}
			assistantMessage.Content = a.repairStructuredOutput(runMessages, assistantMessage.Content, runFactStoreState)
			if len(runMessages) > 0 {
				runMessages[len(runMessages)-1] = assistantMessage
			}
			emit(AgentEvent{
				Type:      AgentEventEnd,
				MessageID: aiMessageID,
				Text:      assistantMessage.Content,
				Metadata:  assistantMessage.Metadata,
				Usage:     cloneUsage(usage),
			})
			return runResultWithFacts(runMessages, assistantMessage.Content, usage, runFactStoreState), nil
		}

		decision := a.runPolicy.Loop.Evaluate(loopState, toolCalls)
		if decision.Warning != "" || decision.HardStop {
			if decision.HardStop {
				finalOutput := strings.TrimSpace(assistantMessage.Content)
				if finalOutput != "" {
					finalOutput += "\n\n"
				}
				finalOutput += loopHardStopMessage
				assistantMessage.Content = finalOutput
				assistantMessage.ToolCalls = nil
				if len(runMessages) > 0 {
					runMessages[len(runMessages)-1] = assistantMessage
				}
				emit(AgentEvent{
					Type:      AgentEventEnd,
					MessageID: aiMessageID,
					Text:      finalOutput,
					Metadata:  assistantMessage.Metadata,
					Usage:     cloneUsage(usage),
				})
				return runResultWithFacts(runMessages, finalOutput, usage, runFactStoreState), nil
			}

			viewedImages := make([]viewedImage, 0)
			var pause bool
			var err error
			beforeToolMessages := len(runMessages)
			runMessages, viewedImages, pause, err = a.executeToolCalls(ctx, sessionID, aiMessageID, runMessages, toolCalls, deferredState, readFileBudgetState, emit)
			if err != nil {
				return nil, err
			}
			if beforeToolMessages >= 0 && beforeToolMessages <= len(runMessages) {
				runFactStoreState.observeMessages(runMessages[beforeToolMessages:])
			}
			if len(viewedImages) > 0 {
				runMessages = append(runMessages, viewedImagesMessage(sessionID, viewedImages, modelLikelySupportsVision(a.model)))
			}
			if pause {
				return runResultWithFacts(runMessages, assistantMessage.Content, usage, runFactStoreState), nil
			}
			runMessages = append(runMessages, models.Message{
				ID:        newMessageID("human"),
				SessionID: sessionID,
				Role:      models.RoleHuman,
				Content:   decision.Warning,
				CreatedAt: time.Now().UTC(),
			})
			continue
		}

		viewedImages := make([]viewedImage, 0)
		pause := false
		var execErr error
		beforeToolMessages := len(runMessages)
		runMessages, viewedImages, pause, execErr = a.executeToolCalls(ctx, sessionID, aiMessageID, runMessages, toolCalls, deferredState, readFileBudgetState, emit)
		if execErr != nil {
			return nil, execErr
		}
		if beforeToolMessages >= 0 && beforeToolMessages <= len(runMessages) {
			runFactStoreState.observeMessages(runMessages[beforeToolMessages:])
		}
		a.runPolicy.Task.ObserveToolCalls(taskState, toolCalls)
		if len(viewedImages) > 0 {
			runMessages = append(runMessages, viewedImagesMessage(sessionID, viewedImages, modelLikelySupportsVision(a.model)))
		}
		if pause {
			return runResultWithFacts(runMessages, assistantMessage.Content, usage, runFactStoreState), nil
		}
	}

	err := fmt.Errorf("agent exceeded max turns (%d)", a.maxTurns)
	emit(AgentEvent{Type: AgentEventError, Err: err.Error(), Error: newAgentError(err)})
	return nil, err
}

func hasTodoTool(tools []models.Tool) bool {
	for _, tool := range tools {
		if strings.EqualFold(strings.TrimSpace(tool.Name), "write_todos") {
			return true
		}
	}
	return false
}

func (a *Agent) recoverToolTurn(
	ctx context.Context,
	req llm.ChatRequest,
	aiMessageID string,
	partialText string,
	hasPartialToolCalls bool,
	textBuilder *strings.Builder,
	toolCalls *[]models.ToolCall,
	streamUsage **llm.Usage,
	stopReason *string,
	emit func(AgentEvent),
	streamErr error,
) (bool, error) {
	recovery, recovered, err := a.runPolicy.Recovery.Recover(
		ctx,
		a.llm,
		req,
		ToolTurnRecoveryState{
			MessageID:           aiMessageID,
			PartialText:         partialText,
			HasPartialToolCalls: hasPartialToolCalls,
		},
		streamErr,
		emit,
	)
	if err != nil || !recovered {
		return recovered, err
	}
	textBuilder.Reset()
	textBuilder.WriteString(recovery.Text)
	*toolCalls = append((*toolCalls)[:0], recovery.ToolCalls...)
	*streamUsage = recovery.Usage
	*stopReason = recovery.StopReason
	return true, nil
}

func prefersStructuredToolCalls(provider llm.LLMProvider, model string) bool {
	if provider == nil {
		return false
	}
	if structured, ok := provider.(modelStructuredToolCallProvider); ok {
		return structured.PrefersStructuredToolCallsForModel(model)
	}
	structured, ok := provider.(structuredToolCallProvider)
	return ok && structured.PrefersStructuredToolCalls()
}

func (a *Agent) executeToolCalls(
	ctx context.Context,
	sessionID string,
	aiMessageID string,
	runMessages []models.Message,
	toolCalls []models.ToolCall,
	deferredState *deferredToolState,
	readFileBudgetState *readFileBudgetState,
	emit func(AgentEvent),
) ([]models.Message, []viewedImage, bool, error) {
	viewedImages := make([]viewedImage, 0)

	for i := 0; i < len(toolCalls); {
		if toolCalls[i].Name != "task" {
			result, pause, err := a.executeSingleToolCall(ctx, sessionID, aiMessageID, toolCalls[i], deferredState, readFileBudgetState, emit, &runMessages, &viewedImages)
			_ = result
			if err != nil {
				return nil, nil, false, err
			}
			if pause {
				return runMessages, viewedImages, true, nil
			}
			i++
			continue
		}

		j := i
		for j < len(toolCalls) && toolCalls[j].Name == "task" {
			j++
		}
		results, err := a.executeParallelTaskCalls(ctx, sessionID, aiMessageID, toolCalls[i:j], deferredState, readFileBudgetState, emit)
		if err != nil {
			return nil, nil, false, err
		}
		for _, item := range results {
			viewedImages = append(viewedImages, item.viewedImages...)
			runMessages = append(runMessages, item.message)
			emit(AgentEvent{
				Type:      AgentEventToolResult,
				MessageID: item.message.ID,
				Result:    &item.result,
				ToolEvent: newToolEventFromResult(item.call, item.result),
			})
			completedCall := item.runningCall
			completedCall.Status = item.result.Status
			completedCall.CompletedAt = item.result.CompletedAt
			emit(AgentEvent{
				Type:      AgentEventToolCallEnd,
				MessageID: item.message.ID,
				ToolCall:  &completedCall,
				Result:    &item.result,
				ToolEvent: newToolEventFromResult(completedCall, item.result),
			})
		}
		i = j
	}

	return runMessages, viewedImages, false, nil
}

type toolExecutionRecord struct {
	call         models.ToolCall
	runningCall  models.ToolCall
	result       models.ToolResult
	message      models.Message
	viewedImages []viewedImage
}

func (a *Agent) executeParallelTaskCalls(
	ctx context.Context,
	sessionID string,
	aiMessageID string,
	taskCalls []models.ToolCall,
	deferredState *deferredToolState,
	readFileBudgetState *readFileBudgetState,
	emit func(AgentEvent),
) ([]toolExecutionRecord, error) {
	type resultEnvelope struct {
		index  int
		record toolExecutionRecord
	}

	results := make([]toolExecutionRecord, len(taskCalls))
	ch := make(chan resultEnvelope, len(taskCalls))
	var wg sync.WaitGroup

	for idx, call := range taskCalls {
		call := call
		idx := idx

		emit(AgentEvent{
			Type:      AgentEventToolCall,
			MessageID: aiMessageID,
			ToolCall:  &call,
			ToolEvent: newToolCallEvent(call, nil),
		})
		startedAt := time.Now().UTC()
		runningCall := call
		runningCall.Status = models.CallStatusRunning
		runningCall.StartedAt = startedAt
		emit(AgentEvent{
			Type:      AgentEventToolCallStart,
			ToolCall:  &runningCall,
			ToolEvent: newToolCallEvent(runningCall, nil),
		})

		wg.Add(1)
		go func() {
			defer wg.Done()
			result, toolErr := a.performToolCall(ctx, sessionID, call, deferredState, readFileBudgetState)
			toolMessage := models.Message{
				ID:         newMessageID("tool"),
				SessionID:  sessionID,
				Role:       models.RoleTool,
				Content:    toolMessageContent(result),
				ToolResult: &result,
				CreatedAt:  time.Now().UTC(),
			}
			_ = toolErr
			ch <- resultEnvelope{
				index: idx,
				record: toolExecutionRecord{
					call:         call,
					runningCall:  runningCall,
					result:       result,
					message:      toolMessage,
					viewedImages: collectViewedImages(result),
				},
			}
		}()
	}

	wg.Wait()
	close(ch)

	for item := range ch {
		results[item.index] = item.record
	}
	return results, nil
}

func (a *Agent) executeSingleToolCall(
	ctx context.Context,
	sessionID string,
	aiMessageID string,
	call models.ToolCall,
	deferredState *deferredToolState,
	readFileBudgetState *readFileBudgetState,
	emit func(AgentEvent),
	runMessages *[]models.Message,
	viewedImages *[]viewedImage,
) (models.ToolResult, bool, error) {
	emit(AgentEvent{
		Type:      AgentEventToolCall,
		MessageID: aiMessageID,
		ToolCall:  &call,
		ToolEvent: newToolCallEvent(call, nil),
	})
	startedAt := time.Now().UTC()
	runningCall := call
	runningCall.Status = models.CallStatusRunning
	runningCall.StartedAt = startedAt
	emit(AgentEvent{
		Type:      AgentEventToolCallStart,
		ToolCall:  &runningCall,
		ToolEvent: newToolCallEvent(runningCall, nil),
	})

	result, err := a.performToolCall(ctx, sessionID, call, deferredState, readFileBudgetState)
	if err != nil {
		return models.ToolResult{}, false, err
	}

	*viewedImages = append(*viewedImages, collectViewedImages(result)...)
	*runMessages = append(*runMessages, models.Message{
		ID:         newMessageID("tool"),
		SessionID:  sessionID,
		Role:       models.RoleTool,
		Content:    toolMessageContent(result),
		ToolResult: &result,
		CreatedAt:  time.Now().UTC(),
	})
	toolMessage := (*runMessages)[len(*runMessages)-1]
	emit(AgentEvent{
		Type:      AgentEventToolResult,
		MessageID: toolMessage.ID,
		Result:    &result,
		ToolEvent: newToolEventFromResult(call, result),
	})
	completedCall := runningCall
	completedCall.Status = result.Status
	completedCall.CompletedAt = result.CompletedAt
	emit(AgentEvent{
		Type:      AgentEventToolCallEnd,
		MessageID: toolMessage.ID,
		ToolCall:  &completedCall,
		Result:    &result,
		ToolEvent: newToolEventFromResult(completedCall, result),
	})
	if a.runPolicy.ToolExec.ShouldPauseAfterToolCall(call, result) {
		return result, true, nil
	}
	if err := ctx.Err(); err != nil {
		err = normalizeRunError(ctx, err, a.requestTimeout)
		emit(AgentEvent{Type: AgentEventError, Err: err.Error(), Error: newAgentError(err)})
		return models.ToolResult{}, false, err
	}
	return result, false, nil
}

func (a *Agent) performToolCall(ctx context.Context, sessionID string, call models.ToolCall, deferredState *deferredToolState, readFileBudgetState *readFileBudgetState) (models.ToolResult, error) {
	toolStarted := time.Now().UTC()
	if strings.EqualFold(strings.TrimSpace(call.Name), "read_file") {
		if budgetResult, blocked := a.readFileBudget.beforeCall(call, readFileBudgetState); blocked {
			budgetResult.Duration = time.Since(toolStarted)
			if budgetResult.CompletedAt.IsZero() {
				budgetResult.CompletedAt = time.Now().UTC()
			}
			return sanitizedToolResult(budgetResult), nil
		}
	}
	if strings.TrimSpace(call.Name) == "ask_clarification" {
		result, err := clarification.InterceptToolCall(ctx, call)
		if err != nil {
			err = normalizeRunError(ctx, err, a.requestTimeout)
			result = preserveToolFailureResult(call, result, err)
		}
		result.Duration = time.Since(toolStarted)
		if result.CompletedAt.IsZero() {
			result.CompletedAt = time.Now().UTC()
		}
		return sanitizedToolResult(result), nil
	}
	if result, blocked := a.evaluateGuardrails(ctx, sessionID, call); blocked {
		result.Duration = time.Since(toolStarted)
		if result.CompletedAt.IsZero() {
			result.CompletedAt = time.Now().UTC()
		}
		return result, nil
	}
	toolCtx := tools.WithSandbox(ctx, a.sandbox)
	toolCtx = tools.WithThreadID(toolCtx, sessionID)
	result, err := a.executeTool(toolCtx, call, deferredState)
	if err != nil {
		err = normalizeRunError(ctx, err, a.requestTimeout)
		result = preserveToolFailureResult(call, result, err)
	}
	result.Duration = time.Since(toolStarted)
	if result.CompletedAt.IsZero() {
		result.CompletedAt = time.Now().UTC()
	}
	if strings.EqualFold(strings.TrimSpace(call.Name), "read_file") {
		result = a.readFileBudget.afterCall(call, result, readFileBudgetState)
	}
	result = sanitizedToolResult(result)
	return result, nil
}

func (a *Agent) evaluateGuardrails(ctx context.Context, sessionID string, call models.ToolCall) (models.ToolResult, bool) {
	if a == nil || a.guardrailProvider == nil {
		return models.ToolResult{}, false
	}
	req := guardrails.Request{
		ToolName:   strings.TrimSpace(call.Name),
		ToolInput:  cloneGuardrailArgs(call.Arguments),
		AgentID:    a.resolveGuardrailAgentID(ctx),
		ThreadID:   strings.TrimSpace(sessionID),
		IsSubagent: false,
		Timestamp:  time.Now().UTC(),
	}
	decision, err := a.guardrailProvider.Evaluate(req)
	if err != nil {
		if !a.guardrailFailClosed {
			return models.ToolResult{}, false
		}
		return deniedGuardrailToolResult(call, guardrails.Decision{
			Allow: false,
			Reasons: []guardrails.Reason{{
				Code:    "oap.evaluator_error",
				Message: "guardrail provider error (fail-closed)",
			}},
		}), true
	}
	if decision.Allow {
		return models.ToolResult{}, false
	}
	return deniedGuardrailToolResult(call, decision), true
}

func (a *Agent) resolveGuardrailAgentID(ctx context.Context) string {
	if a == nil {
		return ""
	}
	if value := strings.TrimSpace(a.guardrailPassport); value != "" {
		return value
	}
	runtimeContext := tools.RuntimeContextFromContext(ctx)
	if value := strings.TrimSpace(stringFromAny(runtimeContext["agent_name"])); value != "" {
		return value
	}
	return ""
}

func deniedGuardrailToolResult(call models.ToolCall, decision guardrails.Decision) models.ToolResult {
	toolName := strings.TrimSpace(call.Name)
	reasonCode := "oap.denied"
	reasonText := "blocked by guardrail policy"
	if len(decision.Reasons) > 0 {
		if value := strings.TrimSpace(decision.Reasons[0].Code); value != "" {
			reasonCode = value
		}
		if value := strings.TrimSpace(decision.Reasons[0].Message); value != "" {
			reasonText = value
		}
	}
	content := fmt.Sprintf(
		"Guardrail denied: tool '%s' was blocked (%s). Reason: %s. Choose an alternative approach.",
		firstNonEmpty(toolName, "unknown_tool"),
		reasonCode,
		reasonText,
	)
	data := map[string]any{
		"guardrail": map[string]any{
			"allowed":   false,
			"policy_id": strings.TrimSpace(decision.PolicyID),
			"reasons":   guardrailReasonsPayload(decision.Reasons),
		},
	}
	return models.ToolResult{
		CallID:      strings.TrimSpace(call.ID),
		ToolName:    firstNonEmpty(toolName, "unknown_tool"),
		Status:      models.CallStatusFailed,
		Content:     content,
		Error:       content,
		Data:        data,
		CompletedAt: time.Now().UTC(),
	}
}

func guardrailReasonsPayload(reasons []guardrails.Reason) []map[string]any {
	if len(reasons) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(reasons))
	for _, reason := range reasons {
		out = append(out, map[string]any{
			"code":    strings.TrimSpace(reason.Code),
			"message": strings.TrimSpace(reason.Message),
		})
	}
	return out
}

func cloneGuardrailArgs(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(args))
	for key, value := range args {
		cloned[key] = value
	}
	return cloned
}

func stringFromAny(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func resolveGuardrails(cfg AgentConfig) (guardrails.Provider, bool, string) {
	if cfg.GuardrailProvider != nil {
		failClosed := true
		if cfg.GuardrailFailClosed != nil {
			failClosed = *cfg.GuardrailFailClosed
		}
		return cfg.GuardrailProvider, failClosed, strings.TrimSpace(cfg.GuardrailPassport)
	}
	envCfg := guardrails.LoadConfigFromEnv()
	return envCfg.BuildProvider(), envCfg.FailClosed, envCfg.Passport
}

func clampMaxConcurrentSubagents(value int) int {
	if value <= 0 {
		return defaultMaxConcurrentSubagents
	}
	if value < minMaxConcurrentSubagents {
		return minMaxConcurrentSubagents
	}
	if value > maxMaxConcurrentSubagents {
		return maxMaxConcurrentSubagents
	}
	return value
}

type readFileBudgetPolicy struct {
	MaxCallsPerRun  int
	MaxCharsPerRun  int
	MaxCharsPerCall int
}

type readFileBudgetState struct {
	CallsUsed  int
	CharsUsed  int
	CallResult map[string]models.ToolResult
}

type schemaRepairPolicy struct {
	Enabled        bool
	MaxOutputChars int
}

func resolveReadFileBudgetPolicy() readFileBudgetPolicy {
	return readFileBudgetPolicy{
		MaxCallsPerRun:  intFromEnvWithDefault("DEERFLOW_READ_FILE_MAX_CALLS", defaultReadFileMaxCallsPerRun),
		MaxCharsPerRun:  intFromEnvWithDefault("DEERFLOW_READ_FILE_MAX_TOTAL_CHARS", defaultReadFileMaxCharsPerRun),
		MaxCharsPerCall: intFromEnvWithDefault("DEERFLOW_READ_FILE_MAX_CHARS_PER_CALL", defaultReadFileMaxCharsPerCall),
	}
}

func intFromEnvWithDefault(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func newReadFileBudgetState() *readFileBudgetState {
	return &readFileBudgetState{
		CallResult: map[string]models.ToolResult{},
	}
}

func (p readFileBudgetPolicy) beforeCall(call models.ToolCall, state *readFileBudgetState) (models.ToolResult, bool) {
	if state == nil {
		return models.ToolResult{}, false
	}
	if key := readFileDedupeKey(call); key != "" {
		if cached, ok := state.CallResult[key]; ok {
			result := cloneToolResultValue(cached)
			result.CallID = strings.TrimSpace(call.ID)
			result.ToolName = strings.TrimSpace(call.Name)
			result.CompletedAt = time.Now().UTC()
			result.Duration = 0
			if result.Data == nil {
				result.Data = map[string]any{}
			}
			result.Data["read_file_dedupe"] = map[string]any{
				"hit":       true,
				"cache_key": key,
			}
			result.Data["read_file_budget"] = p.budgetData(state)
			return result, true
		}
	}
	if p.MaxCallsPerRun <= 0 {
		return models.ToolResult{}, false
	}
	if state.CallsUsed < p.MaxCallsPerRun {
		return models.ToolResult{}, false
	}
	msg := fmt.Sprintf(
		"read_file call budget exceeded (%d/%d). Narrow the request with start_line/end_line or use grep/glob before reading more files.",
		state.CallsUsed,
		p.MaxCallsPerRun,
	)
	return models.ToolResult{
		CallID:      call.ID,
		ToolName:    call.Name,
		Status:      models.CallStatusFailed,
		Content:     msg,
		Error:       msg,
		CompletedAt: time.Now().UTC(),
		Data: map[string]any{
			"read_file_budget": map[string]any{
				"calls_used":  state.CallsUsed,
				"calls_limit": p.MaxCallsPerRun,
			},
		},
	}, true
}

func (p readFileBudgetPolicy) afterCall(call models.ToolCall, result models.ToolResult, state *readFileBudgetState) models.ToolResult {
	if state == nil {
		return result
	}
	state.CallsUsed++
	content := result.Content
	if p.MaxCharsPerCall > 0 {
		content = truncateWithBudgetMarker(content, p.MaxCharsPerCall, fmt.Sprintf(
			"read_file per-call budget: showing first %d chars",
			p.MaxCharsPerCall,
		))
	}
	if p.MaxCharsPerRun > 0 {
		remaining := p.MaxCharsPerRun - state.CharsUsed
		if remaining < 0 {
			remaining = 0
		}
		content = truncateWithBudgetMarker(content, remaining, fmt.Sprintf(
			"read_file total budget reached (%d chars per run)",
			p.MaxCharsPerRun,
		))
	}
	result.Content = content
	state.CharsUsed += len([]rune(content))
	if result.Status == models.CallStatusCompleted {
		if key := readFileDedupeKey(call); key != "" {
			cached := cloneToolResultValue(result)
			cached.CallID = ""
			cached.Duration = 0
			state.CallResult[key] = cached
		}
	}
	if result.Data == nil {
		result.Data = map[string]any{}
	}
	result.Data["read_file_budget"] = p.budgetData(state)
	return result
}

func (p readFileBudgetPolicy) budgetData(state *readFileBudgetState) map[string]any {
	if state == nil {
		return map[string]any{
			"calls_used":     0,
			"calls_limit":    p.MaxCallsPerRun,
			"chars_used":     0,
			"chars_limit":    p.MaxCharsPerRun,
			"per_call_limit": p.MaxCharsPerCall,
		}
	}
	return map[string]any{
		"calls_used":     state.CallsUsed,
		"calls_limit":    p.MaxCallsPerRun,
		"chars_used":     state.CharsUsed,
		"chars_limit":    p.MaxCharsPerRun,
		"per_call_limit": p.MaxCharsPerCall,
	}
}

func readFileDedupeKey(call models.ToolCall) string {
	path := strings.TrimSpace(stringFromAny(call.Arguments["path"]))
	if path == "" {
		return ""
	}
	startRaw := call.Arguments["start_line"]
	if startRaw == nil {
		startRaw = call.Arguments["startLine"]
	}
	endRaw := call.Arguments["end_line"]
	if endRaw == nil {
		endRaw = call.Arguments["endLine"]
	}
	start := intFromAny(startRaw)
	end := intFromAny(endRaw)
	return fmt.Sprintf("%s|%d|%d", path, start, end)
}

func intFromAny(raw any) int64 {
	switch value := raw.(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case int32:
		return int64(value)
	case float64:
		return int64(value)
	case float32:
		return int64(value)
	case json.Number:
		if n, err := value.Int64(); err == nil {
			return n
		}
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			return n
		}
	}
	return 0
}

func resolveSchemaRepairPolicy() schemaRepairPolicy {
	policy := schemaRepairPolicy{
		Enabled:        boolFromEnvWithDefault("DEERFLOW_SCHEMA_REPAIR_ENABLED", true),
		MaxOutputChars: intFromEnvWithDefault("DEERFLOW_SCHEMA_REPAIR_MAX_OUTPUT_CHARS", defaultSchemaRepairMaxOutputChars),
	}
	if policy.MaxOutputChars <= 0 {
		policy.MaxOutputChars = defaultSchemaRepairMaxOutputChars
	}
	return policy
}

func truncateWithBudgetMarker(content string, limit int, reason string) string {
	if limit < 0 {
		limit = 0
	}
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	if limit == 0 {
		return fmt.Sprintf("[truncated by runtime budget: %s]", strings.TrimSpace(reason))
	}
	marker := fmt.Sprintf("\n... [truncated by runtime budget: %s] ...", strings.TrimSpace(reason))
	markerLen := len([]rune(marker))
	keep := limit - markerLen
	if keep <= 0 {
		return string(runes[:limit])
	}
	return string(runes[:keep]) + marker
}

func truncateTaskToolCalls(calls []models.ToolCall, limit int) []models.ToolCall {
	limit = clampMaxConcurrentSubagents(limit)
	taskCount := 0
	for _, call := range calls {
		if call.Name == "task" {
			taskCount++
		}
	}
	if taskCount <= limit {
		return calls
	}

	truncated := make([]models.ToolCall, 0, len(calls)-(taskCount-limit))
	keptTasks := 0
	for _, call := range calls {
		if call.Name != "task" {
			truncated = append(truncated, call)
			continue
		}
		if keptTasks >= limit {
			continue
		}
		truncated = append(truncated, call)
		keptTasks++
	}
	return truncated
}

func (a *Agent) BuildSystemPrompt(ctx context.Context, sessionID string) string {
	return a.buildSystemPrompt(ctx, sessionID, newDeferredToolState(a.deferredTools), nil)
}

func (a *Agent) buildSystemPrompt(_ context.Context, _ string, deferredState *deferredToolState, factState *runFactStoreState) string {
	sections := []string{strings.TrimSpace(a.systemPrompt)}
	if deferredPrompt := deferredState.prompt(); deferredPrompt != "" {
		sections = append(sections, deferredPrompt)
	}
	if factPrompt := factState.prompt(); factPrompt != "" {
		sections = append(sections, factPrompt)
	}
	return strings.Join(sections, "\n\n")
}

func (a *Agent) repairStructuredOutput(messages []models.Message, content string, factState *runFactStoreState) string {
	if a == nil || !a.schemaRepair.Enabled {
		return content
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return content
	}
	if a.schemaRepair.MaxOutputChars > 0 && len([]rune(trimmed)) > a.schemaRepair.MaxOutputChars {
		return content
	}
	if !shouldRepairStructuredOutput(messages, trimmed) {
		return content
	}

	facts := factState.snapshot()
	requiredKeys := make([]string, 0, len(facts))
	for key := range facts {
		requiredKeys = append(requiredKeys, key)
	}
	sort.Strings(requiredKeys)

	obj, strict := parseStrictJSONObject(trimmed)
	if obj == nil {
		obj = parseFirstJSONObject(trimmed)
	}
	if obj == nil {
		if len(requiredKeys) == 0 {
			return content
		}
		repaired := make(map[string]any, len(requiredKeys))
		for _, key := range requiredKeys {
			repaired[key] = facts[key]
		}
		if encoded, err := json.MarshalIndent(repaired, "", "  "); err == nil {
			return string(encoded)
		}
		return content
	}

	changed := !strict
	for _, key := range requiredKeys {
		if _, ok := obj[key]; ok {
			continue
		}
		if value := strings.TrimSpace(facts[key]); value != "" {
			obj[key] = value
			changed = true
		}
	}
	if !changed {
		return content
	}
	encoded, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return content
	}
	return string(encoded)
}

func shouldRepairStructuredOutput(messages []models.Message, content string) bool {
	contentLower := strings.ToLower(strings.TrimSpace(content))
	if strings.Contains(contentLower, "```json") {
		return true
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != models.RoleHuman {
			continue
		}
		text := strings.ToLower(strings.TrimSpace(msg.Content))
		if text == "" {
			continue
		}
		for _, keyword := range []string{
			"json",
			"schema",
			"structured",
			"字段",
			"结构化",
			"只返回",
			"仅返回",
			"输出为json",
			"返回json",
		} {
			if strings.Contains(text, keyword) {
				return true
			}
		}
		if len([]rune(text)) > 24 {
			return false
		}
	}
	return false
}

func parseStrictJSONObject(text string) (map[string]any, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, false
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, false
	}
	return obj, true
}

func (a *Agent) visibleTools(deferredState *deferredToolState) []models.Tool {
	base := a.tools.List()
	visible := make([]models.Tool, 0, len(base))
	for _, tool := range base {
		// Upstream prompt text mentions `present_file`, but the bound model-visible
		// builtin tool name is only `present_files`. Keep the alias executable for
		// compatibility, but do not expose it to the model surface.
		if tool.Name == "present_file" {
			continue
		}
		visible = append(visible, tool)
	}
	if deferredState == nil || !deferredState.hasDeferred() {
		return visible
	}
	visible = append(visible, deferredState.searchTool())
	visible = append(visible, deferredState.activatedTools()...)
	return visible
}

func (a *Agent) executeTool(ctx context.Context, call models.ToolCall, deferredState *deferredToolState) (models.ToolResult, error) {
	if deferredState != nil {
		if call.Name == "tool_search" && deferredState.hasDeferred() {
			registry := tools.NewRegistry()
			_ = registry.Register(deferredState.searchTool())
			return registry.Execute(ctx, call)
		}
		if tool, ok := deferredState.activatedTool(call.Name); ok {
			registry := tools.NewRegistry()
			_ = registry.Register(tool)
			return registry.Execute(ctx, call)
		}
	}
	return a.tools.Execute(ctx, call)
}

type deferredToolState struct {
	registry  *tools.DeferredToolRegistry
	activated map[string]models.Tool
}

func newDeferredToolState(registry *tools.DeferredToolRegistry) *deferredToolState {
	return &deferredToolState{
		registry:  registry,
		activated: map[string]models.Tool{},
	}
}

func (s *deferredToolState) hasDeferred() bool {
	return s != nil && s.registry != nil && len(s.registry.Entries()) > 0
}

func (s *deferredToolState) activate(matched []models.Tool) {
	if s == nil {
		return
	}
	for _, tool := range matched {
		if strings.TrimSpace(tool.Name) == "" {
			continue
		}
		s.activated[tool.Name] = tool
	}
}

func (s *deferredToolState) activatedTool(name string) (models.Tool, bool) {
	if s == nil {
		return models.Tool{}, false
	}
	tool, ok := s.activated[strings.TrimSpace(name)]
	return tool, ok
}

func (s *deferredToolState) activatedTools() []models.Tool {
	if s == nil || len(s.activated) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.activated))
	for name := range s.activated {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]models.Tool, 0, len(names))
	for _, name := range names {
		out = append(out, s.activated[name])
	}
	return out
}

func (s *deferredToolState) searchTool() models.Tool {
	return tools.DeferredToolSearchTool(s.registry.Search, s.activate)
}

func (s *deferredToolState) prompt() string {
	if !s.hasDeferred() {
		return ""
	}
	entries := s.registry.Entries()
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		line := "- " + entry.Name
		if entry.Description != "" {
			line += ": " + entry.Description
		}
		lines = append(lines, line)
	}
	return "<available_deferred_tools>\n" +
		"Some tools are loaded lazily to keep context small. Search them with `tool_search` before calling them.\n" +
		"Use `select:name1,name2` for exact names, keywords for search, or `+keyword rest` to require text in the tool name.\n" +
		strings.Join(lines, "\n") + "\n" +
		"</available_deferred_tools>"
}

func (a *Agent) emit(evt AgentEvent) {
	a.eventsMu.RLock()
	defer a.eventsMu.RUnlock()
	if a.eventsClosed {
		return
	}
	select {
	case a.events <- evt:
	default:
	}
}

func (a *Agent) closeEvents() {
	a.eventsMu.Lock()
	defer a.eventsMu.Unlock()
	if a.eventsClosed {
		return
	}
	close(a.events)
	a.eventsClosed = true
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

func newAgentRequestID() string {
	seq := atomic.AddUint64(&agentRequestSeq, 1)
	return fmt.Sprintf("req_%d_%d", time.Now().UTC().UnixNano(), seq)
}

func normalizeRunError(ctx context.Context, err error, timeout time.Duration) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &TimeoutError{
			Duration: timeout,
			Message:  "agent request timed out",
		}
	}
	return err
}

func runResultWithFacts(messages []models.Message, finalOutput string, usage *Usage, factState *runFactStoreState) *RunResult {
	return &RunResult{
		Messages:    append([]models.Message(nil), messages...),
		FinalOutput: finalOutput,
		Usage:       usage,
		PinnedFacts: factState.snapshot(),
	}
}

func cloneStringMap(raw map[string]string) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		out[key] = value
	}
	return out
}

func mergeToolCalls(existing, incoming []models.ToolCall) []models.ToolCall {
	if len(existing) == 0 {
		return append([]models.ToolCall(nil), incoming...)
	}

	indexByID := make(map[string]int, len(existing))
	for i, call := range existing {
		if callID := strings.TrimSpace(call.ID); callID != "" {
			indexByID[callID] = i
		}
	}

	for _, call := range incoming {
		if callID := strings.TrimSpace(call.ID); callID != "" {
			if idx, ok := indexByID[callID]; ok {
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
			indexByID[callID] = len(existing)
		}
		existing = append(existing, call)
	}

	return existing
}

func patchDanglingToolCalls(messages []models.Message) []models.Message {
	if len(messages) == 0 {
		return messages
	}

	existingResults := make(map[string]struct{})
	for _, msg := range messages {
		if msg.Role != models.RoleTool || msg.ToolResult == nil {
			continue
		}
		if result, ok := models.NormalizeToolResult(*msg.ToolResult); ok {
			existingResults[result.CallID] = struct{}{}
		}
	}

	patched := make([]models.Message, 0, len(messages))
	inserted := make(map[string]struct{})
	needsPatch := false

	for _, msg := range messages {
		if msg.Role == models.RoleAI && len(msg.ToolCalls) > 0 {
			normalized := normalizeToolCalls(msg.ToolCalls)
			if len(normalized) != len(msg.ToolCalls) {
				needsPatch = true
			}
			msg.ToolCalls = normalized
		}
		patched = append(patched, msg)
		if msg.Role != models.RoleAI || len(msg.ToolCalls) == 0 {
			continue
		}

		for _, call := range msg.ToolCalls {
			callID := strings.TrimSpace(call.ID)
			if callID == "" {
				continue
			}
			if _, ok := existingResults[callID]; ok {
				continue
			}
			if _, ok := inserted[callID]; ok {
				continue
			}

			needsPatch = true
			inserted[callID] = struct{}{}
			result := models.ToolResult{
				CallID:      callID,
				ToolName:    call.Name,
				Status:      models.CallStatusFailed,
				Error:       "[Tool call was interrupted and did not return a result.]",
				CompletedAt: time.Now().UTC(),
			}
			patched = append(patched, models.Message{
				ID:         newMessageID("tool"),
				SessionID:  msg.SessionID,
				Role:       models.RoleTool,
				Content:    toolMessageContent(result),
				ToolResult: &result,
				CreatedAt:  result.CompletedAt,
			})
		}
	}

	if !needsPatch {
		return messages
	}
	return patched
}

func accumulateUsage(dst *Usage, src *llm.Usage) {
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.TotalTokens += src.TotalTokens
	dst.ReasoningTokens += src.ReasoningTokens
	dst.CachedInputTokens += src.CachedInputTokens
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
		detail := strings.TrimSpace(result.Error)
		if len(detail) > 500 {
			detail = detail[:497] + "..."
		}
		toolName := strings.TrimSpace(result.ToolName)
		if toolName == "" {
			toolName = "unknown_tool"
		}
		return fmt.Sprintf("Error: Tool '%s' failed: %s. Continue with available context, or choose an alternative tool.", toolName, detail)
	}
	return result.Content
}

func recoverableToolRetryPrompt(messages []models.Message) string {
	if len(messages) == 0 {
		return ""
	}
	last := messages[len(messages)-1]
	if last.Role != models.RoleTool || last.ToolResult == nil {
		return ""
	}
	result := last.ToolResult
	if result.Status != models.CallStatusFailed {
		return ""
	}
	detail := strings.ToLower(strings.TrimSpace(result.Error))
	if !strings.Contains(detail, "missing required argument") && !strings.Contains(detail, "missing required arguments") {
		return ""
	}

	switch strings.TrimSpace(result.ToolName) {
	case "ask_clarification":
		return "The previous ask_clarification call was invalid because required arguments were missing. Retry ask_clarification only if clarification is truly needed, and include at least `question`. Prefer also setting `clarification_type`, plus optional `context` and `options`. If the request is already clear enough, do not ask for clarification and continue with the next tool."
	case "write_file":
		return "The previous write_file call was invalid because required arguments were missing. Retry write_file with both `path` and `content`. For a final webpage, use `/mnt/user-data/outputs/index.html`."
	case "str_replace":
		return "The previous str_replace call was invalid because required arguments were missing. Retry str_replace with `path`, `old_str`, and `new_str`."
	case "read_file":
		return "The previous read_file call was invalid because required arguments were missing. Retry read_file with a valid `path`."
	case "ls":
		return "The previous ls call was invalid because required arguments were missing. Retry ls with a valid directory `path`."
	default:
		return ""
	}
}

func missingWebSearchToolCallRetryPrompt(messages []models.Message, assistant models.Message, visibleTools []models.Tool) string {
	if len(visibleTools) == 0 {
		return ""
	}
	hasWebSearch := false
	for _, tool := range visibleTools {
		if strings.EqualFold(strings.TrimSpace(tool.Name), "web_search") {
			hasWebSearch = true
			break
		}
	}
	if !hasWebSearch || len(assistant.ToolCalls) > 0 {
		return ""
	}

	assistantLower := strings.ToLower(strings.TrimSpace(assistant.Content))
	if assistantLower == "" {
		return ""
	}
	mentionsWebSearch := strings.Contains(assistantLower, "web_search") ||
		strings.Contains(assistantLower, "web search") ||
		strings.Contains(assistantLower, "联网搜索")
	if !mentionsWebSearch {
		return ""
	}
	for _, msg := range messages {
		if msg.Role != models.RoleTool || msg.ToolResult == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(msg.ToolResult.ToolName), "web_search") {
			return ""
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != models.RoleHuman {
			continue
		}
		humanLower := strings.ToLower(strings.TrimSpace(msg.Content))
		if humanLower == "" {
			return ""
		}
		needsSearch := strings.Contains(humanLower, "联网") ||
			strings.Contains(humanLower, "搜索") ||
			strings.Contains(humanLower, "web search") ||
			strings.Contains(humanLower, "openai api 价格") ||
			strings.Contains(humanLower, "openai api price")
		if !needsSearch {
			return ""
		}
		return "Do not simulate search results in plain text. Call `web_search` now with a concrete query, then continue with the tool output."
	}
	return ""
}

func missingReadFileToolCallRetryPrompt(messages []models.Message, assistant models.Message, visibleTools []models.Tool) string {
	if len(visibleTools) == 0 {
		return ""
	}
	hasReadFile := false
	for _, tool := range visibleTools {
		if strings.EqualFold(strings.TrimSpace(tool.Name), "read_file") {
			hasReadFile = true
			break
		}
	}
	if !hasReadFile || len(assistant.ToolCalls) > 0 {
		return ""
	}
	assistantLower := strings.ToLower(strings.TrimSpace(assistant.Content))
	if assistantLower == "" {
		return ""
	}
	for _, msg := range messages {
		if msg.Role != models.RoleTool || msg.ToolResult == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(msg.ToolResult.ToolName), "read_file") {
			return ""
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != models.RoleHuman {
			continue
		}
		human := strings.TrimSpace(msg.Content)
		if human == "" {
			return ""
		}
		if !strings.Contains(human, "<uploaded_files>") {
			return ""
		}
		if strings.Contains(human, "Do not answer from filename metadata only. Call `read_file` now") {
			return ""
		}
		return "Do not answer from filename metadata only. Call `read_file` now on the relevant uploaded file path(s), then continue with evidence from file content."
	}
	return ""
}

func normalizeToolCalls(calls []models.ToolCall) []models.ToolCall {
	if len(calls) == 0 {
		return nil
	}

	out := make([]models.ToolCall, 0, len(calls))
	seen := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		normalized, ok := models.NormalizeToolCall(call)
		if !ok {
			continue
		}
		if _, exists := seen[normalized.ID]; exists {
			continue
		}
		seen[normalized.ID] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func rewriteSkillToolAliases(ctx context.Context, calls []models.ToolCall) []models.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	skillPaths := runtimeSkillPaths(ctx)
	if len(skillPaths) == 0 {
		return calls
	}

	out := make([]models.ToolCall, 0, len(calls))
	for _, call := range calls {
		skillPath := strings.TrimSpace(skillPaths[strings.TrimSpace(call.Name)])
		if skillPath == "" {
			out = append(out, call)
			continue
		}
		description := strings.TrimSpace(stringFromAny(call.Arguments["description"]))
		if description == "" {
			description = "Load skill instructions before continuing."
		}
		out = append(out, models.ToolCall{
			ID:   call.ID,
			Name: "read_file",
			Arguments: map[string]any{
				"description": description,
				"path":        skillPath,
			},
			Status: call.Status,
		})
	}
	return out
}

func runtimeSkillPaths(ctx context.Context) map[string]string {
	runtimeContext := tools.RuntimeContextFromContext(ctx)
	if len(runtimeContext) == 0 {
		return nil
	}
	raw, ok := runtimeContext["skill_paths"]
	if !ok {
		return nil
	}
	values, ok := raw.(map[string]any)
	if !ok || len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for name, path := range values {
		name = strings.TrimSpace(name)
		resolvedPath := strings.TrimSpace(stringFromAny(path))
		if name == "" || resolvedPath == "" {
			continue
		}
		out[name] = resolvedPath
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func preserveToolFailureResult(call models.ToolCall, result models.ToolResult, err error) models.ToolResult {
	if result.CallID == "" && result.ToolName == "" && result.Status == "" &&
		result.Content == "" && result.Error == "" && result.CompletedAt.IsZero() &&
		result.Duration == 0 && len(result.Data) == 0 {
		return models.ToolResult{
			CallID:      call.ID,
			ToolName:    call.Name,
			Status:      models.CallStatusFailed,
			Error:       tools.FormatToolExecutionError(call.Name, err),
			CompletedAt: time.Now().UTC(),
		}
	}
	if result.CallID == "" {
		result.CallID = call.ID
	}
	if result.ToolName == "" {
		result.ToolName = call.Name
	}
	if result.Status == "" {
		result.Status = models.CallStatusFailed
	}
	if result.Error == "" {
		result.Error = tools.FormatToolExecutionError(call.Name, err)
	}
	if result.CompletedAt.IsZero() {
		result.CompletedAt = time.Now().UTC()
	}
	return result
}

func newToolCallEvent(call models.ToolCall, result *models.ToolResult) *ToolCallEvent {
	event := &ToolCallEvent{
		ID:            call.ID,
		Name:          call.Name,
		Arguments:     cloneArguments(call.Arguments),
		ArgumentsText: formatToolArguments(call.Arguments),
		Status:        call.Status,
		RequestedAt:   formatEventTime(call.RequestedAt),
		StartedAt:     formatEventTime(call.StartedAt),
		CompletedAt:   formatEventTime(call.CompletedAt),
	}
	if result != nil {
		event.Result = cloneToolResult(result)
		event.ResultPreview = toolResultPreview(*result)
		event.Error = result.Error
		event.DurationMS = result.Duration.Milliseconds()
		if event.Status == "" {
			event.Status = result.Status
		}
		if event.CompletedAt == "" {
			event.CompletedAt = formatEventTime(result.CompletedAt)
		}
	}
	return event
}

func newToolEventFromResult(call models.ToolCall, result models.ToolResult) *ToolCallEvent {
	return newToolCallEvent(call, &result)
}

func cloneArguments(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	return out
}

func cloneToolResult(result *models.ToolResult) *models.ToolResult {
	if result == nil {
		return nil
	}
	copyResult := *result
	if len(result.Data) > 0 {
		copyResult.Data = make(map[string]any, len(result.Data))
		for k, v := range result.Data {
			copyResult.Data[k] = v
		}
	}
	return &copyResult
}

func cloneToolResultValue(result models.ToolResult) models.ToolResult {
	cloned := result
	if len(result.Data) > 0 {
		cloned.Data = make(map[string]any, len(result.Data))
		for key, value := range result.Data {
			cloned.Data[key] = value
		}
	}
	return cloned
}

func formatToolArguments(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	raw, err := json.MarshalIndent(args, "", "  ")
	if err != nil {
		return ""
	}
	return string(raw)
}

func toolResultPreview(result models.ToolResult) string {
	content := strings.TrimSpace(result.Content)
	if content == "" {
		content = strings.TrimSpace(result.Error)
	}
	if content == "" && len(result.Data) > 0 {
		raw, err := json.Marshal(result.Data)
		if err == nil {
			content = string(raw)
		}
	}
	content = strings.ReplaceAll(content, "\n", " ")
	if len(content) > 240 {
		return content[:240] + "..."
	}
	return content
}

func formatEventTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func newAgentError(err error) *AgentError {
	if err == nil {
		return nil
	}
	agentErr := &AgentError{
		Message: err.Error(),
	}
	switch {
	case errors.Is(err, context.Canceled):
		agentErr.Code = "context_canceled"
		agentErr.Suggestion = "Retry the run if the cancellation was unintended."
		agentErr.Retryable = true
	case errors.Is(err, context.DeadlineExceeded):
		agentErr.Code = "deadline_exceeded"
		agentErr.Suggestion = "Retry with a longer timeout or lower max_tokens."
		agentErr.Retryable = true
	case strings.Contains(strings.ToLower(err.Error()), "max turns"):
		agentErr.Code = "max_turns_exceeded"
		agentErr.Suggestion = "Increase max turns or simplify the request."
	case strings.Contains(strings.ToLower(err.Error()), "api key"):
		agentErr.Code = "provider_auth"
		agentErr.Suggestion = "Verify the provider credentials and base URL."
	default:
		agentErr.Code = "run_error"
		agentErr.Suggestion = "Retry the run or inspect the previous tool and model events."
		agentErr.Retryable = true
	}
	return agentErr
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
