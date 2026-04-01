package langgraphcompat

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestFilterTransientMessagesDropsViewedImageContext(t *testing.T) {
	messages := []models.Message{
		{
			ID:        "m1",
			SessionID: "thread-1",
			Role:      models.RoleHuman,
			Content:   "user prompt",
		},
		{
			ID:        "m2",
			SessionID: "thread-1",
			Role:      models.RoleHuman,
			Content:   "Here are the images you've viewed:",
			Metadata: map[string]string{
				"transient_viewed_images": "true",
				"multi_content":           `[{"type":"text","text":"Here are the images you've viewed:"}]`,
			},
		},
		{
			ID:        "m3",
			SessionID: "thread-1",
			Role:      models.RoleAI,
			Content:   "final answer",
		},
	}

	filtered := filterTransientMessages(messages)
	if len(filtered) != 2 {
		t.Fatalf("len(filtered)=%d want=2", len(filtered))
	}
	for _, msg := range filtered {
		if msg.ID == "m2" {
			t.Fatalf("transient viewed image message was not removed: %#v", msg)
		}
	}
}

func TestFilterTransientMessagesKeepsRegularMultimodalHumanMessage(t *testing.T) {
	messages := []models.Message{{
		ID:        "m1",
		SessionID: "thread-1",
		Role:      models.RoleHuman,
		Content:   "describe this image",
		Metadata: map[string]string{
			"multi_content": `[{"type":"text","text":"describe this image"}]`,
		},
	}}

	filtered := filterTransientMessages(messages)
	if len(filtered) != 1 {
		t.Fatalf("len(filtered)=%d want=1", len(filtered))
	}
	if filtered[0].ID != "m1" {
		t.Fatalf("kept message=%#v want m1", filtered[0])
	}
}

func TestForwardAgentEventEmitsLangChainToolEndEvent(t *testing.T) {
	s := &Server{
		runs:       map[string]*Run{},
		runStreams: map[string]map[uint64]chan StreamEvent{},
	}
	run := &Run{
		RunID:       "run-1",
		ThreadID:    "thread-1",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	s.forwardAgentEvent(nil, nil, run, agent.AgentEvent{
		Type:      agent.AgentEventToolCallEnd,
		MessageID: "msg-tool-1",
		ToolEvent: &agent.ToolCallEvent{
			ID:          "call-1",
			Name:        "setup_agent",
			Status:      models.CallStatusCompleted,
			CompletedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		Result: &models.ToolResult{
			CallID:   "call-1",
			ToolName: "setup_agent",
			Status:   models.CallStatusCompleted,
			Content:  "saved",
		},
	})

	stored := s.getRun(run.RunID)
	if stored == nil {
		t.Fatal("stored run missing")
	}

	var found bool
	for _, evt := range stored.Events {
		if evt.Event != "events" {
			continue
		}
		payload, ok := evt.Data.(map[string]any)
		if !ok {
			t.Fatalf("events payload type=%T", evt.Data)
		}
		if payload["event"] != "on_tool_end" {
			continue
		}
		if payload["name"] != "setup_agent" {
			t.Fatalf("name=%v want setup_agent", payload["name"])
		}
		if payload["run_id"] != "run-1" {
			t.Fatalf("run_id=%v want run-1", payload["run_id"])
		}
		if payload["thread_id"] != "thread-1" {
			t.Fatalf("thread_id=%v want thread-1", payload["thread_id"])
		}
		data, ok := payload["data"].(*agent.ToolCallEvent)
		if !ok {
			t.Fatalf("data type=%T want *agent.ToolCallEvent", payload["data"])
		}
		if data.Name != "setup_agent" {
			t.Fatalf("tool name=%q want setup_agent", data.Name)
		}
		found = true
	}
	if !found {
		t.Fatal("missing on_tool_end events payload")
	}
}

func TestForwardAgentEventEmitsLangChainToolStartEvent(t *testing.T) {
	s := &Server{
		runs:       map[string]*Run{},
		runStreams: map[string]map[uint64]chan StreamEvent{},
	}
	run := &Run{
		RunID:       "run-2",
		ThreadID:    "thread-2",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	s.forwardAgentEvent(nil, nil, run, agent.AgentEvent{
		Type:      agent.AgentEventToolCallStart,
		MessageID: "msg-tool-2",
		ToolEvent: &agent.ToolCallEvent{
			ID:        "call-2",
			Name:      "setup_agent",
			Status:    models.CallStatusRunning,
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	})

	stored := s.getRun(run.RunID)
	if stored == nil {
		t.Fatal("stored run missing")
	}

	var found bool
	for _, evt := range stored.Events {
		if evt.Event != "events" {
			continue
		}
		payload, ok := evt.Data.(map[string]any)
		if !ok {
			t.Fatalf("events payload type=%T", evt.Data)
		}
		if payload["event"] != "on_tool_start" {
			continue
		}
		if payload["name"] != "setup_agent" {
			t.Fatalf("name=%v want setup_agent", payload["name"])
		}
		if payload["run_id"] != "run-2" {
			t.Fatalf("run_id=%v want run-2", payload["run_id"])
		}
		if payload["thread_id"] != "thread-2" {
			t.Fatalf("thread_id=%v want thread-2", payload["thread_id"])
		}
		data, ok := payload["data"].(*agent.ToolCallEvent)
		if !ok {
			t.Fatalf("data type=%T want *agent.ToolCallEvent", payload["data"])
		}
		if data.Name != "setup_agent" {
			t.Fatalf("tool name=%q want setup_agent", data.Name)
		}
		found = true
	}
	if !found {
		t.Fatal("missing on_tool_start events payload")
	}
}

func TestForwardAgentEventEmitsFinalAssistantMessageTupleWithUsage(t *testing.T) {
	s := &Server{
		runs:       map[string]*Run{},
		runStreams: map[string]map[uint64]chan StreamEvent{},
	}
	run := &Run{
		RunID:       "run-3",
		ThreadID:    "thread-3",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	s.forwardAgentEvent(nil, nil, run, agent.AgentEvent{
		Type:      agent.AgentEventEnd,
		MessageID: "msg-ai-final",
		Text:      "Final answer",
		Usage: &agent.Usage{
			InputTokens:  21,
			OutputTokens: 13,
			TotalTokens:  34,
		},
	})

	stored := s.getRun(run.RunID)
	if stored == nil {
		t.Fatal("stored run missing")
	}

	var found bool
	for _, evt := range stored.Events {
		if evt.Event != "messages-tuple" {
			continue
		}
		payload, ok := evt.Data.(Message)
		if !ok {
			t.Fatalf("payload type=%T want Message", evt.Data)
		}
		if payload.ID != "msg-ai-final" {
			continue
		}
		if payload.Content != "Final answer" {
			t.Fatalf("content=%v want Final answer", payload.Content)
		}
		if payload.UsageMetadata["total_tokens"] != 34 {
			t.Fatalf("usage=%#v want total_tokens=34", payload.UsageMetadata)
		}
		found = true
	}
	if !found {
		t.Fatal("missing final assistant messages-tuple payload")
	}
}

func TestForwardAgentEventEmitsArtifactUpdatesWhenPresentToolCompletes(t *testing.T) {
	s, _ := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-artifacts",
		ThreadID:    "thread-artifacts",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	session := s.ensureSession(run.ThreadID, nil)
	sourcePath := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(sourcePath, []byte("# report\n"), 0o644); err != nil {
		t.Fatalf("write artifact source: %v", err)
	}
	if err := session.PresentFiles.Register(tools.PresentFile{
		Path:       "/mnt/user-data/outputs/report.md",
		SourcePath: sourcePath,
	}); err != nil {
		t.Fatalf("register present file: %v", err)
	}

	rec := httptest.NewRecorder()
	s.forwardAgentEvent(rec, rec, run, agent.AgentEvent{
		Type:      agent.AgentEventToolCallEnd,
		MessageID: "tool-msg-artifacts",
		ToolEvent: &agent.ToolCallEvent{
			ID:          "call-artifacts",
			Name:        "present_files",
			Status:      models.CallStatusCompleted,
			CompletedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		Result: &models.ToolResult{
			CallID:   "call-artifacts",
			ToolName: "present_files",
			Status:   models.CallStatusCompleted,
			Content:  "Presented 1 file(s)",
		},
	})

	body := rec.Body.String()
	if !strings.Contains(body, "event: updates") {
		t.Fatalf("expected updates event in %q", body)
	}
	if !strings.Contains(body, `"artifacts":["/mnt/user-data/outputs/report.md"]`) {
		t.Fatalf("expected artifact update payload in %q", body)
	}
}
