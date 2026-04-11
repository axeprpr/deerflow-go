package langgraphcompat

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
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
