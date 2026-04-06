package langgraphcompat

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/internal/webui"
)

func (s *Server) handleEmbeddedUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}

	uiFS, ok := s.embeddedUIFS()
	if !ok {
		http.NotFound(w, r)
		return
	}

	requestPath := cleanUIPath(r.URL.Path)
	if requestPath == "/" {
		s.serveUIFile(w, r, uiFS, "index.html")
		return
	}

	trimmed := strings.TrimPrefix(requestPath, "/")
	if fileExists(uiFS, trimmed) {
		s.serveUIFile(w, r, uiFS, trimmed)
		return
	}
	if fileExists(uiFS, trimmed+".html") {
		s.serveUIFile(w, r, uiFS, trimmed+".html")
		return
	}
	if fileExists(uiFS, path.Join(trimmed, "index.html")) {
		s.serveUIFile(w, r, uiFS, path.Join(trimmed, "index.html"))
		return
	}

	// SPA fallback for client-side routes such as /workspace/chats/<id>.
	s.serveUIFile(w, r, uiFS, "index.html")
}

func (s *Server) embeddedUIFS() (fs.FS, bool) {
	if s != nil && s.frontendFS != nil {
		if fileExists(s.frontendFS, "index.html") {
			return s.frontendFS, true
		}
	}
	uiFS, err := webui.FS()
	if err != nil {
		return nil, false
	}
	if !fileExists(uiFS, "index.html") {
		return nil, false
	}
	return uiFS, true
}

func (s *Server) serveUIFile(w http.ResponseWriter, r *http.Request, uiFS fs.FS, name string) {
	data, err := fs.ReadFile(uiFS, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ctype := mime.TypeByExtension(filepath.Ext(name)); ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	http.ServeContent(w, r, name, time.Time{}, strings.NewReader(string(data)))
}

func fileExists(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && !info.IsDir()
}

func cleanUIPath(raw string) string {
	if raw == "" {
		return "/"
	}
	cleaned := path.Clean(raw)
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned
}
