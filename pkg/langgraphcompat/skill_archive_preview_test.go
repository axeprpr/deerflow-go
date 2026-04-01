package langgraphcompat

import (
	"archive/zip"
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactGetSkillArchiveDefaultsToSkillMarkdownPreview(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-skill-preview"
	s.ensureSession(threadID, nil)

	archivePath := writeTestSkillArchive(t, filepath.Join(s.threadRoot(threadID), "outputs", "sample.skill"))

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/sample.skill", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); !strings.Contains(got, "text/markdown") {
		t.Fatalf("content-type=%q want markdown", got)
	}
	if got := resp.Header().Get("Content-Disposition"); got != "" {
		t.Fatalf("content-disposition=%q want empty", got)
	}
	if body := resp.Body.String(); !strings.Contains(body, "name: sample-skill") || !strings.Contains(body, "Preview archive") {
		t.Fatalf("body=%q missing extracted SKILL.md", body)
	}
	if data, err := os.ReadFile(archivePath); err != nil {
		t.Fatalf("read archive: %v", err)
	} else if bytes.Equal(resp.Body.Bytes(), data) {
		t.Fatal("expected markdown preview, got raw archive bytes")
	}
}

func TestArtifactGetSkillArchiveDownloadReturnsArchive(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-skill-download"
	s.ensureSession(threadID, nil)

	archivePath := writeTestSkillArchive(t, filepath.Join(s.threadRoot(threadID), "outputs", "sample.skill"))
	raw, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/sample.skill?download=true", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, "sample.skill") {
		t.Fatalf("content-disposition=%q want attachment filename", got)
	}
	if !bytes.Equal(resp.Body.Bytes(), raw) {
		t.Fatal("expected downloaded body to match raw archive")
	}
}

func writeTestSkillArchive(t *testing.T, archivePath string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer file.Close()

	zipWriter := zip.NewWriter(file)
	skillFile, err := zipWriter.Create("sample-skill/SKILL.md")
	if err != nil {
		t.Fatalf("create SKILL.md: %v", err)
	}
	content := `---
name: sample-skill
description: Preview archive
---

# Sample Skill
`
	if _, err := skillFile.Write([]byte(content)); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	return archivePath
}
