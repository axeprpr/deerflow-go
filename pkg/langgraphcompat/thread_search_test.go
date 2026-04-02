package langgraphcompat

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestThreadSearchAcceptsCamelCaseAndSelectsRequestedFields(t *testing.T) {
	s, handler := newCompatTestServer(t)

	alpha := s.ensureSession("thread-alpha", map[string]any{
		"title":      "Alpha title",
		"agent_type": "coder",
	})
	alpha.Status = "busy"
	alpha.CreatedAt = time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)
	alpha.UpdatedAt = alpha.CreatedAt.Add(2 * time.Hour)
	alpha.Configurable["model_name"] = "gpt-5"

	beta := s.ensureSession("thread-beta", map[string]any{
		"title": "Beta title",
	})
	beta.Status = "idle"
	beta.CreatedAt = time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)
	beta.UpdatedAt = beta.CreatedAt.Add(1 * time.Hour)

	body := `{"sortBy":"created_at","sortOrder":"asc","limit":1,"select":["thread_id","values","config"]}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/search", strings.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads=%d want=1", len(threads))
	}
	if got := asString(threads[0]["thread_id"]); got != "thread-alpha" {
		t.Fatalf("thread_id=%q want=thread-alpha", got)
	}
	if _, ok := threads[0]["status"]; ok {
		t.Fatalf("unexpected status field in selected response: %#v", threads[0])
	}
	values, ok := threads[0]["values"].(map[string]any)
	if !ok {
		t.Fatalf("values=%#v", threads[0]["values"])
	}
	if got := asString(values["title"]); got != "Alpha title" {
		t.Fatalf("title=%q want=Alpha title", got)
	}
	config, ok := threads[0]["config"].(map[string]any)
	if !ok {
		t.Fatalf("config=%#v", threads[0]["config"])
	}
	configurable, ok := config["configurable"].(map[string]any)
	if !ok {
		t.Fatalf("configurable=%#v", config["configurable"])
	}
	if got := asString(configurable["model_name"]); got != "gpt-5" {
		t.Fatalf("model_name=%q want=gpt-5", got)
	}
}

func TestThreadsListUsesQueryParamsAndDefaultFields(t *testing.T) {
	s, handler := newCompatTestServer(t)

	alpha := s.ensureSession("thread-alpha", map[string]any{"title": "Alpha"})
	beta := s.ensureSession("thread-beta", map[string]any{"title": "Beta"})
	alpha.CreatedAt = time.Date(2026, 3, 31, 8, 0, 0, 0, time.UTC)
	alpha.UpdatedAt = alpha.CreatedAt.Add(1 * time.Hour)
	beta.CreatedAt = time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	beta.UpdatedAt = beta.CreatedAt.Add(1 * time.Hour)

	resp := performCompatRequest(t, handler, http.MethodGet, "/threads?limit=1&sortBy=created_at&sortOrder=asc", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads=%d want=1", len(threads))
	}
	if got := asString(threads[0]["thread_id"]); got != "thread-alpha" {
		t.Fatalf("thread_id=%q want=thread-alpha", got)
	}
	if _, ok := threads[0]["values"].(map[string]any); !ok {
		t.Fatalf("values=%#v", threads[0]["values"])
	}
	if _, ok := threads[0]["metadata"].(map[string]any); !ok {
		t.Fatalf("metadata=%#v", threads[0]["metadata"])
	}
}

func TestThreadsListSupportsSelectQueryParameter(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-select", map[string]any{"title": "Selected"})

	resp := performCompatRequest(t, handler, http.MethodGet, "/threads?select=thread_id,updated_at", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads=%d want=1", len(threads))
	}
	if _, ok := threads[0]["updated_at"]; !ok {
		t.Fatalf("updated_at missing: %#v", threads[0])
	}
	if _, ok := threads[0]["values"]; ok {
		t.Fatalf("unexpected values field: %#v", threads[0])
	}
}

func TestThreadsListDefaultValuesIncludeUploadedFiles(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-list-uploads"
	s.ensureSession(threadID, map[string]any{"title": "Uploads"})

	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/threads", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads=%d want=1", len(threads))
	}

	values, ok := threads[0]["values"].(map[string]any)
	if !ok {
		t.Fatalf("values=%#v", threads[0]["values"])
	}
	uploadedFiles, ok := values["uploaded_files"].([]any)
	if !ok {
		t.Fatalf("uploaded_files=%#v", values["uploaded_files"])
	}
	if len(uploadedFiles) != 1 {
		t.Fatalf("uploaded_files len=%d want=1", len(uploadedFiles))
	}
	file, ok := uploadedFiles[0].(map[string]any)
	if !ok {
		t.Fatalf("uploaded_files[0]=%#v", uploadedFiles[0])
	}
	if got := asString(file["filename"]); got != "notes.txt" {
		t.Fatalf("filename=%q want=notes.txt", got)
	}
	if got := asString(file["extension"]); got != ".txt" {
		t.Fatalf("extension=%q want=.txt", got)
	}
}

func TestThreadsListIncludesRoutePathForAgentThreads(t *testing.T) {
	s, handler := newCompatTestServer(t)

	s.ensureSession("thread-general", map[string]any{"title": "General chat"})
	s.ensureSession("thread-agent", map[string]any{
		"title":      "Agent chat",
		"agent_name": "writer-bot",
	})

	resp := performCompatRequest(t, handler, http.MethodGet, "/threads?sortBy=thread_id&sortOrder=asc", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("threads=%d want=2", len(threads))
	}

	values, ok := threads[0]["values"].(map[string]any)
	if !ok {
		t.Fatalf("values=%#v", threads[0]["values"])
	}
	if got := asString(values["thread_kind"]); got != "agent" {
		t.Fatalf("thread_kind=%q want=agent", got)
	}
	if got := asString(values["route_path"]); got != "/workspace/agents/writer-bot/chats/thread-agent" {
		t.Fatalf("route_path=%q want agent route", got)
	}
	if got := asString(values["agent_name"]); got != "writer-bot" {
		t.Fatalf("agent_name=%q want=writer-bot", got)
	}

	values, ok = threads[1]["values"].(map[string]any)
	if !ok {
		t.Fatalf("values=%#v", threads[1]["values"])
	}
	if got := asString(values["thread_kind"]); got != "chat" {
		t.Fatalf("thread_kind=%q want=chat", got)
	}
	if got := asString(values["route_path"]); got != "/workspace/chats/thread-general" {
		t.Fatalf("route_path=%q want general route", got)
	}
	if got := asString(values["agent_name"]); got != "" {
		t.Fatalf("agent_name=%q want empty", got)
	}
}

func TestThreadSearchFiltersByQueryStatusMetadataAndValues(t *testing.T) {
	s, handler := newCompatTestServer(t)

	matching := s.ensureSession("thread-reporting", map[string]any{
		"title":      "Quarterly report",
		"agent_type": "coder",
	})
	matching.Status = "busy"
	matching.Todos = []Todo{{Content: "draft", Status: "in_progress"}}
	matching.UpdatedAt = time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)

	other := s.ensureSession("thread-notes", map[string]any{
		"title":      "Meeting notes",
		"agent_type": "researcher",
	})
	other.Status = "idle"
	other.Todos = []Todo{{Content: "archive", Status: "completed"}}
	other.UpdatedAt = time.Date(2026, 3, 31, 11, 0, 0, 0, time.UTC)

	body := `{
		"query":"report",
		"status":"busy",
		"metadata":{"agent_type":"coder"},
		"values":{"title":"Quarterly report"}
	}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/search", strings.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads=%d want=1 body=%s", len(threads), resp.Body.String())
	}
	if got := asString(threads[0]["thread_id"]); got != "thread-reporting" {
		t.Fatalf("thread_id=%q want=thread-reporting", got)
	}
}

func TestThreadSearchFiltersByPersistedCustomValues(t *testing.T) {
	s, handler := newCompatTestServer(t)

	matching := s.ensureSession("thread-custom-match", map[string]any{
		"title": "Custom state",
	})
	matching.Values["sidebar_tab"] = "artifacts"
	matching.Values["draft_id"] = "draft-42"

	other := s.ensureSession("thread-custom-other", map[string]any{
		"title": "Other state",
	})
	other.Values["sidebar_tab"] = "chat"
	other.Values["draft_id"] = "draft-7"

	body := `{"values":{"sidebar_tab":"artifacts","draft_id":"draft-42"}}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/search", strings.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads=%d want=1 body=%s", len(threads), resp.Body.String())
	}
	if got := asString(threads[0]["thread_id"]); got != "thread-custom-match" {
		t.Fatalf("thread_id=%q want=thread-custom-match", got)
	}
}

func TestThreadSearchMatchesMessageContent(t *testing.T) {
	s, handler := newCompatTestServer(t)

	matching := s.ensureSession("thread-incident", map[string]any{
		"title": "Runbook updates",
	})
	matching.Messages = []models.Message{
		{
			ID:        "msg-1",
			SessionID: "thread-incident",
			Role:      models.RoleHuman,
			Content:   "Please summarize the incident timeline and customer impact.",
		},
	}

	other := s.ensureSession("thread-planning", map[string]any{
		"title": "Trip planning",
	})
	other.Messages = []models.Message{
		{
			ID:        "msg-2",
			SessionID: "thread-planning",
			Role:      models.RoleHuman,
			Content:   "Plan a weekend in Hangzhou.",
		},
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/search", strings.NewReader(`{"query":"customer impact"}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads=%d want=1 body=%s", len(threads), resp.Body.String())
	}
	if got := asString(threads[0]["thread_id"]); got != "thread-incident" {
		t.Fatalf("thread_id=%q want=thread-incident", got)
	}
}

func TestThreadSearchMatchesStructuredMessageMetadataAndToolCalls(t *testing.T) {
	s, handler := newCompatTestServer(t)

	matching := s.ensureSession("thread-structured", map[string]any{
		"title": "Structured content",
	})
	matching.Todos = []Todo{{Content: "Review quarterly spreadsheet", Status: "pending"}}
	matching.Messages = []models.Message{
		{
			ID:        "msg-1",
			SessionID: "thread-structured",
			Role:      models.RoleHuman,
			Content:   "Please analyze this upload.",
			Metadata: map[string]string{
				"additional_kwargs": `{"files":[{"filename":"quarterly-report.xlsx","path":"/mnt/user-data/uploads/quarterly-report.xlsx"}]}`,
				"multi_content":     `[{"type":"text","text":"Quarterly revenue by region"}]`,
			},
		},
		{
			ID:        "msg-2",
			SessionID: "thread-structured",
			Role:      models.RoleAI,
			Content:   "Running spreadsheet analysis.",
			ToolCalls: []models.ToolCall{
				{
					ID:        "call-1",
					Name:      "python",
					Arguments: map[string]any{"script": "summarize quarterly revenue"},
					Status:    models.CallStatusCompleted,
				},
			},
		},
	}

	other := s.ensureSession("thread-other", map[string]any{
		"title": "Other thread",
	})
	other.Messages = []models.Message{
		{
			ID:        "msg-3",
			SessionID: "thread-other",
			Role:      models.RoleHuman,
			Content:   "Draft a travel checklist.",
		},
	}

	for _, query := range []string{"quarterly-report.xlsx", "quarterly revenue", "summarize quarterly revenue", "review quarterly spreadsheet"} {
		resp := performCompatRequest(t, handler, http.MethodPost, "/threads/search", strings.NewReader(`{"query":"`+query+`"}`), map[string]string{
			"Content-Type": "application/json",
		})
		if resp.Code != http.StatusOK {
			t.Fatalf("query=%q status=%d body=%s", query, resp.Code, resp.Body.String())
		}

		var threads []map[string]any
		if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
			t.Fatalf("query=%q unmarshal response: %v", query, err)
		}
		if len(threads) != 1 {
			t.Fatalf("query=%q threads=%d want=1 body=%s", query, len(threads), resp.Body.String())
		}
		if got := asString(threads[0]["thread_id"]); got != "thread-structured" {
			t.Fatalf("query=%q thread_id=%q want=thread-structured", query, got)
		}
	}
}

func TestThreadSearchMatchesArtifactsAndThreadDataPaths(t *testing.T) {
	s, handler := newCompatTestServer(t)

	threadID := "thread-artifacts"
	s.ensureSession(threadID, map[string]any{
		"title": "Website build",
	})

	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "launch-plan.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	for _, query := range []string{"launch-plan.html", "user-data/workspace"} {
		resp := performCompatRequest(t, handler, http.MethodPost, "/threads/search", strings.NewReader(`{"query":"`+query+`"}`), map[string]string{
			"Content-Type": "application/json",
		})
		if resp.Code != http.StatusOK {
			t.Fatalf("query=%q status=%d body=%s", query, resp.Code, resp.Body.String())
		}

		var threads []map[string]any
		if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
			t.Fatalf("query=%q unmarshal response: %v", query, err)
		}
		if len(threads) != 1 {
			t.Fatalf("query=%q threads=%d want=1 body=%s", query, len(threads), resp.Body.String())
		}
		if got := asString(threads[0]["thread_id"]); got != threadID {
			t.Fatalf("query=%q thread_id=%q want=%s", query, got, threadID)
		}
	}
}

func TestThreadSearchMatchesUploadMarkdownCompanionContent(t *testing.T) {
	s, handler := newCompatTestServer(t)

	threadID := "thread-upload-search"
	s.ensureSession(threadID, map[string]any{
		"title": "Document review",
	})

	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "requirements.pdf"), []byte("%PDF"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "requirements.md"), []byte("# Requirements\n\nCustomer impact analysis and rollback criteria.\n"), 0o644); err != nil {
		t.Fatalf("write markdown companion: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/search", strings.NewReader(`{"query":"rollback criteria"}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads=%d want=1 body=%s", len(threads), resp.Body.String())
	}
	if got := asString(threads[0]["thread_id"]); got != threadID {
		t.Fatalf("thread_id=%q want=%s", got, threadID)
	}
}

func TestThreadSearchMatchesArtifactTextContent(t *testing.T) {
	s, handler := newCompatTestServer(t)

	threadID := "thread-artifact-content"
	s.ensureSession(threadID, map[string]any{
		"title": "Launch prep",
	})

	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "launch-notes.md"), []byte("# Launch Notes\n\nMitigation owner: platform-oncall.\n"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/search", strings.NewReader(`{"query":"platform-oncall"}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads=%d want=1 body=%s", len(threads), resp.Body.String())
	}
	if got := asString(threads[0]["thread_id"]); got != threadID {
		t.Fatalf("thread_id=%q want=%s", got, threadID)
	}
}

func TestThreadSearchHonorsZeroLimit(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-a", map[string]any{"title": "A"})
	s.ensureSession("thread-b", map[string]any{"title": "B"})

	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/search", strings.NewReader(`{"limit":0}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(threads) != 0 {
		t.Fatalf("threads=%d want=0", len(threads))
	}
}

func TestThreadSearchUsesDeterministicTieBreakersForDescSort(t *testing.T) {
	s, handler := newCompatTestServer(t)

	alpha := s.ensureSession("thread-alpha", map[string]any{"title": "Alpha"})
	beta := s.ensureSession("thread-beta", map[string]any{"title": "Beta"})
	gamma := s.ensureSession("thread-gamma", map[string]any{"title": "Gamma"})

	created := time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	updated := created.Add(30 * time.Minute)

	alpha.CreatedAt, alpha.UpdatedAt = created, updated
	beta.CreatedAt, beta.UpdatedAt = created, updated
	gamma.CreatedAt, gamma.UpdatedAt = created, updated

	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/search", strings.NewReader(`{"sortBy":"updated_at","sortOrder":"desc","select":["thread_id"]}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(threads) != 3 {
		t.Fatalf("threads=%d want=3", len(threads))
	}

	got := []string{
		asString(threads[0]["thread_id"]),
		asString(threads[1]["thread_id"]),
		asString(threads[2]["thread_id"]),
	}
	want := []string{"thread-gamma", "thread-beta", "thread-alpha"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("thread order=%q want=%q", strings.Join(got, ","), strings.Join(want, ","))
	}
}

func TestThreadSearchUsesDeterministicTieBreakersForAscSort(t *testing.T) {
	s, handler := newCompatTestServer(t)

	alpha := s.ensureSession("thread-alpha", map[string]any{"title": "Alpha"})
	beta := s.ensureSession("thread-beta", map[string]any{"title": "Beta"})
	gamma := s.ensureSession("thread-gamma", map[string]any{"title": "Gamma"})

	created := time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	updated := created.Add(30 * time.Minute)

	alpha.CreatedAt, alpha.UpdatedAt = created, updated
	beta.CreatedAt, beta.UpdatedAt = created, updated
	gamma.CreatedAt, gamma.UpdatedAt = created, updated

	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/search", strings.NewReader(`{"sortBy":"updated_at","sortOrder":"asc","select":["thread_id"]}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(threads) != 3 {
		t.Fatalf("threads=%d want=3", len(threads))
	}

	got := []string{
		asString(threads[0]["thread_id"]),
		asString(threads[1]["thread_id"]),
		asString(threads[2]["thread_id"]),
	}
	want := []string{"thread-alpha", "thread-beta", "thread-gamma"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("thread order=%q want=%q", strings.Join(got, ","), strings.Join(want, ","))
	}
}

func TestThreadSearchUsesCreatedAtAsSecondaryTieBreakerForUpdatedAtSort(t *testing.T) {
	s, handler := newCompatTestServer(t)

	alpha := s.ensureSession("thread-alpha", map[string]any{"title": "Alpha"})
	beta := s.ensureSession("thread-beta", map[string]any{"title": "Beta"})
	gamma := s.ensureSession("thread-gamma", map[string]any{"title": "Gamma"})

	updated := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	alpha.CreatedAt, alpha.UpdatedAt = time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC), updated
	beta.CreatedAt, beta.UpdatedAt = time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC), updated
	gamma.CreatedAt, gamma.UpdatedAt = time.Date(2026, 3, 31, 11, 0, 0, 0, time.UTC), updated

	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/search", strings.NewReader(`{"sortBy":"updated_at","sortOrder":"desc","select":["thread_id"]}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(threads) != 3 {
		t.Fatalf("threads=%d want=3", len(threads))
	}

	got := []string{
		asString(threads[0]["thread_id"]),
		asString(threads[1]["thread_id"]),
		asString(threads[2]["thread_id"]),
	}
	want := []string{"thread-gamma", "thread-beta", "thread-alpha"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("thread order=%q want=%q", strings.Join(got, ","), strings.Join(want, ","))
	}
}
