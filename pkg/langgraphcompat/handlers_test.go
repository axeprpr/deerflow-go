package langgraphcompat

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
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

func TestFilterTransientMessagesStripsInjectedUploadedImageDataURLs(t *testing.T) {
	messages := []models.Message{{
		ID:        "m1",
		SessionID: "thread-1",
		Role:      models.RoleHuman,
		Content:   "<uploaded_files>\n- diagram.png (1.0 KB)\n  Path: /mnt/user-data/uploads/diagram.png\n</uploaded_files>\n\ndescribe this image",
		Metadata: map[string]string{
			"additional_kwargs": `{"files":[{"filename":"diagram.png","size":1024}]}`,
			"multi_content":     `[{"type":"text","text":"<uploaded_files>\n- diagram.png (1.0 KB)\n  Path: /mnt/user-data/uploads/diagram.png\n</uploaded_files>\n\ndescribe this image"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]`,
		},
	}}

	filtered := filterTransientMessages(messages)
	if len(filtered) != 1 {
		t.Fatalf("len(filtered)=%d want=1", len(filtered))
	}
	multi := decodeMultiContent(filtered[0].Metadata)
	if len(multi) != 1 {
		t.Fatalf("multi_content len=%d want=1", len(multi))
	}
	if got := multi[0]["type"]; got != "text" {
		t.Fatalf("multi_content[0].type=%v want text", got)
	}
	if kwargs := decodeAdditionalKwargs(filtered[0].Metadata); kwargs == nil {
		t.Fatal("expected additional_kwargs to be preserved")
	}
}

func TestFilterTransientMessagesKeepsRegularMultimodalImagesWithoutUploadContext(t *testing.T) {
	messages := []models.Message{{
		ID:        "m1",
		SessionID: "thread-1",
		Role:      models.RoleHuman,
		Content:   "describe this image",
		Metadata: map[string]string{
			"multi_content": `[{"type":"text","text":"describe this image"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]`,
		},
	}}

	filtered := filterTransientMessages(messages)
	if len(filtered) != 1 {
		t.Fatalf("len(filtered)=%d want=1", len(filtered))
	}
	multi := decodeMultiContent(filtered[0].Metadata)
	if len(multi) != 2 {
		t.Fatalf("multi_content len=%d want=2", len(multi))
	}
	if got := multi[1]["type"]; got != "image_url" {
		t.Fatalf("multi_content[1].type=%v want image_url", got)
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

	s.forwardAgentEvent(nil, nil, run, newStreamModeFilter(nil), agent.AgentEvent{
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

func TestForwardAgentEventEmitsCreatedAgentNameUpdateAfterSetupAgent(t *testing.T) {
	s := &Server{
		sessions:   map[string]*Session{},
		runs:       map[string]*Run{},
		runStreams: map[string]map[uint64]chan StreamEvent{},
	}
	s.ensureSession("thread-bootstrap", nil)
	s.setThreadValue("thread-bootstrap", "created_agent_name", "code-reviewer")

	run := &Run{
		RunID:       "run-bootstrap",
		ThreadID:    "thread-bootstrap",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	s.forwardAgentEvent(nil, nil, run, newStreamModeFilter(nil), agent.AgentEvent{
		Type:      agent.AgentEventToolCallEnd,
		MessageID: "msg-tool-bootstrap",
		ToolEvent: &agent.ToolCallEvent{
			ID:          "call-bootstrap",
			Name:        "setup_agent",
			Status:      models.CallStatusCompleted,
			CompletedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		Result: &models.ToolResult{
			CallID:   "call-bootstrap",
			ToolName: "setup_agent",
			Status:   models.CallStatusCompleted,
			Content:  "Agent 'code-reviewer' created successfully!",
		},
	})

	stored := s.getRun(run.RunID)
	if stored == nil {
		t.Fatal("stored run missing")
	}

	var found bool
	for _, evt := range stored.Events {
		if evt.Event != "updates" {
			continue
		}
		payload, ok := evt.Data.(map[string]any)
		if !ok {
			t.Fatalf("updates payload type=%T", evt.Data)
		}
		agentUpdate, ok := payload["agent"].(map[string]any)
		if !ok {
			t.Fatalf("agent update payload=%#v", payload["agent"])
		}
		if got := stringFromAny(agentUpdate["created_agent_name"]); got != "code-reviewer" {
			continue
		}
		found = true
	}
	if !found {
		t.Fatal("missing created_agent_name updates payload")
	}
}

func TestForwardAgentEventDoesNotEmitCreatedAgentNameUpdateAfterFailedSetupAgent(t *testing.T) {
	s := &Server{
		sessions:   map[string]*Session{},
		runs:       map[string]*Run{},
		runStreams: map[string]map[uint64]chan StreamEvent{},
	}
	s.ensureSession("thread-bootstrap", nil)
	s.setThreadValue("thread-bootstrap", "created_agent_name", "code-reviewer")

	run := &Run{
		RunID:       "run-bootstrap-failed",
		ThreadID:    "thread-bootstrap",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	s.forwardAgentEvent(nil, nil, run, newStreamModeFilter(nil), agent.AgentEvent{
		Type:      agent.AgentEventToolCallEnd,
		MessageID: "msg-tool-bootstrap-failed",
		ToolEvent: &agent.ToolCallEvent{
			ID:          "call-bootstrap-failed",
			Name:        "setup_agent",
			Status:      models.CallStatusFailed,
			CompletedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Error:       "agent already exists",
		},
		Result: &models.ToolResult{
			CallID:   "call-bootstrap-failed",
			ToolName: "setup_agent",
			Status:   models.CallStatusFailed,
			Error:    "agent already exists",
		},
	})

	stored := s.getRun(run.RunID)
	if stored == nil {
		t.Fatal("stored run missing")
	}

	for _, evt := range stored.Events {
		if evt.Event != "updates" {
			continue
		}
		payload, ok := evt.Data.(map[string]any)
		if !ok {
			t.Fatalf("updates payload type=%T", evt.Data)
		}
		agentUpdate, ok := payload["agent"].(map[string]any)
		if !ok {
			continue
		}
		if got := stringFromAny(agentUpdate["created_agent_name"]); got != "" {
			t.Fatalf("unexpected created_agent_name update=%q", got)
		}
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

	s.forwardAgentEvent(nil, nil, run, newStreamModeFilter(nil), agent.AgentEvent{
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

func TestForwardTaskEventIncludesStructuredMessageMetadata(t *testing.T) {
	s := &Server{
		runs:       map[string]*Run{},
		runStreams: map[string]map[uint64]chan StreamEvent{},
	}
	run := &Run{
		RunID:       "run-task-1",
		ThreadID:    "thread-task-1",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	s.forwardTaskEvent(nil, nil, run, newStreamModeFilter(nil), subagent.TaskEvent{
		Type:          "task_running",
		TaskID:        "task-1",
		RequestID:     "subreq-1",
		Description:   "inspect repo",
		Message:       map[string]any{"id": "ai-1", "type": "ai", "tool_calls": []map[string]any{{"id": "call-1", "name": "bash"}}},
		MessageIndex:  1,
		TotalMessages: 2,
	})

	stored := s.getRun(run.RunID)
	if stored == nil {
		t.Fatal("stored run missing")
	}

	var found bool
	for _, evt := range stored.Events {
		if evt.Event != "task_running" {
			continue
		}
		payload, ok := evt.Data.(map[string]any)
		if !ok {
			t.Fatalf("task payload type=%T", evt.Data)
		}
		if payload["request_id"] != "subreq-1" {
			t.Fatalf("request_id=%v want subreq-1", payload["request_id"])
		}
		if payload["message_index"] != 1 {
			t.Fatalf("message_index=%v want 1", payload["message_index"])
		}
		if payload["total_messages"] != 2 {
			t.Fatalf("total_messages=%v want 2", payload["total_messages"])
		}
		message, ok := payload["message"].(map[string]any)
		if !ok {
			t.Fatalf("message type=%T want map[string]any", payload["message"])
		}
		toolCalls, ok := message["tool_calls"].([]map[string]any)
		if !ok || len(toolCalls) != 1 {
			t.Fatalf("tool_calls=%#v want single call", message["tool_calls"])
		}
		if toolCalls[0]["name"] != "bash" {
			t.Fatalf("tool name=%v want bash", toolCalls[0]["name"])
		}
		found = true
	}
	if !found {
		t.Fatal("missing task_running event")
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

	s.forwardAgentEvent(nil, nil, run, newStreamModeFilter(nil), agent.AgentEvent{
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

func TestForwardAgentEventEmitsNormalizedFinalAssistantText(t *testing.T) {
	s := &Server{
		runs:       map[string]*Run{},
		runStreams: map[string]map[uint64]chan StreamEvent{},
	}
	run := &Run{
		RunID:       "run-think",
		ThreadID:    "thread-think",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	s.forwardAgentEvent(nil, nil, run, newStreamModeFilter(nil), agent.AgentEvent{
		Type:      agent.AgentEventEnd,
		MessageID: "msg-ai-think",
		Text:      "Visible answer",
	})

	stored := s.getRun(run.RunID)
	if stored == nil {
		t.Fatal("stored run missing")
	}

	for _, evt := range stored.Events {
		if evt.Event != "messages-tuple" {
			continue
		}
		payload, ok := evt.Data.(Message)
		if !ok || payload.ID != "msg-ai-think" {
			continue
		}
		if payload.Content != "Visible answer" {
			t.Fatalf("content=%v want Visible answer", payload.Content)
		}
		return
	}

	t.Fatal("missing normalized assistant messages-tuple payload")
}

func TestForwardAgentEventRewritesFinalAssistantArtifactLinks(t *testing.T) {
	s := &Server{
		runs:       map[string]*Run{},
		runStreams: map[string]map[uint64]chan StreamEvent{},
	}
	run := &Run{
		RunID:       "run-artifact-links",
		ThreadID:    "thread-artifact-links",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	s.forwardAgentEvent(nil, nil, run, newStreamModeFilter(nil), agent.AgentEvent{
		Type:      agent.AgentEventEnd,
		MessageID: "msg-ai-artifact-links",
		Text:      "Open [artifact](/mnt/user-data/outputs/final report.md)",
	})

	stored := s.getRun(run.RunID)
	if stored == nil {
		t.Fatal("stored run missing")
	}

	for _, evt := range stored.Events {
		if evt.Event != "messages-tuple" {
			continue
		}
		payload, ok := evt.Data.(Message)
		if !ok || payload.ID != "msg-ai-artifact-links" {
			continue
		}
		if payload.Content != "Open [artifact](/api/threads/thread-artifact-links/artifacts/mnt/user-data/outputs/final%20report.md)" {
			t.Fatalf("content=%v want rewritten artifact url", payload.Content)
		}
		return
	}

	t.Fatal("missing rewritten final assistant messages-tuple payload")
}

func TestForwardAgentEventPreservesFinalAssistantAdditionalKwargs(t *testing.T) {
	s := &Server{
		runs:       map[string]*Run{},
		runStreams: map[string]map[uint64]chan StreamEvent{},
	}
	run := &Run{
		RunID:       "run-think-meta",
		ThreadID:    "thread-think-meta",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	s.forwardAgentEvent(nil, nil, run, newStreamModeFilter(nil), agent.AgentEvent{
		Type:      agent.AgentEventEnd,
		MessageID: "msg-ai-think-meta",
		Text:      "Visible answer",
		Metadata: map[string]string{
			"additional_kwargs": `{"reasoning_content":"internal reasoning"}`,
		},
	})

	stored := s.getRun(run.RunID)
	if stored == nil {
		t.Fatal("stored run missing")
	}

	for _, evt := range stored.Events {
		if evt.Event != "messages-tuple" {
			continue
		}
		payload, ok := evt.Data.(Message)
		if !ok || payload.ID != "msg-ai-think-meta" {
			continue
		}
		if payload.Content != "Visible answer" {
			t.Fatalf("content=%v want Visible answer", payload.Content)
		}
		if payload.AdditionalKwargs["reasoning_content"] != "internal reasoning" {
			t.Fatalf("additional_kwargs=%#v want reasoning_content", payload.AdditionalKwargs)
		}
		return
	}

	t.Fatal("missing final assistant messages-tuple payload with additional_kwargs")
}

func TestForwardAgentEventEmitsReasoningOnlyAssistantMessage(t *testing.T) {
	s := &Server{
		runs:       map[string]*Run{},
		runStreams: map[string]map[uint64]chan StreamEvent{},
	}
	run := &Run{
		RunID:       "run-think-only",
		ThreadID:    "thread-think-only",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	s.forwardAgentEvent(nil, nil, run, newStreamModeFilter(nil), agent.AgentEvent{
		Type:      agent.AgentEventEnd,
		MessageID: "msg-ai-think-only",
		Metadata: map[string]string{
			"additional_kwargs": `{"reasoning_content":"internal reasoning"}`,
		},
	})

	stored := s.getRun(run.RunID)
	if stored == nil {
		t.Fatal("stored run missing")
	}

	for _, evt := range stored.Events {
		if evt.Event != "messages-tuple" {
			continue
		}
		payload, ok := evt.Data.(Message)
		if !ok || payload.ID != "msg-ai-think-only" {
			continue
		}
		if payload.Content != "" {
			t.Fatalf("content=%v want empty", payload.Content)
		}
		if payload.AdditionalKwargs["reasoning_content"] != "internal reasoning" {
			t.Fatalf("additional_kwargs=%#v want reasoning_content", payload.AdditionalKwargs)
		}
		return
	}

	t.Fatal("missing reasoning-only messages-tuple payload")
}

func TestUsagePayloadFromAgentUsageDefaultsToZero(t *testing.T) {
	got := usagePayloadFromAgentUsage(nil)
	want := map[string]int{
		"input_tokens":        0,
		"output_tokens":       0,
		"total_tokens":        0,
		"reasoning_tokens":    0,
		"cached_input_tokens": 0,
	}
	if got["input_tokens"] != want["input_tokens"] || got["output_tokens"] != want["output_tokens"] || got["total_tokens"] != want["total_tokens"] || got["reasoning_tokens"] != want["reasoning_tokens"] || got["cached_input_tokens"] != want["cached_input_tokens"] {
		t.Fatalf("usage=%#v want %#v", got, want)
	}
}

func TestUsagePayloadFromAgentUsagePreservesCounts(t *testing.T) {
	got := usagePayloadFromAgentUsage(&agent.Usage{
		InputTokens:       150,
		OutputTokens:      25,
		TotalTokens:       175,
		ReasoningTokens:   11,
		CachedInputTokens: 42,
	})
	if got["input_tokens"] != 150 || got["output_tokens"] != 25 || got["total_tokens"] != 175 || got["reasoning_tokens"] != 11 || got["cached_input_tokens"] != 42 {
		t.Fatalf("usage=%#v", got)
	}
}

func TestEndEventJSONIncludesZeroUsageWhenMissing(t *testing.T) {
	payload := map[string]any{
		"run_id": "run-no-usage",
		"usage":  usagePayloadFromAgentUsage(nil),
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `"usage":{"cached_input_tokens":0,"input_tokens":0,"output_tokens":0,"reasoning_tokens":0,"total_tokens":0}`) {
		t.Fatalf("payload=%s", text)
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
	s.forwardAgentEvent(rec, rec, run, newStreamModeFilter(nil), agent.AgentEvent{
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

func TestForwardAgentEventEmitsArtifactUpdatesWhenWriteFileCompletes(t *testing.T) {
	s, _ := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-write-file-artifacts",
		ThreadID:    "thread-write-file-artifacts",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)
	s.ensureSession(run.ThreadID, nil)

	outputDir := filepath.Join(s.threadRoot(run.ThreadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "report.md"), []byte("# report\n"), 0o644); err != nil {
		t.Fatalf("write output artifact: %v", err)
	}

	rec := httptest.NewRecorder()
	s.forwardAgentEvent(rec, rec, run, newStreamModeFilter(nil), agent.AgentEvent{
		Type:      agent.AgentEventToolCallEnd,
		MessageID: "tool-msg-write-file-artifacts",
		ToolEvent: &agent.ToolCallEvent{
			ID:          "call-write-file-artifacts",
			Name:        "write_file",
			Status:      models.CallStatusCompleted,
			CompletedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		Result: &models.ToolResult{
			CallID:   "call-write-file-artifacts",
			ToolName: "write_file",
			Status:   models.CallStatusCompleted,
			Content:  "wrote /mnt/user-data/outputs/report.md",
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

func TestForwardAgentEventEmitsArtifactUpdatesWhenBashCompletes(t *testing.T) {
	s, _ := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-bash-artifacts",
		ThreadID:    "thread-bash-artifacts",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)
	s.ensureSession(run.ThreadID, nil)

	outputDir := filepath.Join(s.threadRoot(run.ThreadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "site.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("write output artifact: %v", err)
	}

	rec := httptest.NewRecorder()
	s.forwardAgentEvent(rec, rec, run, newStreamModeFilter(nil), agent.AgentEvent{
		Type:      agent.AgentEventToolCallEnd,
		MessageID: "tool-msg-bash-artifacts",
		ToolEvent: &agent.ToolCallEvent{
			ID:          "call-bash-artifacts",
			Name:        "bash",
			Status:      models.CallStatusCompleted,
			CompletedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		Result: &models.ToolResult{
			CallID:   "call-bash-artifacts",
			ToolName: "bash",
			Status:   models.CallStatusCompleted,
			Content:  "generated /mnt/user-data/outputs/site.html",
		},
	})

	body := rec.Body.String()
	if !strings.Contains(body, "event: updates") {
		t.Fatalf("expected updates event in %q", body)
	}
	if !strings.Contains(body, `"artifacts":["/mnt/user-data/outputs/site.html"]`) {
		t.Fatalf("expected artifact update payload in %q", body)
	}
}

func TestToolMayAffectArtifacts(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "bash", want: true},
		{name: "write_file", want: true},
		{name: "str_replace", want: true},
		{name: "task", want: true},
		{name: "invoke_acp_agent", want: true},
		{name: " present_file ", want: false},
		{name: "write_todos", want: false},
		{name: "web_search", want: false},
		{name: "", want: false},
	}

	for _, tt := range tests {
		if got := toolMayAffectArtifacts(tt.name); got != tt.want {
			t.Fatalf("toolMayAffectArtifacts(%q)=%v want %v", tt.name, got, tt.want)
		}
	}
}

func TestResolvedToolNameForArtifactsPrefersResultNameButFallsBackToToolEvent(t *testing.T) {
	tests := []struct {
		name string
		evt  agent.AgentEvent
		want string
	}{
		{
			name: "result name wins",
			evt: agent.AgentEvent{
				Result:    &models.ToolResult{ToolName: "write_file"},
				ToolEvent: &agent.ToolCallEvent{Name: "bash"},
			},
			want: "write_file",
		},
		{
			name: "tool event fallback",
			evt: agent.AgentEvent{
				Result:    &models.ToolResult{},
				ToolEvent: &agent.ToolCallEvent{Name: " task "},
			},
			want: "task",
		},
		{
			name: "empty",
			evt:  agent.AgentEvent{},
			want: "",
		},
	}

	for _, tt := range tests {
		if got := resolvedToolNameForArtifacts(tt.evt); got != tt.want {
			t.Fatalf("%s: resolvedToolNameForArtifacts()=%q want %q", tt.name, got, tt.want)
		}
	}
}

func TestThreadMetadataFromRuntimeContextPrefersRuntimeAgentName(t *testing.T) {
	metadata := threadMetadataFromRuntimeContext(map[string]any{
		"agent_name": "release-bot",
	}, runConfig{
		AgentName: "fallback-bot",
		AgentType: agent.AgentTypeCoder,
	})

	if got := stringValue(metadata["agent_name"]); got != "release-bot" {
		t.Fatalf("agent_name=%q want=%q", got, "release-bot")
	}
	if got := stringValue(metadata["agent_type"]); got != string(agent.AgentTypeCoder) {
		t.Fatalf("agent_type=%q want=%q", got, agent.AgentTypeCoder)
	}
}

func TestThreadMetadataFromRuntimeContextReturnsNilWhenEmpty(t *testing.T) {
	if metadata := threadMetadataFromRuntimeContext(nil, runConfig{}); metadata != nil {
		t.Fatalf("metadata=%#v want nil", metadata)
	}
}
