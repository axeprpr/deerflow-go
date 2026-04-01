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
	llm     llm.LLMProvider
	tools   *tools.Registry
	sandbox *sandbox.Sandbox
	model   string
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

func (e *SubagentExecutor) Execute(ctx context.Context, task *subagent.Task, emit func(subagent.TaskEvent)) (subagent.ExecutionResult, error) {
	if e == nil || e.llm == nil {
		return subagent.ExecutionResult{}, fmt.Errorf("subagent llm provider is required")
	}

	registry := tools.NewRegistry()
	for _, tool := range selectSubagentToolsWithDenylist(e.tools.List(), task.Config.Tools, task.Config.DisallowedTools) {
		_ = registry.Register(tool)
	}

	runAgent := New(AgentConfig{
		LLMProvider:    e.llm,
		Tools:          registry,
		MaxTurns:       task.Config.MaxTurns,
		Model:          e.model,
		Sandbox:        e.sandbox,
		RequestTimeout: task.Config.Timeout,
	})

	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		for evt := range runAgent.Events() {
			message := subagentMessageFromAgentEvent(evt)
			if strings.TrimSpace(message) == "" {
				continue
			}
			emit(subagent.TaskEvent{
				Type:        "task_running",
				TaskID:      task.ID,
				Description: task.Description,
				Message:     message,
			})
		}
	}()

	result, err := runAgent.Run(ctx, task.ID, []models.Message{
		{
			ID:        newSubagentMessageID("system"),
			SessionID: task.ID,
			Role:      models.RoleSystem,
			Content:   subagentSystemPrompt(task),
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

func subagentMessageFromAgentEvent(evt AgentEvent) string {
	switch evt.Type {
	case AgentEventChunk, AgentEventTextChunk:
		return strings.TrimSpace(evt.Text)
	case AgentEventToolCallStart:
		if evt.ToolEvent != nil {
			return fmt.Sprintf("calling tool %s", evt.ToolEvent.Name)
		}
	case AgentEventToolCallEnd:
		if evt.ToolEvent != nil {
			if evt.ToolEvent.Error != "" {
				return fmt.Sprintf("tool %s failed: %s", evt.ToolEvent.Name, evt.ToolEvent.Error)
			}
			return fmt.Sprintf("tool %s completed", evt.ToolEvent.Name)
		}
	case AgentEventError:
		return strings.TrimSpace(evt.Err)
	}
	return ""
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

func newSubagentMessageID(prefix string) string {
	seq := atomic.AddUint64(&subagentMessageSeq, 1)
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UTC().UnixNano(), seq)
}
