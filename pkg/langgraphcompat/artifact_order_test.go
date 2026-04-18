package langgraphcompat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestThreadStatePreservesPresentedArtifactOrder(t *testing.T) {
	s, _ := newCompatTestServer(t)
	threadID := "thread-artifact-order"
	session := s.ensureSession(threadID, nil)

	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}

	for _, name := range []string{"report.md", "chart.png"} {
		if err := os.WriteFile(filepath.Join(outputDir, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	for _, path := range []string{"/mnt/user-data/outputs/report.md", "/mnt/user-data/outputs/chart.png"} {
		if err := session.PresentFiles.Register(tools.PresentFile{
			Path:       path,
			SourcePath: filepath.Join(outputDir, filepath.Base(path)),
		}); err != nil {
			t.Fatalf("register %s: %v", path, err)
		}
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state is nil")
	}

	artifacts, ok := state.Values["artifacts"].([]string)
	if !ok {
		t.Fatalf("artifacts=%#v", state.Values["artifacts"])
	}
	if strings.Join(artifacts, ",") != "/mnt/user-data/outputs/report.md,/mnt/user-data/outputs/chart.png" {
		t.Fatalf("artifacts=%#v", artifacts)
	}
}

func TestPersistedSessionsReloadArtifactsNewestFirstFromDisk(t *testing.T) {
	s, _ := newCompatTestServer(t)
	threadID := "thread-artifact-reload-order"
	s.ensureSession(threadID, nil)

	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}

	oldPath := filepath.Join(outputDir, "older.md")
	newPath := filepath.Join(outputDir, "newer.md")
	if err := os.WriteFile(oldPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write old artifact: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0o644); err != nil {
		t.Fatalf("write new artifact: %v", err)
	}

	oldTime := time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(2 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old artifact: %v", err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatalf("chtimes new artifact: %v", err)
	}

	reloaded := &Server{
		sessions: make(map[string]*Session),
		runs:     make(map[string]*Run),
		dataRoot: s.dataRoot,
	}
	if err := reloaded.loadPersistedSessions(); err != nil {
		t.Fatalf("load persisted sessions: %v", err)
	}

	state := reloaded.getThreadState(threadID)
	if state == nil {
		t.Fatal("state is nil")
	}

	artifacts, ok := state.Values["artifacts"].([]string)
	if !ok {
		t.Fatalf("artifacts=%#v", state.Values["artifacts"])
	}
	if strings.Join(artifacts, ",") != "/mnt/user-data/outputs/newer.md,/mnt/user-data/outputs/older.md" {
		t.Fatalf("artifacts=%#v", artifacts)
	}
}

func TestThreadFilesListUploadMarkdownBeforeOriginalFile(t *testing.T) {
	s, _ := newCompatTestServer(t)
	threadID := "thread-upload-artifact-order"
	session := s.ensureSession(threadID, nil)

	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "report.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "report.md"), []byte("# Report"), 0o644); err != nil {
		t.Fatalf("write markdown companion: %v", err)
	}

	files := s.sessionFiles(session)
	paths := make([]string, 0, len(files))
	for _, file := range files {
		if strings.HasPrefix(file.Path, "/mnt/user-data/uploads/") {
			paths = append(paths, file.Path)
		}
	}
	if strings.Join(paths, ",") != "/mnt/user-data/uploads/report.md,/mnt/user-data/uploads/report.pdf" {
		t.Fatalf("paths=%#v", paths)
	}
}

func TestThreadStateArtifactsAppendUploadMarkdownAfterOutputs(t *testing.T) {
	t.Setenv(envIncludeUploadMarkdownArtifacts, "1")
	s, _ := newCompatTestServer(t)
	threadID := "thread-artifacts-include-upload-markdown"
	session := s.ensureSession(threadID, nil)

	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	reportPath := filepath.Join(outputDir, "report.md")
	if err := os.WriteFile(reportPath, []byte("# Report"), 0o644); err != nil {
		t.Fatalf("write output artifact: %v", err)
	}
	if err := session.PresentFiles.Register(tools.PresentFile{
		Path:       "/mnt/user-data/outputs/report.md",
		SourcePath: reportPath,
	}); err != nil {
		t.Fatalf("register output artifact: %v", err)
	}

	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "brief.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "brief.md"), []byte("# Brief"), 0o644); err != nil {
		t.Fatalf("write upload markdown: %v", err)
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state is nil")
	}

	artifacts, ok := state.Values["artifacts"].([]string)
	if !ok {
		t.Fatalf("artifacts=%#v", state.Values["artifacts"])
	}
	if strings.Join(artifacts, ",") != "/mnt/user-data/outputs/report.md,/mnt/user-data/uploads/brief.md" {
		t.Fatalf("artifacts=%#v", artifacts)
	}
}

func TestThreadStateArtifactsSortAcrossRootsByNewestModifiedTime(t *testing.T) {
	t.Setenv(envIncludeUploadMarkdownArtifacts, "1")
	s, _ := newCompatTestServer(t)
	threadID := "thread-artifacts-global-sort"
	s.ensureSession(threadID, nil)

	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	outputPath := filepath.Join(outputDir, "report.md")
	if err := os.WriteFile(outputPath, []byte("# Report"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "brief.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	uploadMarkdownPath := filepath.Join(uploadDir, "brief.md")
	if err := os.WriteFile(uploadMarkdownPath, []byte("# Brief"), 0o644); err != nil {
		t.Fatalf("write upload markdown: %v", err)
	}

	baseTime := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(outputPath, baseTime, baseTime); err != nil {
		t.Fatalf("chtimes output: %v", err)
	}
	uploadTime := baseTime.Add(5 * time.Minute)
	if err := os.Chtimes(uploadMarkdownPath, uploadTime, uploadTime); err != nil {
		t.Fatalf("chtimes upload markdown: %v", err)
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state is nil")
	}

	artifacts, ok := state.Values["artifacts"].([]string)
	if !ok {
		t.Fatalf("artifacts=%#v", state.Values["artifacts"])
	}
	if strings.Join(artifacts, ",") != "/mnt/user-data/uploads/brief.md,/mnt/user-data/outputs/report.md" {
		t.Fatalf("artifacts=%#v", artifacts)
	}
}

func TestThreadStateSkipsDeletedPresentedArtifacts(t *testing.T) {
	s, _ := newCompatTestServer(t)
	threadID := "thread-artifacts-skip-deleted"
	session := s.ensureSession(threadID, nil)

	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	livePath := filepath.Join(outputDir, "report.md")
	if err := os.WriteFile(livePath, []byte("# Report"), 0o644); err != nil {
		t.Fatalf("write live artifact: %v", err)
	}
	deletedPath := filepath.Join(outputDir, "deleted.md")
	if err := os.WriteFile(deletedPath, []byte("# Deleted"), 0o644); err != nil {
		t.Fatalf("write deleted artifact: %v", err)
	}

	for _, file := range []tools.PresentFile{
		{Path: "/mnt/user-data/outputs/report.md", SourcePath: livePath},
		{Path: "/mnt/user-data/outputs/deleted.md", SourcePath: deletedPath},
	} {
		if err := session.PresentFiles.Register(file); err != nil {
			t.Fatalf("register %s: %v", file.Path, err)
		}
	}
	if err := os.Remove(deletedPath); err != nil {
		t.Fatalf("remove deleted artifact: %v", err)
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state is nil")
	}

	artifacts, ok := state.Values["artifacts"].([]string)
	if !ok {
		t.Fatalf("artifacts=%#v", state.Values["artifacts"])
	}
	if strings.Join(artifacts, ",") != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("artifacts=%#v", artifacts)
	}
}
