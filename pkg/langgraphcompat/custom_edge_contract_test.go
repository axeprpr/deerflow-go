package langgraphcompat

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryImportFailureUsesConsistentDetail(t *testing.T) {
	s, handler := newCompatTestServer(t)

	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	s.dataRoot = blocker

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/memory/import", strings.NewReader(`{"version":"1","facts":[]}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v body=%s", err, resp.Body.String())
	}
	if payload["detail"] != "Failed to import memory data." {
		t.Fatalf("detail=%#v", payload["detail"])
	}
}

func TestWrapAuthInvalidTokenUsesConsistentDetail(t *testing.T) {
	protected := wrapAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), AuthConfig{Token: "secret"})

	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}
	if payload["detail"] != "Invalid token" {
		t.Fatalf("detail=%#v", payload["detail"])
	}
}

func TestTTSValidationUsesConsistentDetail(t *testing.T) {
	s, _ := newCompatTestServer(t)

	t.Run("invalid body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/tts", bytes.NewBufferString("{"))
		rec := httptest.NewRecorder()
		s.handleTTS(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}

		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
		}
		if payload["detail"] != "Invalid request body" {
			t.Fatalf("detail=%#v", payload["detail"])
		}
	})

	t.Run("missing text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/tts", bytes.NewBufferString(`{"text":"  "}`))
		rec := httptest.NewRecorder()
		s.handleTTS(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}

		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
		}
		if payload["detail"] != "Text is required" {
			t.Fatalf("detail=%#v", payload["detail"])
		}
	})
}
