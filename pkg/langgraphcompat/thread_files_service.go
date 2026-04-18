package langgraphcompat

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/tools"
)

const envIncludeUploadMarkdownArtifacts = "DEERFLOW_ARTIFACT_INCLUDE_UPLOAD_MARKDOWN"

func (s *Server) uploadedFilesState(threadID string) []map[string]any {
	entries, err := os.ReadDir(s.uploadsDir(threadID))
	if err != nil {
		return []map[string]any{}
	}
	files := make([]map[string]any, 0, len(entries))
	companionFiles := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.EqualFold(filepath.Ext(name), ".md") {
			base := strings.TrimSuffix(name, filepath.Ext(name))
			for _, ext := range []string{".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx", ".csv", ".tsv", ".json", ".txt", ".log", ".ini", ".cfg", ".conf", ".env", ".toml", ".xml", ".html", ".htm", ".xhtml", ".yaml", ".yml"} {
				if _, err := os.Stat(filepath.Join(s.uploadsDir(threadID), base+ext)); err == nil {
					companionFiles[name] = struct{}{}
					break
				}
			}
		}
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := companionFiles[entry.Name()]; ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, s.uploadInfo(threadID, filepath.Join(s.uploadsDir(threadID), entry.Name()), entry.Name(), info.Size(), info.ModTime().Unix()))
	}
	sort.Slice(files, func(i, j int) bool {
		li := toInt64(files[i]["modified"])
		lj := toInt64(files[j]["modified"])
		if li == lj {
			return asString(files[i]["filename"]) < asString(files[j]["filename"])
		}
		return li > lj
	})
	return files
}

func (s *Server) messageUploadedFilesState(session *Session) []map[string]any {
	if session == nil {
		return []map[string]any{}
	}
	files := s.uploadedFilesState(session.ThreadID)
	if len(files) > 0 {
		return files
	}
	if restored := uploadedFilesFromMetadata(session.Metadata["uploaded_files"]); len(restored) > 0 {
		return restored
	}
	return []map[string]any{}
}

func (s *Server) sessionArtifactPaths(session *Session) []string {
	files := s.collectArtifactFiles(session)
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	return paths
}

func sessionArtifactPaths(session *Session) []string {
	if session == nil {
		return nil
	}
	seen := make(map[string]struct{})
	paths := make([]string, 0)
	if session.PresentFiles != nil {
		for _, file := range session.PresentFiles.List() {
			addArtifactPath(&paths, seen, file.Path)
		}
	}
	for _, message := range session.Messages {
		for _, path := range messageArtifactPaths(message) {
			addArtifactPath(&paths, seen, path)
		}
	}
	for _, path := range anyStringSlice(session.Metadata["artifacts"]) {
		addArtifactPath(&paths, seen, path)
	}
	return paths
}

func (s *Server) collectSessionFiles(session *Session) []tools.PresentFile {
	if session == nil {
		return nil
	}
	seen := make(map[string]struct{})
	files := make([]tools.PresentFile, 0)
	add := func(file tools.PresentFile) {
		file = s.normalizePresentFile(session.ThreadID, file)
		if file.ID == "" || file.Path == "" || !presentFileExists(file) {
			return
		}
		if _, ok := seen[file.Path]; ok {
			return
		}
		seen[file.Path] = struct{}{}
		files = append(files, file)
	}
	if session.PresentFiles != nil {
		for _, file := range session.PresentFiles.List() {
			add(file)
		}
	}
	for _, root := range []struct {
		dir          string
		virtual      string
		markdownOnly bool
	}{
		{dir: s.uploadsDir(session.ThreadID), virtual: "/mnt/user-data/uploads", markdownOnly: false},
		{dir: s.workspaceDir(session.ThreadID), virtual: "/mnt/user-data/workspace", markdownOnly: false},
		{dir: s.outputsDir(session.ThreadID), virtual: "/mnt/user-data/outputs", markdownOnly: false},
	} {
		for _, file := range collectPresentFiles(root.dir, root.virtual, root.markdownOnly) {
			add(file)
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].CreatedAt.Equal(files[j].CreatedAt) {
			return files[i].Path < files[j].Path
		}
		return files[i].CreatedAt.After(files[j].CreatedAt)
	})
	return files
}

func (s *Server) collectArtifactFiles(session *Session) []tools.PresentFile {
	if session == nil {
		return nil
	}
	files := make([]tools.PresentFile, 0)
	seen := make(map[string]struct{})
	addExisting := func(file tools.PresentFile) {
		file = s.normalizePresentFile(session.ThreadID, file)
		if file.ID == "" || file.Path == "" || !presentFileExists(file) {
			return
		}
		if _, ok := seen[file.Path]; ok {
			return
		}
		seen[file.Path] = struct{}{}
		files = append(files, file)
	}
	addKnownPath := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		files = append(files, tools.PresentFile{
			ID:          autodiscoveredPresentFileID(path),
			Path:        path,
			VirtualPath: path,
			ArtifactURL: artifactURLForThread(session.ThreadID, path),
			Extension:   strings.ToLower(filepath.Ext(path)),
		})
	}
	if session.PresentFiles != nil {
		for _, file := range session.PresentFiles.List() {
			addExisting(file)
		}
	}
	for _, message := range session.Messages {
		for _, path := range messageArtifactPaths(message) {
			addKnownPath(path)
		}
	}
	for _, path := range anyStringSlice(session.Metadata["artifacts"]) {
		addKnownPath(path)
	}
	autoFiles := make([]tools.PresentFile, 0)
	for _, file := range collectPresentFiles(s.outputsDir(session.ThreadID), "/mnt/user-data/outputs", false) {
		file = s.normalizePresentFile(session.ThreadID, file)
		if file.ID == "" || file.Path == "" || !presentFileExists(file) {
			continue
		}
		if _, ok := seen[file.Path]; ok {
			continue
		}
		autoFiles = append(autoFiles, file)
	}
	if includeUploadMarkdownArtifacts() {
		for _, file := range collectPresentFiles(s.uploadsDir(session.ThreadID), "/mnt/user-data/uploads", true) {
			file = s.normalizePresentFile(session.ThreadID, file)
			if file.ID == "" || file.Path == "" || !presentFileExists(file) {
				continue
			}
			if _, ok := seen[file.Path]; ok {
				continue
			}
			autoFiles = append(autoFiles, file)
		}
	}
	sort.Slice(autoFiles, func(i, j int) bool {
		if autoFiles[i].CreatedAt.Equal(autoFiles[j].CreatedAt) {
			return autoFiles[i].Path < autoFiles[j].Path
		}
		return autoFiles[i].CreatedAt.After(autoFiles[j].CreatedAt)
	})
	files = append(files, autoFiles...)
	return files
}

func includeUploadMarkdownArtifacts() bool {
	raw := strings.TrimSpace(os.Getenv(envIncludeUploadMarkdownArtifacts))
	if raw == "" {
		return false
	}
	parsed, err := strconv.ParseBool(raw)
	return err == nil && parsed
}
