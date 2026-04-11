package langgraphcompat

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) handleChannelsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.gatewayChannelStatus())
}

func (s *Server) handleChannelRestart(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	status, success, message := s.restartGatewayChannel(name)
	writeJSON(w, status, map[string]any{
		"success": success,
		"message": message,
	})
}

func (s *Server) handleGatewayThreadDelete(w http.ResponseWriter, r *http.Request) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	if threadID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "thread_id is required"})
		return
	}
	if err := validateThreadID(threadID); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": err.Error()})
		return
	}
	if err := os.RemoveAll(s.threadRoot(threadID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to delete local thread data"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "local thread data deleted",
	})
}

func (s *Server) handleUploadsCreate(w http.ResponseWriter, r *http.Request) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	if threadID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "thread_id is required"})
		return
	}
	if err := validateThreadID(threadID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid multipart form"})
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "no files uploaded"})
		return
	}

	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to create upload dir"})
		return
	}

	infos := make([]map[string]any, 0, len(files))
	for _, fh := range files {
		info, err := s.saveUploadedFile(threadID, uploadDir, fh)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
			return
		}
		infos = append(infos, info)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"files":   infos,
		"message": "files uploaded",
	})
}

func (s *Server) handleUploadsList(w http.ResponseWriter, r *http.Request) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	if threadID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "thread_id is required"})
		return
	}

	files := s.uploadedFilesState(threadID)

	writeJSON(w, http.StatusOK, map[string]any{
		"files": files,
		"count": len(files),
	})
}

func (s *Server) handleUploadsDelete(w http.ResponseWriter, r *http.Request) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	filename := sanitizeFilename(r.PathValue("filename"))
	if threadID == "" || filename == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request"})
		return
	}

	target := filepath.Join(s.uploadsDir(threadID), filename)
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "failed to delete file"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "file deleted",
	})
}

func (s *Server) handleArtifactGet(w http.ResponseWriter, r *http.Request) {
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	artifactPath := strings.TrimSpace(r.PathValue("artifact_path"))
	if threadID == "" || artifactPath == "" {
		http.NotFound(w, r)
		return
	}

	absPath := s.resolveArtifactPath(threadID, artifactPath)
	if !s.artifactAllowed(threadID, absPath) {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	if strings.EqualFold(filepath.Ext(absPath), ".skill") && !strings.EqualFold(r.URL.Query().Get("download"), "true") {
		preview, err := readSkillArchivePreview(absPath)
		if err == nil && strings.TrimSpace(preview) != "" {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			_, _ = io.WriteString(w, preview)
			return
		}
	}
	if strings.EqualFold(r.URL.Query().Get("download"), "true") {
		w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(absPath)+`"`)
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	mimeType := detectArtifactMimeType(absPath)
	if shouldForceArtifactDownload(mimeType) || strings.EqualFold(r.URL.Query().Get("download"), "true") {
		w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(absPath)+`"`)
	}
	if strings.HasPrefix(mimeType, "text/") && !strings.Contains(strings.ToLower(mimeType), "charset=") {
		mimeType += "; charset=utf-8"
	}
	if mimeType != "" {
		w.Header().Set("Content-Type", mimeType)
	}
	http.ServeContent(w, r, filepath.Base(absPath), info.ModTime(), bytes.NewReader(content))
}
