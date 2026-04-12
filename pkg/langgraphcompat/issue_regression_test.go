package langgraphcompat

import (
	"context"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestIssue2143And2125CustomSkillUsesMountedSkillPath(t *testing.T) {
	s := newFileCompatTestServer(t)
	writeGatewaySkill(t, s.dataRoot, "custom", "scenic-spot-analytics", `---
name: scenic-spot-analytics
description: Query scenic spot analytics data
---

# Scenic Spot Analytics
`)

	cfg, err := s.resolveRunConfig(runConfig{}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig() error = %v", err)
	}

	if !strings.Contains(cfg.SystemPrompt, "/mnt/skills/custom/scenic-spot-analytics/SKILL.md") {
		t.Fatalf("system prompt missing mounted custom skill path: %q", cfg.SystemPrompt)
	}
	if strings.Contains(cfg.SystemPrompt, "/mnt/user-data/scenic-spot-analytics") ||
		strings.Contains(cfg.SystemPrompt, "/mnt/user-data/uploads/scenic-spot-analytics") ||
		strings.Contains(cfg.SystemPrompt, "/mnt/user-data/workspace/scenic-spot-analytics") {
		t.Fatalf("system prompt should not route custom skill through user-data: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "Query scenic spot analytics data") {
		t.Fatalf("system prompt missing custom skill description: %q", cfg.SystemPrompt)
	}

	paths := s.runtimeSkillPaths()
	if got := paths["scenic-spot-analytics"]; got != "/mnt/skills/custom/scenic-spot-analytics/SKILL.md" {
		t.Fatalf("runtimeSkillPaths[scenic-spot-analytics]=%q", got)
	}
}

func TestIssue2139DefaultRuntimeExposesWebSearchTools(t *testing.T) {
	runtime := harnessruntime.NewDefaultToolRuntime(nil, clarification.NewManager(4), nil)
	if runtime == nil || runtime.Registry() == nil {
		t.Fatal("runtime tool registry is not available")
	}

	for _, name := range []string{"web_search", "web_fetch", "image_search"} {
		if runtime.Registry().Get(name) == nil {
			t.Fatalf("default runtime tool surface missing %s", name)
		}
	}
}

func TestIssue2126CompactionKeepsCompletedOutputStateForRemainingWrite(t *testing.T) {
	threadID := "issue-2126"
	summaryLLM := &summaryProvider{
		response: "Older investigation context was compacted. Keep the completed markdown report and only finish the remaining JSON output.",
	}
	server := &Server{
		llmProvider:  summaryLLM,
		defaultModel: "summary-model",
		sessions:     map[string]*Session{},
		runs:         map[string]*Run{},
	}
	server.ensureSession(threadID, nil)

	state := &harness.RunState{
		ThreadID: threadID,
		Model:    "run-model",
		Messages: buildIssue2126LongHistory(threadID),
		Metadata: map[string]any{},
	}
	if err := server.beforeRunSummarizationFeature(context.Background(), state); err != nil {
		t.Fatalf("beforeRunSummarizationFeature() error = %v", err)
	}
	if got := len(state.Messages); got != defaultSummaryKeepMessages {
		t.Fatalf("kept messages=%d want=%d", got, defaultSummaryKeepMessages)
	}
	if !strings.Contains(summaryLLM.lastReq.Messages[0].Content, "deployment checkpoint") {
		t.Fatalf("summary prompt=%q want compacted older context", summaryLLM.lastReq.Messages[0].Content)
	}
	server.afterRunSummarizationFeature(state)
	if got := server.threadHistorySummary(threadID); got != summaryLLM.response {
		t.Fatalf("persisted summary=%q want=%q", got, summaryLLM.response)
	}
	if !messagesContainCompletedWrite(state.Messages, "/mnt/user-data/outputs/orders-summary.md") {
		t.Fatal("compacted history lost completed markdown output marker")
	}
	if messagesContainCompletedWrite(state.Messages, "/mnt/user-data/outputs/orders-summary.json") {
		t.Fatal("compacted history should not claim JSON output is already complete")
	}

	registry := tools.NewRegistry()
	var writes []string
	if err := registry.Register(models.Tool{
		Name:        "write_file",
		Description: "write_file",
		Handler: func(_ context.Context, call models.ToolCall) (models.ToolResult, error) {
			path := strings.TrimSpace(stringValue(call.Arguments["path"]))
			writes = append(writes, path)
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusCompleted,
				Content:  "Successfully wrote " + path,
			}, nil
		},
	}); err != nil {
		t.Fatalf("register write_file: %v", err)
	}

	provider := &issue2126ChatProvider{t: t}
	runAgent := agent.New(agent.AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    4,
	})

	result, err := runAgent.Run(context.Background(), threadID, append([]models.Message(nil), state.Messages...))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalOutput != "done" {
		t.Fatalf("FinalOutput=%q want done", result.FinalOutput)
	}
	if len(writes) != 1 || writes[0] != "/mnt/user-data/outputs/orders-summary.json" {
		t.Fatalf("writes=%v want only remaining JSON output", writes)
	}
}

type issue2126ChatProvider struct {
	t     *testing.T
	calls int
}

func (p *issue2126ChatProvider) PrefersStructuredToolCalls() bool { return true }

func (p *issue2126ChatProvider) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	p.calls++
	switch p.calls {
	case 1:
		if !messagesContainCompletedWrite(req.Messages, "/mnt/user-data/outputs/orders-summary.md") {
			p.t.Fatalf("messages lost completed markdown output: %#v", req.Messages)
		}
		if messagesContainCompletedWrite(req.Messages, "/mnt/user-data/outputs/orders-summary.json") {
			p.t.Fatalf("messages should not show completed JSON output: %#v", req.Messages)
		}
		return llm.ChatResponse{
			Model: "test-model",
			Message: models.Message{
				Role: models.RoleAI,
				ToolCalls: []models.ToolCall{{
					ID:   "write-json",
					Name: "write_file",
					Arguments: map[string]any{
						"path":    "/mnt/user-data/outputs/orders-summary.json",
						"content": "{\"status\":\"ok\"}",
					},
				}},
			},
			Stop: "tool_calls",
		}, nil
	case 2:
		if !messagesContainCompletedWrite(req.Messages, "/mnt/user-data/outputs/orders-summary.md") {
			p.t.Fatalf("messages lost completed markdown output after continuation: %#v", req.Messages)
		}
		if !messagesContainCompletedWrite(req.Messages, "/mnt/user-data/outputs/orders-summary.json") {
			p.t.Fatalf("messages missing completed JSON output after write: %#v", req.Messages)
		}
		return llm.ChatResponse{
			Model: "test-model",
			Message: models.Message{
				Role:    models.RoleAI,
				Content: "done",
			},
			Stop: "stop",
		}, nil
	default:
		p.t.Fatalf("unexpected Chat call %d", p.calls)
		return llm.ChatResponse{}, nil
	}
}

func (p *issue2126ChatProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	if p.t != nil {
		p.t.Fatal("Stream() should not be used for structured tool turns")
	}
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func buildIssue2126LongHistory(threadID string) []models.Message {
	messages := make([]models.Message, 0, 34)
	for i := 0; i < 22; i++ {
		role := models.RoleHuman
		if i%2 == 1 {
			role = models.RoleAI
		}
		messages = append(messages, models.Message{
			ID:        "old-" + string(rune('a'+(i%26))),
			SessionID: threadID,
			Role:      role,
			Content:   strings.Repeat("deployment checkpoint ", 18),
		})
	}
	messages = append(messages,
		models.Message{
			ID:        "csv-request",
			SessionID: threadID,
			Role:      models.RoleHuman,
			Content:   "我上传了 orders_dirty.csv。请继续分析，保留已经完成的步骤。",
		},
		models.Message{
			ID:        "read-call",
			SessionID: threadID,
			Role:      models.RoleAI,
			Content:   "先读取 CSV。",
			ToolCalls: []models.ToolCall{{
				ID:   "read-orders",
				Name: "read_file",
				Arguments: map[string]any{
					"path": "/mnt/user-data/uploads/orders_dirty.csv",
				},
			}},
		},
		models.Message{
			ID:        "read-result",
			SessionID: threadID,
			Role:      models.RoleTool,
			ToolResult: &models.ToolResult{
				CallID:   "read-orders",
				ToolName: "read_file",
				Status:   models.CallStatusCompleted,
				Content:  "Loaded /mnt/user-data/uploads/orders_dirty.csv with duplicate rows and blank sales cells.",
			},
		},
		models.Message{
			ID:        "write-md-call",
			SessionID: threadID,
			Role:      models.RoleAI,
			Content:   "先写 Markdown 汇总。",
			ToolCalls: []models.ToolCall{{
				ID:   "write-md",
				Name: "write_file",
				Arguments: map[string]any{
					"path":    "/mnt/user-data/outputs/orders-summary.md",
					"content": "# Orders Summary",
				},
			}},
		},
		models.Message{
			ID:        "write-md-result",
			SessionID: threadID,
			Role:      models.RoleTool,
			ToolResult: &models.ToolResult{
				CallID:   "write-md",
				ToolName: "write_file",
				Status:   models.CallStatusCompleted,
				Content:  "Successfully wrote /mnt/user-data/outputs/orders-summary.md",
			},
		},
		models.Message{
			ID:        "after-md",
			SessionID: threadID,
			Role:      models.RoleAI,
			Content:   "Markdown 汇总已完成，下一步只剩 orders-summary.json。",
		},
		models.Message{
			ID:        "continue-1",
			SessionID: threadID,
			Role:      models.RoleHuman,
			Content:   "继续，只生成剩余的 JSON，不要重写已经完成的 Markdown。",
		},
		models.Message{
			ID:        "continue-2",
			SessionID: threadID,
			Role:      models.RoleAI,
			Content:   "收到，我会复用已完成的 Markdown 结果。",
		},
		models.Message{
			ID:        "continue-3",
			SessionID: threadID,
			Role:      models.RoleHuman,
			Content:   "必要时可以参考 orders_dirty.csv，但保持已完成输出不变。",
		},
		models.Message{
			ID:        "continue-4",
			SessionID: threadID,
			Role:      models.RoleAI,
			Content:   "明白，只补齐缺失的 JSON 文件。",
		},
		models.Message{
			ID:        "continue-5",
			SessionID: threadID,
			Role:      models.RoleHuman,
			Content:   "继续。",
		},
		models.Message{
			ID:        "continue-6",
			SessionID: threadID,
			Role:      models.RoleAI,
			Content:   "继续处理中。",
		},
	)
	return messages
}

func messagesContainCompletedWrite(messages []models.Message, path string) bool {
	for _, msg := range messages {
		if msg.Role != models.RoleTool || msg.ToolResult == nil {
			continue
		}
		if msg.ToolResult.ToolName != "write_file" || msg.ToolResult.Status != models.CallStatusCompleted {
			continue
		}
		if strings.Contains(msg.ToolResult.Content, path) {
			return true
		}
	}
	return false
}
