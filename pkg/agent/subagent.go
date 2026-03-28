package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

// SubagentPool executes bounded subagent tasks.
type SubagentPool struct {
	llm           llm.LLMProvider
	tools         *tools.Registry
	sandbox       *sandbox.Sandbox
	maxConcurrent int
	timeout       time.Duration
	sem           chan struct{}
}

type SubagentConfig struct {
	Name         string
	SystemPrompt string
	AllowedTools []string
	MaxTurns     int
}

func NewSubagentPool(provider llm.LLMProvider, registry *tools.Registry, sb *sandbox.Sandbox, maxConcurrent int, timeout time.Duration) *SubagentPool {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	if registry == nil {
		registry = tools.NewRegistry()
	}
	if sb == nil {
		id := fmt.Sprintf("subagent-%d", time.Now().UnixNano())
		sb, _ = sandbox.New(id, "/tmp/deerflow-sandbox")
	}
	return &SubagentPool{
		llm:           provider,
		tools:         registry,
		sandbox:       sb,
		maxConcurrent: maxConcurrent,
		timeout:       timeout,
		sem:           make(chan struct{}, maxConcurrent),
	}
}

func (p *SubagentPool) Execute(ctx context.Context, task string, cfg SubagentConfig) (string, error) {
	if p == nil || p.llm == nil {
		return "", fmt.Errorf("subagent pool llm provider is required")
	}

	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-p.sem }()

	runCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	restricted := p.tools.Restrict(cfg.AllowedTools)
	agent := New(AgentConfig{
		LLMProvider: p.llm,
		Tools:       restricted,
		MaxTurns:    cfg.MaxTurns,
		Sandbox:     p.sandbox,
	})

	systemPrompt := cfg.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = "You are a focused subagent. Complete the assigned task and return the result."
	}
	if cfg.Name != "" {
		systemPrompt = fmt.Sprintf("Subagent Name: %s\n\n%s", cfg.Name, systemPrompt)
	}

	result, err := agent.Run(runCtx, cfg.Name, []models.Message{
		{
			ID:        newMessageID("system"),
			SessionID: cfg.Name,
			Role:      models.RoleSystem,
			Content:   systemPrompt,
			CreatedAt: time.Now().UTC(),
		},
		{
			ID:        newMessageID("human"),
			SessionID: cfg.Name,
			Role:      models.RoleHuman,
			Content:   task,
			CreatedAt: time.Now().UTC(),
		},
	})
	if err != nil {
		return "", err
	}
	return result.FinalOutput, nil
}
