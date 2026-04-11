package langgraphcompat

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/memory"
)

func TestMemoryReloadLoadsPersistedFileState(t *testing.T) {
	s, handler := newCompatTestServer(t)

	s.uiStateMu.Lock()
	mem := defaultGatewayMemory()
	mem.User.TopOfMind.Summary = "in-memory value"
	mem.LastUpdated = "2026-04-11T00:00:00Z"
	s.setMemoryLocked(mem)
	s.uiStateMu.Unlock()

	fileState := gatewayMemoryResponse{
		Version:     "1",
		LastUpdated: "2026-04-11T01:02:03Z",
		User: memoryUser{
			TopOfMind: memorySection{
				Summary:   "persisted value",
				UpdatedAt: "2026-04-11T01:02:03Z",
			},
		},
		History: defaultGatewayMemory().History,
		Facts: []memoryFact{{
			ID:       "fact-1",
			Content:  "Remember the persisted preference.",
			Category: "preference",
			Source:   "thread-1",
		}},
	}
	data, err := json.Marshal(fileState)
	if err != nil {
		t.Fatalf("marshal memory file: %v", err)
	}
	if err := os.WriteFile(s.memoryPath(), data, 0o644); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/memory/reload", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("reload status=%d body=%s", resp.Code, resp.Body.String())
	}

	var got gatewayMemoryResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode reload body: %v", err)
	}
	if got.User.TopOfMind.Summary != "persisted value" {
		t.Fatalf("topOfMind=%q want persisted value", got.User.TopOfMind.Summary)
	}
	if len(got.Facts) != 1 || got.Facts[0].ID != "fact-1" {
		t.Fatalf("facts=%#v", got.Facts)
	}

	s.uiStateMu.RLock()
	loaded := s.getMemoryLocked()
	s.uiStateMu.RUnlock()
	if loaded.User.TopOfMind.Summary != "persisted value" {
		t.Fatalf("in-memory topOfMind=%q want persisted value", loaded.User.TopOfMind.Summary)
	}
}

func TestMemoryClearResetsStateAndPersistsDefaultSnapshot(t *testing.T) {
	s, handler := newCompatTestServer(t)

	s.uiStateMu.Lock()
	mem := defaultGatewayMemory()
	mem.User.TopOfMind.Summary = "to be cleared"
	mem.LastUpdated = "2026-04-11T02:03:04Z"
	mem.Facts = []memoryFact{{
		ID:       "fact-to-delete",
		Content:  "Temporary fact",
		Category: "working",
	}}
	s.setMemoryLocked(mem)
	s.uiStateMu.Unlock()

	if err := s.persistMemoryFile(); err != nil {
		t.Fatalf("persist seeded memory: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodDelete, "/api/memory", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", resp.Code, resp.Body.String())
	}

	var got gatewayMemoryResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode clear body: %v", err)
	}
	if got.User.TopOfMind.Summary != "" {
		t.Fatalf("topOfMind=%q want empty", got.User.TopOfMind.Summary)
	}
	if len(got.Facts) != 0 {
		t.Fatalf("facts=%#v want empty", got.Facts)
	}

	data, err := os.ReadFile(s.memoryPath())
	if err != nil {
		t.Fatalf("read memory file: %v", err)
	}
	if strings.Contains(string(data), "fact-to-delete") || strings.Contains(string(data), "to be cleared") {
		t.Fatalf("memory file still contains cleared state: %s", string(data))
	}
}

func TestMemoryExportMatchesCurrentState(t *testing.T) {
	s, handler := newCompatTestServer(t)

	s.uiStateMu.Lock()
	mem := defaultGatewayMemory()
	mem.User.TopOfMind.Summary = "exported state"
	mem.Facts = []memoryFact{{
		ID:       "fact-export",
		Content:  "Export me",
		Category: "context",
		Source:   "thread-export",
	}}
	s.setMemoryLocked(mem)
	s.uiStateMu.Unlock()

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/memory/export", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.Code, resp.Body.String())
	}

	var got gatewayMemoryResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode export body: %v", err)
	}
	if got.User.TopOfMind.Summary != "exported state" {
		t.Fatalf("topOfMind=%q want exported state", got.User.TopOfMind.Summary)
	}
	if len(got.Facts) != 1 || got.Facts[0].ID != "fact-export" {
		t.Fatalf("facts=%#v", got.Facts)
	}
}

func TestMemoryImportReplacesPersistedState(t *testing.T) {
	_, handler := newCompatTestServer(t)

	body := `{
		"version":"1",
		"lastUpdated":"2026-04-11T03:04:05Z",
		"user":{"topOfMind":{"summary":"imported summary","updatedAt":"2026-04-11T03:04:05Z"}},
		"history":{"recentMonths":{"summary":"","updatedAt":""},"earlierContext":{"summary":"","updatedAt":""},"longTermBackground":{"summary":"","updatedAt":""}},
		"facts":[{"id":"fact-import","content":"Imported fact","category":"preference","confidence":0.9,"createdAt":"2026-04-11T03:04:05Z","source":"manual"}]
	}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/memory/import", strings.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", resp.Code, resp.Body.String())
	}

	var got gatewayMemoryResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode import body: %v", err)
	}
	if got.User.TopOfMind.Summary != "imported summary" {
		t.Fatalf("topOfMind=%q want imported summary", got.User.TopOfMind.Summary)
	}
	if len(got.Facts) != 1 || got.Facts[0].ID != "fact-import" {
		t.Fatalf("facts=%#v", got.Facts)
	}
}

func TestMemoryFactCreateAndPatchLifecycle(t *testing.T) {
	_, handler := newCompatTestServer(t)

	createResp := performCompatRequest(t, handler, http.MethodPost, "/api/memory/facts", strings.NewReader(`{"content":"Created fact","category":"context","confidence":0.8}`), map[string]string{
		"Content-Type": "application/json",
	})
	if createResp.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createResp.Code, createResp.Body.String())
	}

	var created gatewayMemoryResponse
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create body: %v", err)
	}
	if len(created.Facts) != 1 {
		t.Fatalf("facts len=%d want 1", len(created.Facts))
	}
	factID := created.Facts[0].ID
	if factID == "" {
		t.Fatal("expected fact id")
	}

	patchResp := performCompatRequest(t, handler, http.MethodPatch, "/api/memory/facts/"+factID, strings.NewReader(`{"content":"Updated fact","confidence":0.6}`), map[string]string{
		"Content-Type": "application/json",
	})
	if patchResp.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", patchResp.Code, patchResp.Body.String())
	}

	var patched gatewayMemoryResponse
	if err := json.Unmarshal(patchResp.Body.Bytes(), &patched); err != nil {
		t.Fatalf("decode patch body: %v", err)
	}
	if len(patched.Facts) != 1 {
		t.Fatalf("facts len=%d want 1", len(patched.Facts))
	}
	if patched.Facts[0].Content != "Updated fact" {
		t.Fatalf("content=%q want updated fact", patched.Facts[0].Content)
	}
	if patched.Facts[0].Confidence != 0.6 {
		t.Fatalf("confidence=%v want 0.6", patched.Facts[0].Confidence)
	}
}

func TestMemoryImportPersistsToMemoryStore(t *testing.T) {
	s, handler := newCompatTestServer(t)

	store, err := memory.NewFileStore(filepath.Join(s.dataRoot, "memory-store"))
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	if err := store.AutoMigrate(context.Background()); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	s.memoryStore = store

	body := `{
		"version":"1",
		"lastUpdated":"2026-04-11T05:06:07Z",
		"user":{"topOfMind":{"summary":"store-backed summary","updatedAt":"2026-04-11T05:06:07Z"}},
		"history":{"recentMonths":{"summary":"","updatedAt":""},"earlierContext":{"summary":"","updatedAt":""},"longTermBackground":{"summary":"","updatedAt":""}},
		"facts":[{"id":"fact-store","content":"Stored fact","category":"preference","confidence":0.7,"createdAt":"2026-04-11T05:06:07Z","source":"manual"}]
	}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/memory/import", strings.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", resp.Code, resp.Body.String())
	}

	doc, err := store.Load(context.Background(), gatewayMemorySessionID)
	if err != nil {
		t.Fatalf("load store doc: %v", err)
	}
	if doc.User.TopOfMind != "store-backed summary" {
		t.Fatalf("topOfMind=%q want store-backed summary", doc.User.TopOfMind)
	}
	if len(doc.Facts) != 1 || doc.Facts[0].ID != "fact-store" {
		t.Fatalf("facts=%#v", doc.Facts)
	}
}

func TestMemoryReloadPrefersMemoryStoreState(t *testing.T) {
	s, handler := newCompatTestServer(t)

	store, err := memory.NewFileStore(filepath.Join(s.dataRoot, "memory-store"))
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	if err := store.AutoMigrate(context.Background()); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	s.memoryStore = store

	if err := store.Save(context.Background(), memory.Document{
		SessionID: gatewayMemorySessionID,
		Source:    gatewayMemorySessionID,
		User: memory.UserMemory{
			TopOfMind: "store wins",
		},
		History:   memory.HistoryMemory{},
		UpdatedAt: parseGatewayMemoryTimestamp("2026-04-11T06:07:08Z"),
		Facts: []memory.Fact{{
			ID:        "fact-store-reload",
			Content:   "Reload from store",
			Category:  "context",
			CreatedAt: parseGatewayMemoryTimestamp("2026-04-11T06:07:08Z"),
			Source:    "manual",
		}},
	}); err != nil {
		t.Fatalf("save store doc: %v", err)
	}

	fileState := gatewayMemoryResponse{
		Version:     "1",
		LastUpdated: "2026-04-11T01:02:03Z",
		User: memoryUser{
			TopOfMind: memorySection{
				Summary:   "file fallback",
				UpdatedAt: "2026-04-11T01:02:03Z",
			},
		},
		History: defaultGatewayMemory().History,
	}
	data, err := json.Marshal(fileState)
	if err != nil {
		t.Fatalf("marshal memory file: %v", err)
	}
	if err := os.WriteFile(s.memoryPath(), data, 0o644); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/memory/reload", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("reload status=%d body=%s", resp.Code, resp.Body.String())
	}

	var got gatewayMemoryResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode reload body: %v", err)
	}
	if got.User.TopOfMind.Summary != "store wins" {
		t.Fatalf("topOfMind=%q want store wins", got.User.TopOfMind.Summary)
	}
	if len(got.Facts) != 1 || got.Facts[0].ID != "fact-store-reload" {
		t.Fatalf("facts=%#v", got.Facts)
	}
}
