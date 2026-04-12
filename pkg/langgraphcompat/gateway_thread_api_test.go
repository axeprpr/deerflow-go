package langgraphcompat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
)

func TestGatewayThreadsListReturnsGatewayThreadEnvelopes(t *testing.T) {
	s, handler := newCompatTestServer(t)
	first := s.ensureSession("thread-gateway-list-1", map[string]any{"agent_name": "writer-bot"})
	applyThreadStateUpdate(first, map[string]any{"title": "Alpha brief"}, nil)
	second := s.ensureSession("thread-gateway-list-2", map[string]any{"agent_name": "coder-bot"})
	applyThreadStateUpdate(second, map[string]any{"title": "Beta plan"}, nil)

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads?limit=1&query="+url.QueryEscape("Beta"), nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads=%d want=1 body=%s", len(threads), resp.Body.String())
	}
	if got := asString(threads[0]["thread_id"]); got != "thread-gateway-list-2" {
		t.Fatalf("thread_id=%q want=thread-gateway-list-2", got)
	}

	values, ok := threads[0]["values"].(map[string]any)
	if !ok {
		t.Fatalf("values=%#v", threads[0]["values"])
	}
	if got := asString(values["title"]); got != "Beta plan" {
		t.Fatalf("title=%q want=Beta plan", got)
	}
}

func TestGatewayThreadSearchReturnsGatewayThreadEnvelopes(t *testing.T) {
	s, handler := newCompatTestServer(t)
	first := s.ensureSession("thread-gateway-search-1", map[string]any{"agent_name": "writer-bot"})
	applyThreadStateUpdate(first, map[string]any{"title": "Alpha brief"}, nil)
	second := s.ensureSession("thread-gateway-search-2", map[string]any{"agent_name": "coder-bot"})
	applyThreadStateUpdate(second, map[string]any{"title": "Beta launch"}, nil)

	body := `{"query":"Beta","limit":1,"select":["thread_id","values","config"]}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/search", strings.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var threads []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads=%d want=1 body=%s", len(threads), resp.Body.String())
	}
	if got := asString(threads[0]["thread_id"]); got != "thread-gateway-search-2" {
		t.Fatalf("thread_id=%q want=thread-gateway-search-2", got)
	}

	values, ok := threads[0]["values"].(map[string]any)
	if !ok {
		t.Fatalf("values=%#v", threads[0]["values"])
	}
	if got := asString(values["title"]); got != "Beta launch" {
		t.Fatalf("title=%q want=Beta launch", got)
	}

	config, ok := threads[0]["config"].(map[string]any)
	if !ok {
		t.Fatalf("config=%#v", threads[0]["config"])
	}
	configurable, ok := config["configurable"].(map[string]any)
	if !ok {
		t.Fatalf("configurable=%#v", config["configurable"])
	}
	if got := asString(configurable["agent_name"]); got != "coder-bot" {
		t.Fatalf("agent_name=%q want=coder-bot", got)
	}
}

func TestGatewayThreadCreateReturnsThreadEnvelope(t *testing.T) {
	s, handler := newCompatTestServer(t)

	body := `{
		"thread_id":"thread-gateway-create",
		"metadata":{"agent_name":"writer-bot"},
		"values":{"title":"Fresh draft"},
		"status":"busy",
		"config":{"configurable":{"model_name":"openai/gpt-5"}}
	}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads", strings.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var thread map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &thread); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := asString(thread["thread_id"]); got != "thread-gateway-create" {
		t.Fatalf("thread_id=%q want=thread-gateway-create", got)
	}

	stored := s.getThreadState("thread-gateway-create")
	if stored == nil {
		t.Fatal("expected stored thread state")
	}
	if got := asString(stored.Values["title"]); got != "Fresh draft" {
		t.Fatalf("title=%q want=Fresh draft", got)
	}
}

func TestGatewayThreadCreateRejectsInvalidThreadID(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads", strings.NewReader(`{"thread_id":"bad.id"}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "invalid thread_id") {
		t.Fatalf("body=%q", resp.Body.String())
	}
}

func TestGatewayThreadClarificationsListReturnsThreadItems(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-gateway-clarify", nil)

	first, err := s.clarify.Request(clarification.WithThreadID(context.Background(), "thread-gateway-clarify"), clarification.ClarificationRequest{
		Question: "First?",
	})
	if err != nil {
		t.Fatalf("first request error = %v", err)
	}
	second, err := s.clarify.Request(clarification.WithThreadID(context.Background(), "thread-gateway-clarify"), clarification.ClarificationRequest{
		Question: "Second?",
	})
	if err != nil {
		t.Fatalf("second request error = %v", err)
	}
	if _, err := s.clarify.Request(clarification.WithThreadID(context.Background(), "thread-other"), clarification.ClarificationRequest{
		Question: "Other?",
	}); err != nil {
		t.Fatalf("other request error = %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/thread-gateway-clarify/clarifications", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Clarifications []clarification.Clarification `json:"clarifications"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Clarifications) != 2 {
		t.Fatalf("clarifications=%d want=2 body=%s", len(payload.Clarifications), resp.Body.String())
	}
	if payload.Clarifications[0].ID != first.ID || payload.Clarifications[1].ID != second.ID {
		t.Fatalf("ids=[%s %s] want=[%s %s]", payload.Clarifications[0].ID, payload.Clarifications[1].ID, first.ID, second.ID)
	}
}

func TestGatewayThreadGetReturnsThreadEnvelope(t *testing.T) {
	s, handler := newCompatTestServer(t)
	session := s.ensureSession("thread-gateway-get", map[string]any{"agent_name": "writer-bot"})
	applyThreadStateUpdate(session, map[string]any{
		"title":       "Release checklist",
		"sidebar_tab": "artifacts",
	}, map[string]any{
		"agent_type": "coder",
	})
	session.Configurable["model_name"] = "openai/gpt-5"

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/thread-gateway-get", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var thread map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &thread); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := asString(thread["thread_id"]); got != "thread-gateway-get" {
		t.Fatalf("thread_id=%q want=thread-gateway-get", got)
	}
	if got := asString(thread["status"]); got != "idle" {
		t.Fatalf("status=%q want=idle", got)
	}

	values, ok := thread["values"].(map[string]any)
	if !ok {
		t.Fatalf("values=%#v", thread["values"])
	}
	if got := asString(values["title"]); got != "Release checklist" {
		t.Fatalf("title=%q want=Release checklist", got)
	}
	if got := asString(values["sidebar_tab"]); got != "artifacts" {
		t.Fatalf("sidebar_tab=%q want=artifacts", got)
	}

	config, ok := thread["config"].(map[string]any)
	if !ok {
		t.Fatalf("config=%#v", thread["config"])
	}
	configurable, ok := config["configurable"].(map[string]any)
	if !ok {
		t.Fatalf("configurable=%#v", config["configurable"])
	}
	if got := asString(configurable["model_name"]); got != "openai/gpt-5" {
		t.Fatalf("model_name=%q want=openai/gpt-5", got)
	}
}

func TestGatewayThreadPatchUpdatesThreadEnvelope(t *testing.T) {
	s, handler := newCompatTestServer(t)
	session := s.ensureSession("thread-gateway-patch", map[string]any{"title": "Old title"})
	session.Status = "idle"

	body := `{
		"metadata":{"agent_name":"writer-bot"},
		"values":{"title":"New title","draft_id":"draft-42"},
		"status":"busy",
		"config":{"configurable":{"model_name":"openai/gpt-5"}}
	}`
	resp := performCompatRequest(t, handler, http.MethodPatch, "/api/threads/thread-gateway-patch", strings.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	reloaded := s.getThreadState("thread-gateway-patch")
	if reloaded == nil {
		t.Fatal("expected thread state")
	}
	if got := asString(reloaded.Values["title"]); got != "New title" {
		t.Fatalf("title=%q want=New title", got)
	}
	if got := asString(reloaded.Values["draft_id"]); got != "draft-42" {
		t.Fatalf("draft_id=%q want=draft-42", got)
	}

	s.sessionsMu.RLock()
	updated := s.sessions["thread-gateway-patch"]
	s.sessionsMu.RUnlock()
	if updated == nil {
		t.Fatal("expected stored session")
	}
	if got := updated.Status; got != "busy" {
		t.Fatalf("status=%q want=busy", got)
	}
	if got := asString(updated.Metadata["agent_name"]); got != "writer-bot" {
		t.Fatalf("agent_name=%q want=writer-bot", got)
	}
	if got := asString(updated.Configurable["model_name"]); got != "openai/gpt-5" {
		t.Fatalf("model_name=%q want=openai/gpt-5", got)
	}
}

func TestGatewayThreadPutUpdatesThreadEnvelope(t *testing.T) {
	s, handler := newCompatTestServer(t)
	session := s.ensureSession("thread-gateway-put", map[string]any{"title": "Old title"})
	session.Status = "idle"

	body := `{
		"metadata":{"agent_name":"writer-bot"},
		"values":{"title":"New title","draft_id":"draft-99"},
		"status":"busy",
		"config":{"configurable":{"model_name":"openai/gpt-5"}}
	}`
	resp := performCompatRequest(t, handler, http.MethodPut, "/api/threads/thread-gateway-put", strings.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	reloaded := s.getThreadState("thread-gateway-put")
	if reloaded == nil {
		t.Fatal("expected thread state")
	}
	if got := asString(reloaded.Values["title"]); got != "New title" {
		t.Fatalf("title=%q want=New title", got)
	}
	if got := asString(reloaded.Values["draft_id"]); got != "draft-99" {
		t.Fatalf("draft_id=%q want=draft-99", got)
	}

	s.sessionsMu.RLock()
	updated := s.sessions["thread-gateway-put"]
	s.sessionsMu.RUnlock()
	if updated == nil {
		t.Fatal("expected stored session")
	}
	if got := updated.Status; got != "busy" {
		t.Fatalf("status=%q want=busy", got)
	}
	if got := asString(updated.Metadata["agent_name"]); got != "writer-bot" {
		t.Fatalf("agent_name=%q want=writer-bot", got)
	}
	if got := asString(updated.Configurable["model_name"]); got != "openai/gpt-5" {
		t.Fatalf("model_name=%q want=openai/gpt-5", got)
	}
}

func TestGatewayThreadGetRejectsInvalidThreadID(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/bad.id", nil, nil)
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "invalid thread_id") {
		t.Fatalf("body=%q", resp.Body.String())
	}
}

func TestGatewayThreadPatchRejectsInvalidThreadID(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodPatch, "/api/threads/bad.id", strings.NewReader(`{"values":{"title":"ignored"}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "invalid thread_id") {
		t.Fatalf("body=%q", resp.Body.String())
	}
}

func TestGatewayThreadPutRejectsInvalidThreadID(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodPut, "/api/threads/bad.id", strings.NewReader(`{"values":{"title":"ignored"}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "invalid thread_id") {
		t.Fatalf("body=%q", resp.Body.String())
	}
}

func TestGatewayThreadStateRoutesProxyLangGraphStateHandlers(t *testing.T) {
	s, handler := newCompatTestServer(t)
	session := s.ensureSession("thread-gateway-state", map[string]any{"agent_name": "writer-bot"})
	applyThreadStateUpdate(session, map[string]any{"title": "Initial title"}, nil)

	patchResp := performCompatRequest(t, handler, http.MethodPatch, "/api/threads/thread-gateway-state/state", strings.NewReader(`{"values":{"sidebar_tab":"artifacts","draft_id":"draft-42"}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if patchResp.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", patchResp.Code, patchResp.Body.String())
	}

	stateResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/thread-gateway-state/state", nil, nil)
	if stateResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", stateResp.Code, stateResp.Body.String())
	}

	var state map[string]any
	if err := json.Unmarshal(stateResp.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	values, ok := state["values"].(map[string]any)
	if !ok {
		t.Fatalf("values=%#v", state["values"])
	}
	if got := asString(values["sidebar_tab"]); got != "artifacts" {
		t.Fatalf("sidebar_tab=%q want=artifacts", got)
	}
	if got := asString(values["draft_id"]); got != "draft-42" {
		t.Fatalf("draft_id=%q want=draft-42", got)
	}
}

func TestGatewayThreadHistoryRouteProxyLangGraphHistoryHandler(t *testing.T) {
	s, handler := newCompatTestServer(t)
	session := s.ensureSession("thread-gateway-history", map[string]any{"agent_name": "writer-bot"})
	applyThreadStateUpdate(session, map[string]any{"title": "Initial"}, nil)

	resp1 := performCompatRequest(t, handler, http.MethodPatch, "/api/threads/thread-gateway-history/state", strings.NewReader(`{"values":{"draft_id":"draft-1"}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp1.Code != http.StatusOK {
		t.Fatalf("first patch status=%d body=%s", resp1.Code, resp1.Body.String())
	}
	resp2 := performCompatRequest(t, handler, http.MethodPatch, "/api/threads/thread-gateway-history/state", strings.NewReader(`{"values":{"draft_id":"draft-2"}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp2.Code != http.StatusOK {
		t.Fatalf("second patch status=%d body=%s", resp2.Code, resp2.Body.String())
	}

	historyResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/thread-gateway-history/history?limit=1", nil, nil)
	if historyResp.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", historyResp.Code, historyResp.Body.String())
	}

	var history []map[string]any
	if err := json.Unmarshal(historyResp.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history=%d want=1 body=%s", len(history), historyResp.Body.String())
	}
	values, ok := history[0]["values"].(map[string]any)
	if !ok {
		t.Fatalf("values=%#v", history[0]["values"])
	}
	if got := asString(values["draft_id"]); got != "draft-2" {
		t.Fatalf("draft_id=%q want=draft-2", got)
	}
}

func TestGatewayThreadRunRoutesProxyLangGraphRunHandlers(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-gateway-runs", map[string]any{"agent_name": "writer-bot"})
	run := &Run{
		RunID:     "run-gateway-routes",
		ThreadID:  "thread-gateway-runs",
		Status:    "success",
		CreatedAt: time.Now().UTC().Add(time.Minute),
		UpdatedAt: time.Now().UTC().Add(2 * time.Minute),
	}
	s.saveRun(run)

	listResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/thread-gateway-runs/runs", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	var listed map[string]any
	if err := json.Unmarshal(listResp.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	runs, ok := listed["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("runs=%#v", listed["runs"])
	}

	getResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/thread-gateway-runs/runs/run-gateway-routes", nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(getResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got := asString(payload["run_id"]); got != "run-gateway-routes" {
		t.Fatalf("run_id=%q want=run-gateway-routes", got)
	}
}

func TestGatewayThreadStreamJoinRouteProxyLangGraphJoinHandler(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-gateway-stream", map[string]any{"agent_name": "writer-bot"})

	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-gateway-stream/stream", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content-type=%q want text/event-stream", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, ": no active run") {
		t.Fatalf("body=%q missing no active run marker", body)
	}
}

func TestGatewayThreadNestedRoutesRejectInvalidThreadID(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/bad.id/state", nil, nil)
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "invalid thread_id") {
		t.Fatalf("body=%q", resp.Body.String())
	}
}

func TestGatewayThreadStateMissingUsesJSONDetail(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/thread-missing/state", nil, nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v body=%s", err, resp.Body.String())
	}
	if payload["detail"] != "thread not found" {
		t.Fatalf("detail=%#v", payload["detail"])
	}
}

func TestGatewayThreadFilesMissingUsesJSONDetail(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/thread-missing/files", nil, nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v body=%s", err, resp.Body.String())
	}
	if payload["detail"] != "thread not found" {
		t.Fatalf("detail=%#v", payload["detail"])
	}
}

func TestGatewayThreadClarificationsMissingUsesJSONDetail(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/thread-missing/clarifications", nil, nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v body=%s", err, resp.Body.String())
	}
	if payload["detail"] != "thread not found" {
		t.Fatalf("detail=%#v", payload["detail"])
	}
}
