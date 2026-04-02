package agent

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

var subagentMessageSeq uint64

type SubagentExecutor struct {
	llm             llm.LLMProvider
	tools           *tools.Registry
	sandbox         *sandbox.Sandbox
	sandboxProvider func() (*sandbox.Sandbox, error)
	model           string
}

type subagentStreamState struct {
	currentMessageID string
	currentContent   strings.Builder
	currentToolCalls []map[string]any
	messageCount     int
}

func NewSubagentExecutor(provider llm.LLMProvider, registry *tools.Registry, sb *sandbox.Sandbox) *SubagentExecutor {
	if registry == nil {
		registry = tools.NewRegistry()
	}
	return &SubagentExecutor{
		llm:     provider,
		tools:   registry,
		sandbox: sb,
	}
}

func (e *SubagentExecutor) SetSandboxProvider(provider func() (*sandbox.Sandbox, error)) {
	if e == nil {
		return
	}
	e.sandboxProvider = provider
}

func (e *SubagentExecutor) Execute(ctx context.Context, task *subagent.Task, emit func(subagent.TaskEvent)) (subagent.ExecutionResult, error) {
	if e == nil || e.llm == nil {
		return subagent.ExecutionResult{}, fmt.Errorf("subagent llm provider is required")
	}

	registry := tools.NewRegistry()
	for _, tool := range selectSubagentToolsWithDenylist(e.tools.List(), task.Config.Tools, task.Config.DisallowedTools) {
		_ = registry.Register(tool)
	}

	sandboxRef := e.sandbox
	if sandboxRef == nil && e.sandboxProvider != nil {
		if sb, err := e.sandboxProvider(); err == nil {
			sandboxRef = sb
		}
	}

	runAgent := New(AgentConfig{
		LLMProvider:     e.llm,
		Tools:           registry,
		MaxTurns:        task.Config.MaxTurns,
		Model:           inheritedSubagentModel(ctx, e.model),
		ReasoningEffort: inheritedSubagentReasoningEffort(ctx),
		Sandbox:         sandboxRef,
		RequestTimeout:  task.Config.Timeout,
		SystemPrompt:    inheritedSubagentSystemPrompt(ctx, task),
	})

	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		streamState := &subagentStreamState{}
		for evt := range runAgent.Events() {
			taskEvent, ok := subagentTaskEventFromAgentEvent(task, evt, streamState)
			if !ok {
				continue
			}
			emit(taskEvent)
		}
	}()

	result, err := runAgent.Run(ctx, task.ID, []models.Message{
		{
			ID:        newSubagentMessageID("system"),
			SessionID: task.ID,
			Role:      models.RoleSystem,
			Content:   inheritedSubagentSystemPrompt(ctx, task),
			CreatedAt: time.Now().UTC(),
		},
		{
			ID:        newSubagentMessageID("human"),
			SessionID: task.ID,
			Role:      models.RoleHuman,
			Content:   task.Prompt,
			CreatedAt: time.Now().UTC(),
		},
	})
	<-eventsDone
	if err != nil {
		return subagent.ExecutionResult{}, err
	}
	return subagent.ExecutionResult{
		Result:   result.FinalOutput,
		Messages: result.Messages,
	}, nil
}

func NewSubagentPool(provider llm.LLMProvider, registry *tools.Registry, sb *sandbox.Sandbox, maxConcurrent int, timeout time.Duration) *subagent.Pool {
	return subagent.NewPool(NewSubagentExecutor(provider, registry, sb), subagent.PoolConfig{
		MaxConcurrent: maxConcurrent,
		Timeout:       timeout,
	})
}

func selectSubagentTools(all []models.Tool, selectors []string) []models.Tool {
	return selectSubagentToolsWithDenylist(all, selectors, nil)
}

func selectSubagentToolsWithDenylist(all []models.Tool, selectors []string, denylist []string) []models.Tool {
	if len(selectors) == 0 {
		selected := append([]models.Tool(nil), all...)
		return filterDisallowedSubagentTools(selected, denylist)
	}

	allowNames := make(map[string]struct{}, len(selectors))
	allowGroups := make(map[string]struct{}, len(selectors))
	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		if selector == "" {
			continue
		}
		allowNames[selector] = struct{}{}
		allowGroups[selector] = struct{}{}
	}

	selected := make([]models.Tool, 0, len(all))
	for _, tool := range all {
		if _, ok := allowNames[tool.Name]; ok {
			selected = append(selected, tool)
			continue
		}
		for _, group := range tool.Groups {
			if _, ok := allowGroups[group]; ok {
				selected = append(selected, tool)
				break
			}
		}
	}
	return filterDisallowedSubagentTools(selected, denylist)
}

func filterDisallowedSubagentTools(all []models.Tool, denylist []string) []models.Tool {
	denied := map[string]struct{}{
		"task": {},
	}
	for _, name := range denylist {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		denied[name] = struct{}{}
	}

	filtered := make([]models.Tool, 0, len(all))
	for _, tool := range all {
		if _, blocked := denied[tool.Name]; blocked {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

func subagentTaskEventFromAgentEvent(task *subagent.Task, evt AgentEvent, state *subagentStreamState) (subagent.TaskEvent, bool) {
	if task == nil {
		return subagent.TaskEvent{}, false
	}
	switch evt.Type {
	case AgentEventChunk:
		snapshot := state.applyChunk(evt.MessageID, evt.Text)
		if snapshot == nil {
			return subagent.TaskEvent{}, false
		}
		return subagent.TaskEvent{
			Type:          "task_running",
			TaskID:        task.ID,
			Description:   task.Description,
			Message:       snapshot,
			MessageIndex:  state.messageCount,
			TotalMessages: state.messageCount,
		}, true
	case AgentEventToolCall:
		if evt.ToolCall == nil {
			return subagent.TaskEvent{}, false
		}
		snapshot := state.applyToolCall(evt.MessageID, evt.ToolCall)
		if snapshot == nil {
			return subagent.TaskEvent{}, false
		}
		return subagent.TaskEvent{
			Type:          "task_running",
			TaskID:        task.ID,
			Description:   task.Description,
			Message:       snapshot,
			MessageIndex:  state.messageCount,
			TotalMessages: state.messageCount,
		}, true
	case AgentEventEnd:
		if strings.TrimSpace(evt.Text) == "" {
			return subagent.TaskEvent{}, false
		}
		snapshot := state.applyFinalText(evt.MessageID, evt.Text)
		if snapshot == nil {
			return subagent.TaskEvent{}, false
		}
		return subagent.TaskEvent{
			Type:          "task_running",
			TaskID:        task.ID,
			Description:   task.Description,
			Message:       snapshot,
			MessageIndex:  state.messageCount,
			TotalMessages: state.messageCount,
		}, true
	case AgentEventToolCallEnd:
		if evt.ToolEvent != nil && strings.TrimSpace(evt.ToolEvent.Error) != "" {
			return subagent.TaskEvent{
				Type:        "task_running",
				TaskID:      task.ID,
				Description: task.Description,
				Message:     fmt.Sprintf("tool %s failed: %s", evt.ToolEvent.Name, evt.ToolEvent.Error),
			}, true
		}
	case AgentEventError:
		if text := strings.TrimSpace(evt.Err); text != "" {
			return subagent.TaskEvent{
				Type:        "task_running",
				TaskID:      task.ID,
				Description: task.Description,
				Message:     text,
			}, true
		}
	}
	return subagent.TaskEvent{}, false
}

func (s *subagentStreamState) applyChunk(messageID string, delta string) map[string]any {
	if delta == "" {
		return nil
	}
	s.ensureMessage(messageID)
	s.currentContent.WriteString(delta)
	if strings.TrimSpace(s.currentContent.String()) == "" && len(s.currentToolCalls) == 0 {
		return nil
	}
	return s.snapshot()
}

func (s *subagentStreamState) applyToolCall(messageID string, call *models.ToolCall) map[string]any {
	if call == nil {
		return nil
	}
	s.ensureMessage(messageID)
	toolCall := map[string]any{
		"id":   call.ID,
		"name": call.Name,
		"args": cloneToolCallArgs(call.Arguments),
	}
	for i, existing := range s.currentToolCalls {
		if trimStringValue(existing["id"]) == call.ID && call.ID != "" {
			s.currentToolCalls[i] = toolCall
			return s.snapshot()
		}
	}
	s.currentToolCalls = append(s.currentToolCalls, toolCall)
	return s.snapshot()
}

func (s *subagentStreamState) applyFinalText(messageID string, content string) map[string]any {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	s.ensureMessage(messageID)
	s.currentContent.Reset()
	s.currentContent.WriteString(content)
	return s.snapshot()
}

func (s *subagentStreamState) ensureMessage(messageID string) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		messageID = newSubagentMessageID("ai")
	}
	if s.currentMessageID == messageID {
		return
	}
	s.currentMessageID = messageID
	s.currentContent.Reset()
	s.currentToolCalls = nil
	s.messageCount++
}

func (s *subagentStreamState) snapshot() map[string]any {
	if s == nil || strings.TrimSpace(s.currentMessageID) == "" {
		return nil
	}
	message := map[string]any{
		"type": "ai",
		"id":   s.currentMessageID,
	}
	if content := strings.TrimSpace(s.currentContent.String()); content != "" {
		message["content"] = content
	}
	if len(s.currentToolCalls) > 0 {
		toolCalls := make([]map[string]any, 0, len(s.currentToolCalls))
		for _, call := range s.currentToolCalls {
			toolCalls = append(toolCalls, cloneToolCallMap(call))
		}
		message["tool_calls"] = toolCalls
	}
	return message
}

func cloneToolCallArgs(args map[string]any) map[string]any {
	if len(args) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(args))
	for key, value := range args {
		cloned[key] = value
	}
	return cloned
}

func cloneToolCallMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(src))
	for key, value := range src {
		cloned[key] = value
	}
	return cloned
}

func trimStringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func subagentSystemPrompt(task *subagent.Task) string {
	if task != nil && strings.TrimSpace(task.Config.SystemPrompt) != "" {
		return strings.TrimSpace(task.Config.SystemPrompt)
	}
	if task != nil && task.Type == subagent.SubagentBash {
		return "You are a bash execution specialist. Execute commands carefully, summarize what ran, and report relevant output or failures."
	}
	return "You are a general-purpose subagent working on a delegated task. Complete it autonomously and return a concise, actionable result."
}

func inheritedSubagentSystemPrompt(ctx context.Context, task *subagent.Task) string {
	base := subagentSystemPrompt(task)
	skillsPrompt := runtimeContextString(tools.RuntimeContextFromContext(ctx), "skills_prompt")
	if skillsPrompt == "" {
		return base
	}
	return strings.TrimSpace(base + "\n\n" + skillsPrompt)
}

func inheritedSubagentModel(ctx context.Context, fallback string) string {
	modelName := runtimeContextString(tools.RuntimeContextFromContext(ctx), "model_name")
	if modelName != "" {
		return modelName
	}
	return strings.TrimSpace(fallback)
}

func inheritedSubagentReasoningEffort(ctx context.Context) string {
	return runtimeContextString(tools.RuntimeContextFromContext(ctx), "reasoning_effort")
}

func runtimeContextString(runtimeContext map[string]any, key string) string {
	if len(runtimeContext) == 0 {
		return ""
	}
	value, _ := runtimeContext[key].(string)
	return strings.TrimSpace(value)
}

func newSubagentMessageID(prefix string) string {
	seq := atomic.AddUint64(&subagentMessageSeq, 1)
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UTC().UnixNano(), seq)
}
