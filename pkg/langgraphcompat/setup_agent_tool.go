package langgraphcompat

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func (s *Server) setupAgentTool() models.Tool {
	return models.Tool{
		Name:        "setup_agent",
		Description: "Create or finalize a custom agent by saving its SOUL and description.",
		Groups:      []string{"builtin", "agent"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"soul": map[string]any{
					"type":        "string",
					"description": "Full SOUL.md content for the custom agent",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "One-line description of what the agent does",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Optional explicit agent name override",
				},
			},
			"required": []any{"soul", "description"},
		},
		Handler: func(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
			return s.handleSetupAgentTool(ctx, call)
		},
	}
}

func (s *Server) handleSetupAgentTool(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	soul, _ := call.Arguments["soul"].(string)
	description, _ := call.Arguments["description"].(string)
	explicitName, _ := call.Arguments["name"].(string)

	soul = strings.TrimSpace(soul)
	description = strings.TrimSpace(description)
	if soul == "" {
		err := fmt.Errorf("soul is required")
		return failedToolResult(call, err), err
	}
	if description == "" {
		err := fmt.Errorf("description is required")
		return failedToolResult(call, err), err
	}

	agentName, err := setupAgentNameFromContext(ctx, explicitName)
	if err != nil {
		return failedToolResult(call, err), err
	}

	createdAgent := gatewayAgent{
		Name:        agentName,
		Description: description,
		Soul:        soul,
	}

	s.uiStateMu.Lock()
	agents := s.getAgentsLocked()
	if _, exists := agents[agentName]; exists {
		s.uiStateMu.Unlock()
		err := fmt.Errorf("agent %q already exists", agentName)
		return failedToolResult(call, err), err
	}
	agents[agentName] = createdAgent
	s.uiStateMu.Unlock()

	if err := s.persistAgentFiles(agentName, createdAgent); err != nil {
		s.uiStateMu.Lock()
		delete(s.getAgentsLocked(), agentName)
		s.uiStateMu.Unlock()
		return failedToolResult(call, err), err
	}
	if err := s.persistGatewayState(); err != nil {
		s.uiStateMu.Lock()
		delete(s.getAgentsLocked(), agentName)
		s.uiStateMu.Unlock()
		_ = os.RemoveAll(s.agentDir(agentName))
		err = fmt.Errorf("failed to persist state: %w", err)
		return failedToolResult(call, err), err
	}
	if threadID := strings.TrimSpace(tools.ThreadIDFromContext(ctx)); threadID != "" {
		s.setThreadValue(threadID, "created_agent_name", agentName)
	}

	return models.ToolResult{
		CallID:   call.ID,
		ToolName: call.Name,
		Status:   models.CallStatusCompleted,
		Content:  fmt.Sprintf("Agent '%s' created successfully!", agentName),
		Data: map[string]any{
			"name":        createdAgent.Name,
			"description": createdAgent.Description,
		},
		CompletedAt: time.Now().UTC(),
	}, nil
}

func setupAgentNameFromContext(ctx context.Context, explicitName string) (string, error) {
	if name, ok := normalizeAgentName(strings.TrimSpace(explicitName)); ok {
		return name, nil
	}

	runtimeContext := tools.RuntimeContextFromContext(ctx)
	if name, ok := normalizeAgentName(stringFromAny(runtimeContext["agent_name"])); ok {
		return name, nil
	}
	if name, ok := normalizeAgentName(stringFromAny(runtimeContext["created_agent_name"])); ok {
		return name, nil
	}
	return "", fmt.Errorf("agent name is required in runtime context")
}

func failedToolResult(call models.ToolCall, err error) models.ToolResult {
	return models.ToolResult{
		CallID:      call.ID,
		ToolName:    call.Name,
		Status:      models.CallStatusFailed,
		Error:       err.Error(),
		CompletedAt: time.Now().UTC(),
	}
}
