package langgraphcompat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"gopkg.in/yaml.v3"
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

	if err := s.persistAgentFiles(createdAgent); err != nil {
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

func (s *Server) persistAgentFiles(agent gatewayAgent) error {
	dir := s.agentDir(agent.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte(agent.Soul), 0o644); err != nil {
		return err
	}

	payload := map[string]any{
		"name":        agent.Name,
		"description": agent.Description,
		"tool_groups": agent.ToolGroups,
	}
	if agent.Model != nil {
		payload["model"] = *agent.Model
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), data, 0o644); err != nil {
		return err
	}

	yamlData, err := yaml.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.yaml"), yamlData, 0o644)
}

func (s *Server) deleteAgentFiles(name string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	if err := os.RemoveAll(s.agentDir(name)); err != nil {
		return err
	}
	return nil
}

func (s *Server) agentDir(name string) string {
	return filepath.Join(s.dataRoot, "agents", name)
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
