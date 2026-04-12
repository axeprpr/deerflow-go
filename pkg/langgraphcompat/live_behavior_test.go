package langgraphcompat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type liveBehaviorThread struct {
	ThreadID string `json:"thread_id"`
	Status   string `json:"status"`
	Values   struct {
		Artifacts []string          `json:"artifacts"`
		Messages  []json.RawMessage `json:"messages"`
	} `json:"values"`
}

type liveBehaviorMessage struct {
	Role       string                 `json:"role"`
	Type       string                 `json:"type"`
	Name       string                 `json:"name"`
	Status     string                 `json:"status"`
	Content    any                    `json:"content"`
	ToolCallID string                 `json:"tool_call_id"`
	ToolCalls  []liveBehaviorToolCall `json:"tool_calls"`
}

type liveBehaviorToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

func requireLiveBehaviorBaseURL(t *testing.T) string {
	t.Helper()
	if os.Getenv("DEERFLOW_LIVE_BEHAVIOR") == "" {
		t.Skip("set DEERFLOW_LIVE_BEHAVIOR=1 to run live behavior regression tests")
	}
	baseURL := strings.TrimSpace(os.Getenv("DEERFLOW_LIVE_BASE_URL"))
	if baseURL == "" {
		t.Skip("set DEERFLOW_LIVE_BASE_URL to the running gateway base URL, for example http://127.0.0.1:18080")
	}
	return strings.TrimRight(baseURL, "/")
}

func runLiveBehaviorThread(t *testing.T, baseURL, threadID, prompt string) liveBehaviorThread {
	t.Helper()

	payload := map[string]any{
		"assistant_id": "lead_agent",
		"input": map[string]any{
			"messages": []any{
				map[string]any{
					"type": "human",
					"content": []any{
						map[string]any{
							"type": "text",
							"text": prompt,
						},
					},
					"additional_kwargs": map[string]any{},
				},
			},
		},
		"stream_mode":      []string{"messages-tuple", "events"},
		"stream_subgraphs": true,
		"stream_resumable": true,
		"context": map[string]any{
			"mode":             "flash",
			"model_name":       "Qwen/Qwen3.5-27B",
			"thinking_enabled": false,
			"is_plan_mode":     false,
			"subagent_enabled": false,
			"reasoning_effort": "minimal",
		},
		"config": map[string]any{
			"recursion_limit": 1000,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	client := &http.Client{Timeout: 4 * time.Minute}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/langgraph/threads/%s/runs/stream", baseURL, threadID), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new stream request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		streamBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		t.Fatalf("stream status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(streamBody)))
	}
	streamBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read stream body: %v", err)
	}
	if !bytes.Contains(streamBody, []byte("\nevent: end\n")) && !bytes.HasSuffix(bytes.TrimSpace(streamBody), []byte("event: end")) {
		t.Fatalf("stream missing end event: %s", string(streamBody))
	}

	threadResp, err := client.Get(fmt.Sprintf("%s/api/threads/%s", baseURL, threadID))
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}
	defer threadResp.Body.Close()
	if threadResp.StatusCode != http.StatusOK {
		threadBody, _ := io.ReadAll(io.LimitReader(threadResp.Body, 8192))
		t.Fatalf("thread status=%d body=%s", threadResp.StatusCode, strings.TrimSpace(string(threadBody)))
	}

	var thread liveBehaviorThread
	if err := json.NewDecoder(threadResp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	return thread
}

func uploadLiveBehaviorFile(t *testing.T, baseURL, threadID, filename string, content []byte) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", filename)
	if err != nil {
		t.Fatalf("create upload part: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write upload body: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/threads/%s/uploads", baseURL, threadID), &body)
	if err != nil {
		t.Fatalf("new upload request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		uploadBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		t.Fatalf("upload status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(uploadBody)))
	}
}

func decodeLiveBehaviorMessages(t *testing.T, raws []json.RawMessage) []liveBehaviorMessage {
	t.Helper()
	out := make([]liveBehaviorMessage, 0, len(raws))
	for _, raw := range raws {
		var msg liveBehaviorMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("decode message: %v", err)
		}
		out = append(out, msg)
	}
	return out
}

func liveBehaviorContentString(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		var b strings.Builder
		for _, part := range value {
			item, _ := part.(map[string]any)
			if text, _ := item["text"].(string); text != "" {
				b.WriteString(text)
			}
		}
		return b.String()
	default:
		return ""
	}
}

func TestLiveBehaviorConcretePromptExecutesWithoutClarification(t *testing.T) {
	baseURL := requireLiveBehaviorBaseURL(t)
	threadID := uuid.NewString()
	thread := runLiveBehaviorThread(t, baseURL, threadID, "帮我生成一个小鱼游泳的页面")
	if thread.Status != "idle" {
		t.Fatalf("thread status=%q want idle", thread.Status)
	}
	if len(thread.Values.Artifacts) == 0 || thread.Values.Artifacts[0] != "/mnt/user-data/outputs/index.html" {
		t.Fatalf("artifacts=%v want index.html", thread.Values.Artifacts)
	}

	messages := decodeLiveBehaviorMessages(t, thread.Values.Messages)
	var (
		sawSkillRead         bool
		sawClarificationTool bool
		finalAssistant       string
	)
	for _, msg := range messages {
		for _, call := range msg.ToolCalls {
			if call.Name == "read_file" {
				sawSkillRead = true
			}
			if call.Name == "ask_clarification" {
				sawClarificationTool = true
			}
		}
		content := strings.TrimSpace(liveBehaviorContentString(msg.Content))
		if (msg.Role == "assistant" || msg.Type == "ai") && content != "" {
			finalAssistant = content
		}
	}
	if !sawSkillRead {
		t.Fatal("expected concrete prompt to load a skill via read_file")
	}
	if sawClarificationTool {
		t.Fatal("concrete prompt unexpectedly emitted ask_clarification")
	}
	if strings.Contains(strings.ToLower(finalAssistant), "clarification") {
		t.Fatalf("final assistant reply should not ask for clarification: %q", finalAssistant)
	}
}

func TestLiveBehaviorAmbiguousPromptRequestsMoreDetail(t *testing.T) {
	baseURL := requireLiveBehaviorBaseURL(t)
	threadID := uuid.NewString()
	thread := runLiveBehaviorThread(t, baseURL, threadID, "帮我做一个页面")
	if thread.Status != "idle" && thread.Status != "interrupted" {
		t.Fatalf("thread status=%q want idle or interrupted", thread.Status)
	}
	if len(thread.Values.Artifacts) != 0 {
		t.Fatalf("ambiguous prompt should not create artifacts, got %v", thread.Values.Artifacts)
	}

	messages := decodeLiveBehaviorMessages(t, thread.Values.Messages)
	var (
		sawWriteTool         bool
		sawClarificationTool bool
		finalAssistant       string
	)
	for _, msg := range messages {
		for _, call := range msg.ToolCalls {
			if call.Name == "write_file" || call.Name == "present_files" {
				sawWriteTool = true
			}
			if call.Name == "ask_clarification" {
				sawClarificationTool = true
			}
		}
		content := strings.TrimSpace(liveBehaviorContentString(msg.Content))
		if (msg.Role == "assistant" || msg.Type == "ai") && content != "" {
			finalAssistant = content
		}
	}
	if sawWriteTool {
		t.Fatal("ambiguous prompt unexpectedly executed artifact-writing tools")
	}
	if finalAssistant == "" && !sawClarificationTool {
		t.Fatal("ambiguous prompt should either ask a clarification question or emit ask_clarification")
	}
	if finalAssistant == "" {
		return
	}
	if !looksLikeDetailRequest(finalAssistant) {
		t.Fatalf("ambiguous prompt should request more detail, got %q", finalAssistant)
	}
}

func looksLikeDetailRequest(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return false
	}
	for _, token := range []string{
		"detail",
		"clarification",
		"more detail",
		"具体",
		"细节",
		"更多",
		"需求",
		"类型",
		"功能",
		"风格",
		"偏好",
		"参考",
		"示例",
		"做什么",
		"用来做什么",
		"什么类型",
		"什么风格",
		"什么需求",
	} {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	return false
}

func TestLiveIssue2126CSVLongRunDoesNotRepeatCompletedOutputs(t *testing.T) {
	baseURL := requireLiveBehaviorBaseURL(t)
	threadID := uuid.NewString()

	csvFixture, err := os.ReadFile(filepath.Join("testdata", "issues", "orders_dirty.csv"))
	if err != nil {
		t.Fatalf("read csv fixture: %v", err)
	}
	uploadLiveBehaviorFile(t, baseURL, threadID, "orders_dirty.csv", csvFixture)

	thread := runLiveBehaviorThread(t, baseURL, threadID, "我上传了一个 CSV 文件 orders_dirty.csv。请读取它，清洗空值和重复行，统计每个 region 的总 sales，并生成两个最终文件到 /mnt/user-data/outputs：orders-summary.md 和 orders-summary.json。最后展示这两个文件。如果某个文件已经成功生成，不要重复写同名文件。")
	if thread.Status != "idle" {
		t.Fatalf("thread status=%q want idle", thread.Status)
	}

	artifactSet := map[string]struct{}{}
	for _, artifact := range thread.Values.Artifacts {
		artifactSet[artifact] = struct{}{}
	}
	for _, want := range []string{
		"/mnt/user-data/outputs/orders-summary.md",
		"/mnt/user-data/outputs/orders-summary.json",
	} {
		if _, ok := artifactSet[want]; !ok {
			t.Fatalf("artifacts=%v missing %s", thread.Values.Artifacts, want)
		}
	}

	messages := decodeLiveBehaviorMessages(t, thread.Values.Messages)
	pendingCalls := map[string]liveBehaviorToolCall{}
	successfulWriteCounts := map[string]int{}
	var (
		sawClarification bool
		sawCSVAccess     bool
	)

	for _, msg := range messages {
		for _, call := range msg.ToolCalls {
			pendingCalls[call.ID] = call
			if call.Name == "ask_clarification" {
				sawClarification = true
			}
			if toolCallMentions(call, "orders_dirty.csv") {
				sawCSVAccess = true
			}
		}
		if msg.Role != "tool" || msg.ToolCallID == "" || !strings.EqualFold(msg.Status, "success") {
			continue
		}
		call, ok := pendingCalls[msg.ToolCallID]
		if !ok {
			continue
		}
		if call.Name == "write_file" {
			if path, _ := call.Args["path"].(string); strings.TrimSpace(path) != "" {
				successfulWriteCounts[path]++
			}
		}
		delete(pendingCalls, msg.ToolCallID)
	}

	if sawClarification {
		t.Fatal("csv long-run regression unexpectedly emitted ask_clarification")
	}
	if !sawCSVAccess {
		t.Fatal("csv long-run regression never referenced uploaded orders_dirty.csv")
	}
	for path, count := range successfulWriteCounts {
		if count > 1 {
			t.Fatalf("write_file repeated completed path %s %d times", path, count)
		}
	}
}

func toolCallMentions(call liveBehaviorToolCall, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	if strings.Contains(call.Name, needle) {
		return true
	}
	for _, value := range call.Args {
		switch typed := value.(type) {
		case string:
			if strings.Contains(typed, needle) {
				return true
			}
		case []any:
			for _, item := range typed {
				if text, ok := item.(string); ok && strings.Contains(text, needle) {
					return true
				}
			}
		}
	}
	return false
}
