package langgraphcompat

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillInstallIgnoresMacOSMetadataAndNestedWrapperDir(t *testing.T) {
	s, handler := newCompatTestServer(t)

	threadID := "thread-skill-install-nested"
	archivePath := filepath.Join(s.uploadsDir(threadID), "nested.skill")
	writeSkillArchiveWithEntries(t, archivePath, map[string]string{
		"exported/my-skill/SKILL.md":   "---\nname: my-skill\ndescription: Nested archive\n---\n\n# My Skill\n",
		"exported/my-skill/README.md":  "hello",
		"__MACOSX/exported/._my-skill": "meta",
		".DS_Store":                    "junk",
	})

	body := `{"thread_id":"` + threadID + `","path":"` + uploadArtifactURL(threadID, "nested.skill") + `"}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/skills/install", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusOK {
		t.Fatalf("install status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Success   bool   `json:"success"`
		SkillName string `json:"skill_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Success || payload.SkillName != "my-skill" {
		t.Fatalf("payload=%#v", payload)
	}
	if _, err := os.Stat(filepath.Join(s.gatewayCustomSkillsRoot(), "my-skill", "SKILL.md")); err != nil {
		t.Fatalf("installed skill missing: %v", err)
	}
}

func TestSkillInstallRejectsWindowsAbsoluteArchiveMember(t *testing.T) {
	s, handler := newCompatTestServer(t)

	threadID := "thread-skill-install-unsafe"
	archivePath := filepath.Join(s.uploadsDir(threadID), "unsafe.skill")
	writeSkillArchiveWithEntries(t, archivePath, map[string]string{
		"C:\\temp\\SKILL.md": "---\nname: bad-skill\ndescription: bad\n---\n",
	})

	body := `{"thread_id":"` + threadID + `","path":"` + uploadArtifactURL(threadID, "unsafe.skill") + `"}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/skills/install", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("install status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(strings.ToLower(resp.Body.String()), "unsafe path") {
		t.Fatalf("body=%q want unsafe path error", resp.Body.String())
	}
}

func TestSkillInstallRejectsArchivesWithMultipleSkillRoots(t *testing.T) {
	s, handler := newCompatTestServer(t)

	threadID := "thread-skill-install-multi"
	archivePath := filepath.Join(s.uploadsDir(threadID), "multi.skill")
	writeSkillArchiveWithEntries(t, archivePath, map[string]string{
		"alpha/SKILL.md": "---\nname: alpha\ndescription: first\n---\n",
		"beta/SKILL.md":  "---\nname: beta\ndescription: second\n---\n",
	})

	body := `{"thread_id":"` + threadID + `","path":"` + uploadArtifactURL(threadID, "multi.skill") + `"}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/skills/install", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("install status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "exactly one SKILL.md") {
		t.Fatalf("body=%q want multiple root error", resp.Body.String())
	}
}

func writeSkillArchiveWithEntries(t *testing.T, archivePath string, entries map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("mkdir archive dir: %v", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %q: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := os.WriteFile(archivePath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
}
