package agent

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
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
const defaultRequestTimeout = 10 * time.Minute
const defaultLoopWarnThreshold = 3
const defaultLoopHardLimit = 5
const defaultLoopWindowSize = 20
const defaultMaxConcurrentSubagents = 3
const minMaxConcurrentSubagents = 2
const maxMaxConcurrentSubagents = 4

const loopWarningMessage = "[LOOP DETECTED] You are repeating the same tool calls. Stop calling tools and produce your final answer now. If you cannot complete the task, summarize what you accomplished so far."
const loopHardStopMessage = "[FORCED STOP] Repeated tool calls exceeded the safety limit. Producing final answer with results collected so far."

var messageSeq uint64
var agentRequestSeq uint64

// Agent runs our custom ReAct loop while delegating model streaming and tool schemas to Eino.
type Agent struct {
	llm                    llm.LLMProvider
	tools                  *tools.Registry
	deferredTools          *tools.DeferredToolRegistry
	sandbox                *sandbox.Sandbox
	agentType              AgentType
	model                  string
	reasoningEffort        string
	systemPrompt           string
	temperature            *float64
	maxTokens              *int
	maxTurns               int
	maxConcurrentSubagents int
	requestTimeout         time.Duration
	events                 chan AgentEvent
	requests               sync.Map
	runMu                  sync.Mutex
	eventsMu               sync.RWMutex
	eventsClosed           bool
	started                bool
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
		events:                 make(chan AgentEvent, 128),
	}
}

func cloneRegistryWithPresentFileTool(base *tools.Registry, presentFiles *tools.PresentFileRegistry) *tools.Registry {
	cloned := tools.NewRegistry()
	if base != nil {
		for _, tool := range base.List() {
			if tool.Name == "present_file" || tool.Name == "present_files" {
				continue
			}
			_ = cloned.Register(tool)
		}
	}
	_ = cloned.Register(tools.PresentFilesTool(presentFiles))
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
	loopHistory := make([]string, 0, defaultLoopWindowSize)
	loopWarned := make(map[string]struct{})

	for turn := 0; turn < a.maxTurns; turn++ {
		req := llm.ChatRequest{
			Model:           a.model,
			Messages:        runMessages,
			Tools:           a.visibleTools(deferredState),
			ReasoningEffort: a.reasoningEffort,
			Temperature:     a.temperature,
			MaxTokens:       a.maxTokens,
			SystemPrompt:    a.buildSystemPrompt(ctx, sessionID, deferredState),
		}

		stream, err := a.llm.Stream(ctx, req)
		if err != nil {
			err = normalizeRunError(ctx, err, a.requestTimeout)
			emit(AgentEvent{Type: AgentEventError, Err: err.Error(), Error: newAgentError(err)})
			return nil, err
		}

		var (
			aiMessageID = newMessageID("ai")
			textBuilder strings.Builder
			toolCalls   []models.ToolCall
			streamUsage *llm.Usage
			stopReason  string
		)

		for chunk := range stream {
			if chunk.Err != nil {
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
					if len(toolCalls) == 0 && len(chunk.Message.ToolCalls) > 0 {
						toolCalls = append(toolCalls, chunk.Message.ToolCalls...)
					}
				}
			}
		}
		if err := ctx.Err(); err != nil {
			err = normalizeRunError(ctx, err, a.requestTimeout)
			emit(AgentEvent{Type: AgentEventError, Err: err.Error(), Error: newAgentError(err)})
			return nil, err
		}

		if streamUsage != nil {
			accumulateUsage(usage, streamUsage)
		}
		toolCalls = truncateTaskToolCalls(toolCalls, a.maxConcurrentSubagents)

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
		if assistantMessage.Content != "" || len(assistantMessage.ToolCalls) > 0 {
			runMessages = append(runMessages, assistantMessage)
		}

		if len(toolCalls) == 0 {
			emit(AgentEvent{
				Type:      AgentEventEnd,
				MessageID: aiMessageID,
				Text:      assistantMessage.Content,
				Usage:     cloneUsage(usage),
			})
			return &RunResult{
				Messages:    runMessages,
				FinalOutput: assistantMessage.Content,
				Usage:       usage,
			}, nil
		}

		warning, hardStop, nextLoopHistory := detectToolCallLoop(loopHistory, toolCalls, loopWarned)
		loopHistory = nextLoopHistory
		if warning != "" || hardStop {
			if hardStop {
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
					Usage:     cloneUsage(usage),
				})
				return &RunResult{
					Messages:    runMessages,
					FinalOutput: finalOutput,
					Usage:       usage,
				}, nil
			}

			viewedImages := make([]viewedImage, 0)
			var pause bool
			var err error
			runMessages, viewedImages, pause, err = a.executeToolCalls(ctx, sessionID, aiMessageID, runMessages, toolCalls, deferredState, emit)
			if err != nil {
				return nil, err
			}
			if len(viewedImages) > 0 {
				runMessages = append(runMessages, viewedImagesMessage(sessionID, viewedImages, modelLikelySupportsVision(a.model)))
			}
			if pause {
				return &RunResult{
					Messages:    runMessages,
					FinalOutput: assistantMessage.Content,
					Usage:       usage,
				}, nil
			}
			runMessages = append(runMessages, models.Message{
				ID:        newMessageID("human"),
				SessionID: sessionID,
				Role:      models.RoleHuman,
				Content:   warning,
				CreatedAt: time.Now().UTC(),
			})
			continue
		}

		viewedImages := make([]viewedImage, 0)
		pause := false
		runMessages, viewedImages, pause, err = a.executeToolCalls(ctx, sessionID, aiMessageID, runMessages, toolCalls, deferredState, emit)
		if err != nil {
			return nil, err
		}
		if len(viewedImages) > 0 {
			runMessages = append(runMessages, viewedImagesMessage(sessionID, viewedImages, modelLikelySupportsVision(a.model)))
		}
		if pause {
			return &RunResult{
				Messages:    runMessages,
				FinalOutput: assistantMessage.Content,
				Usage:       usage,
			}, nil
		}
	}

	err := fmt.Errorf("agent exceeded max turns (%d)", a.maxTurns)
	emit(AgentEvent{Type: AgentEventError, Err: err.Error(), Error: newAgentError(err)})
	return nil, err
}

func (a *Agent) executeToolCalls(
	ctx context.Context,
	sessionID string,
	aiMessageID string,
	runMessages []models.Message,
	toolCalls []models.ToolCall,
	deferredState *deferredToolState,
	emit func(AgentEvent),
) ([]models.Message, []viewedImage, bool, error) {
	viewedImages := make([]viewedImage, 0)

	for i := 0; i < len(toolCalls); {
		if toolCalls[i].Name != "task" {
			result, pause, err := a.executeSingleToolCall(ctx, sessionID, aiMessageID, toolCalls[i], deferredState, emit, &runMessages, &viewedImages)
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
		results, err := a.executeParallelTaskCalls(ctx, sessionID, aiMessageID, toolCalls[i:j], deferredState, emit)
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
			result, toolErr := a.performToolCall(ctx, sessionID, call, deferredState)
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

	result, err := a.performToolCall(ctx, sessionID, call, deferredState)
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
	if shouldPauseAfterToolCall(call, result) {
		return result, true, nil
	}
	if err := ctx.Err(); err != nil {
		err = normalizeRunError(ctx, err, a.requestTimeout)
		emit(AgentEvent{Type: AgentEventError, Err: err.Error(), Error: newAgentError(err)})
		return models.ToolResult{}, false, err
	}
	return result, false, nil
}

func (a *Agent) performToolCall(ctx context.Context, sessionID string, call models.ToolCall, deferredState *deferredToolState) (models.ToolResult, error) {
	toolStarted := time.Now().UTC()
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
	result = sanitizedToolResult(result)
	return result, nil
}

func shouldPauseAfterToolCall(call models.ToolCall, result models.ToolResult) bool {
	return call.Name == "ask_clarification" && result.Status != models.CallStatusFailed
}

func detectToolCallLoop(history []string, calls []models.ToolCall, warned map[string]struct{}) (string, bool, []string) {
	callHash := hashToolCalls(calls)
	if callHash == "" {
		return "", false, history
	}
	history = append(history, callHash)
	if len(history) > defaultLoopWindowSize {
		history = history[len(history)-defaultLoopWindowSize:]
	}
	count := 0
	for _, previous := range history {
		if previous == callHash {
			count++
		}
	}
	if count >= defaultLoopHardLimit {
		return "", true, history
	}
	if count >= defaultLoopWarnThreshold {
		if _, ok := warned[callHash]; !ok {
			warned[callHash] = struct{}{}
			return loopWarningMessage, false, history
		}
	}
	return "", false, history
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
		normalized = append(normalized, normalizedToolCall{
			Name: call.Name,
			Args: call.Arguments,
		})
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

func (a *Agent) BuildSystemPrompt(ctx context.Context, sessionID string) string {
	return a.buildSystemPrompt(ctx, sessionID, newDeferredToolState(a.deferredTools))
}

func (a *Agent) buildSystemPrompt(_ context.Context, _ string, deferredState *deferredToolState) string {
	sections := []string{
		strings.TrimSpace(a.systemPrompt),
		"You are running in a ReAct-style loop. Think step by step, call tools when necessary, and stop when you have a complete answer.",
	}
	if deferredPrompt := deferredState.prompt(); deferredPrompt != "" {
		sections = append(sections, deferredPrompt)
	}
	if toolDescriptions := describeTools(a.visibleTools(deferredState)); strings.TrimSpace(toolDescriptions) != "" {
		sections = append(sections, "Available Tools:\n"+toolDescriptions)
	}
	return strings.Join(sections, "\n\n")
}

func (a *Agent) visibleTools(deferredState *deferredToolState) []models.Tool {
	visible := append([]models.Tool(nil), a.tools.List()...)
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

func describeTools(items []models.Tool) string {
	if len(items) == 0 {
		return ""
	}
	var lines []string
	for _, tool := range items {
		line := fmt.Sprintf("- %s: %s", tool.Name, strings.TrimSpace(tool.Description))
		if len(tool.InputSchema) > 0 {
			if raw, err := json.MarshalIndent(tool.InputSchema, "", "  "); err == nil {
				line += "\n  schema: " + strings.ReplaceAll(string(raw), "\n", "\n  ")
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
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

func patchDanglingToolCalls(messages []models.Message) []models.Message {
	if len(messages) == 0 {
		return messages
	}

	existingResults := make(map[string]struct{})
	for _, msg := range messages {
		if msg.Role != models.RoleTool || msg.ToolResult == nil {
			continue
		}
		callID := strings.TrimSpace(msg.ToolResult.CallID)
		if callID != "" {
			existingResults[callID] = struct{}{}
		}
	}

	patched := make([]models.Message, 0, len(messages))
	inserted := make(map[string]struct{})
	needsPatch := false

	for _, msg := range messages {
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
		return result.Error
	}
	return result.Content
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
