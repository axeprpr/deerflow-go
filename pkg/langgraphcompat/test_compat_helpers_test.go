package langgraphcompat

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func performCompatRequest(t *testing.T, handler any, method, target string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	switch h := any(handler).(type) {
	case *httptest.Server:
		req, err := http.NewRequest(method, h.URL+target, body)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		resp, err := h.Client().Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer resp.Body.Close()
		rec := httptest.NewRecorder()
		rec.Code = resp.StatusCode
		for key, values := range resp.Header {
			for _, value := range values {
				rec.Header().Add(key, value)
			}
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		_, _ = rec.Body.Write(data)
		return rec
	case http.Handler:
		req := httptest.NewRequest(method, target, body)
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	default:
		t.Fatalf("unsupported handler type %T", handler)
		return nil
	}
}

func (s *Server) sessionFiles(session *Session) []tools.PresentFile {
	if s == nil || session == nil {
		return nil
	}
	files := make([]tools.PresentFile, 0)
	if session.PresentFiles != nil {
		files = append(files, session.PresentFiles.List()...)
	}
	addDiskFiles := func(root, virtualPrefix string) {
		entries, err := listPresentFiles(root, virtualPrefix)
		if err != nil {
			return
		}
		files = append(files, entries...)
	}
	addDiskFiles(s.uploadsDir(session.ThreadID), "/mnt/user-data/uploads")
	addDiskFiles(s.workspaceDir(session.ThreadID), "/mnt/user-data/workspace")
	addDiskFiles(s.outputsDir(session.ThreadID), "/mnt/user-data/outputs")
	return sortPresentFiles(files)
}

func listPresentFiles(root, virtualPrefix string) ([]tools.PresentFile, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	files := make([]tools.PresentFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, tools.PresentFile{
			Path:       strings.TrimRight(virtualPrefix, "/") + "/" + entry.Name(),
			SourcePath: filepath.Join(root, entry.Name()),
			CreatedAt:  info.ModTime(),
		})
	}
	return files, nil
}

func sortPresentFiles(files []tools.PresentFile) []tools.PresentFile {
	sort.Slice(files, func(i, j int) bool {
		ti := files[i].CreatedAt
		tj := files[j].CreatedAt
		if ti.Equal(tj) {
			return files[i].Path < files[j].Path
		}
		if ti.IsZero() {
			ti = time.Time{}
		}
		if tj.IsZero() {
			tj = time.Time{}
		}
		return ti.After(tj)
	})
	return files
}
