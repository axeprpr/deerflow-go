package langgraphcompat

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactGetIndexHTMLServesFileWithoutRedirect(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-artifact-index-inline"
	s.ensureSession(threadID, nil)

	target := filepath.Join(s.threadRoot(threadID), "outputs", "index.html")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "<!doctype html><title>artifact</title><p>fish</p>"
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/index.html", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d headers=%v body=%q", resp.Code, resp.Header(), resp.Body.String())
	}
	if got := resp.Header().Get("Location"); got != "" {
		t.Fatalf("location=%q want empty", got)
	}
	if got := resp.Header().Get("Content-Disposition"); got != "" {
		t.Fatalf("content-disposition=%q want empty", got)
	}
	if got := resp.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("content-type=%q want html", got)
	}
	if got := resp.Body.String(); got != body {
		t.Fatalf("body=%q want %q", got, body)
	}
}

func TestArtifactGetIndexHTMLDownloadDoesNotRedirect(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-artifact-index-download"
	s.ensureSession(threadID, nil)

	target := filepath.Join(s.threadRoot(threadID), "outputs", "index.html")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "<!doctype html><title>artifact</title><p>fish</p>"
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/index.html?download=true", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d headers=%v body=%q", resp.Code, resp.Header(), resp.Body.String())
	}
	if got := resp.Header().Get("Location"); got != "" {
		t.Fatalf("location=%q want empty", got)
	}
	if got := resp.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, "index.html") {
		t.Fatalf("content-disposition=%q want attachment filename", got)
	}
	if got := resp.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("content-type=%q want html", got)
	}
	if got := resp.Body.String(); got != body {
		t.Fatalf("body=%q want %q", got, body)
	}
}
