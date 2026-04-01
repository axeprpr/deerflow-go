package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

var presentFileSeq uint64

type PresentFile struct {
	ID          string    `json:"id"`
	Path        string    `json:"path"`
	SourcePath  string    `json:"-"`
	Description string    `json:"description,omitempty"`
	MimeType    string    `json:"mime_type"`
	Content     string    `json:"content,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type PresentFileRegistry struct {
	mu    sync.RWMutex
	files map[string]PresentFile
}

func NewPresentFileRegistry() *PresentFileRegistry {
	return &PresentFileRegistry{
		files: make(map[string]PresentFile),
	}
}

func (r *PresentFileRegistry) Register(file PresentFile) error {
	if r == nil {
		return fmt.Errorf("present file registry is nil")
	}

	path := strings.TrimSpace(file.Path)
	if path == "" {
		return fmt.Errorf("path is required")
	}

	sourcePath := strings.TrimSpace(file.SourcePath)
	if sourcePath == "" {
		sourcePath = path
	}

	absSourcePath, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	normalizedPath := normalizePresentRegistryPath(path)
	data, err := os.ReadFile(absSourcePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	mimeType := strings.TrimSpace(file.MimeType)
	if mimeType == "" {
		mimeType = detectMimeType(absSourcePath, data)
	}

	content := file.Content
	if strings.TrimSpace(content) == "" {
		content = encodePresentFileContent(mimeType, data)
	}

	description := strings.TrimSpace(file.Description)

	r.mu.Lock()
	defer r.mu.Unlock()

	id := strings.TrimSpace(file.ID)
	if id == "" {
		if existingID, existing, ok := r.findByPathLocked(normalizedPath); ok {
			id = existingID
			if file.CreatedAt.IsZero() {
				file.CreatedAt = existing.CreatedAt
			}
		} else {
			id = newPresentFileID()
		}
	}

	createdAt := file.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	r.files[id] = PresentFile{
		ID:          id,
		Path:        normalizedPath,
		Description: description,
		MimeType:    mimeType,
		Content:     content,
		CreatedAt:   createdAt,
	}
	return nil
}

func (r *PresentFileRegistry) List() []PresentFile {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	files := make([]PresentFile, 0, len(r.files))
	for _, file := range r.files {
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].CreatedAt.Equal(files[j].CreatedAt) {
			return files[i].ID < files[j].ID
		}
		return files[i].CreatedAt.Before(files[j].CreatedAt)
	})
	return files
}

func (r *PresentFileRegistry) Get(id string) (PresentFile, bool) {
	if r == nil {
		return PresentFile{}, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	file, ok := r.files[strings.TrimSpace(id)]
	return file, ok
}

func (r *PresentFileRegistry) Clear() {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.files = make(map[string]PresentFile)
}

func (r *PresentFileRegistry) findByPathLocked(path string) (string, PresentFile, bool) {
	path = normalizePresentRegistryPath(path)
	for id, file := range r.files {
		if file.Path == path {
			return id, file, true
		}
	}
	return "", PresentFile{}, false
}

func PresentFileTool(registry *PresentFileRegistry) models.Tool {
	return buildPresentFileTool(registry, "present_file", false)
}

func PresentFilesTool(registry *PresentFileRegistry) models.Tool {
	return buildPresentFileTool(registry, "present_files", true)
}

func buildPresentFileTool(registry *PresentFileRegistry, name string, multiple bool) models.Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"description": map[string]any{"type": "string", "description": "Short description shown in the UI"},
			"mime_type":   map[string]any{"type": "string", "description": "Optional MIME type override"},
		},
	}
	if multiple {
		schema["properties"].(map[string]any)["filepaths"] = map[string]any{
			"type":        "array",
			"description": "Paths to generated files",
			"items":       map[string]any{"type": "string"},
		}
		schema["required"] = []any{"filepaths"}
	} else {
		schema["properties"].(map[string]any)["path"] = map[string]any{"type": "string", "description": "Path to the generated file"}
		schema["required"] = []any{"path"}
	}

	return models.Tool{
		Name:        name,
		Description: "Register generated output files so the UI can display them.",
		Groups:      []string{"builtin", "file_ops"},
		InputSchema: schema,
		Handler: func(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
			return presentFiles(ctx, registry, call)
		},
	}
}

func presentFiles(ctx context.Context, registry *PresentFileRegistry, call models.ToolCall) (models.ToolResult, error) {
	if registry == nil {
		err := fmt.Errorf("present file registry is required")
		return models.ToolResult{
			CallID:   call.ID,
			ToolName: call.Name,
			Status:   models.CallStatusFailed,
			Error:    err.Error(),
		}, err
	}

	filepaths := extractPresentFilePaths(call.Arguments)
	if len(filepaths) == 0 {
		err := fmt.Errorf("at least one file path is required")
		return models.ToolResult{
			CallID:   call.ID,
			ToolName: call.Name,
			Status:   models.CallStatusFailed,
			Error:    err.Error(),
		}, err
	}

	description, _ := call.Arguments["description"].(string)
	mimeType, _ := call.Arguments["mime_type"].(string)
	registeredFiles := make([]PresentFile, 0, len(filepaths))
	for _, path := range filepaths {
		displayPath, sourcePath, err := normalizePresentedFilePath(ctx, path)
		if err != nil {
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusFailed,
				Error:    err.Error(),
			}, err
		}
		file := PresentFile{
			Path:        displayPath,
			SourcePath:  sourcePath,
			Description: description,
			MimeType:    mimeType,
		}
		if err := registry.Register(file); err != nil {
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusFailed,
				Error:    err.Error(),
			}, err
		}

		registered, ok := latestRegisteredFile(registry, displayPath)
		if !ok {
			err := fmt.Errorf("registered file not found")
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusFailed,
				Error:    err.Error(),
			}, err
		}
		registeredFiles = append(registeredFiles, registered)
	}

	resultData := map[string]any{
		"filepaths": presentRegistryPaths(registeredFiles),
	}
	content := fmt.Sprintf("Presented %d file(s)", len(registeredFiles))
	if len(registeredFiles) == 1 {
		registered := registeredFiles[0]
		content = fmt.Sprintf("Registered file %s", registered.Path)
		resultData["id"] = registered.ID
		resultData["path"] = registered.Path
		resultData["description"] = registered.Description
		resultData["mime_type"] = registered.MimeType
		resultData["created_at"] = registered.CreatedAt.Format(time.RFC3339Nano)
	}

	return models.ToolResult{
		CallID:   call.ID,
		ToolName: call.Name,
		Status:   models.CallStatusCompleted,
		Content:  content,
		Data:     resultData,
	}, nil
}

func extractPresentFilePaths(args map[string]any) []string {
	if len(args) == 0 {
		return nil
	}
	if path, _ := args["path"].(string); strings.TrimSpace(path) != "" {
		return []string{strings.TrimSpace(path)}
	}
	rawPaths, _ := args["filepaths"].([]any)
	paths := make([]string, 0, len(rawPaths))
	for _, raw := range rawPaths {
		path, _ := raw.(string)
		path = strings.TrimSpace(path)
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func presentRegistryPaths(files []PresentFile) []string {
	if len(files) == 0 {
		return nil
	}
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	return paths
}

func latestRegisteredFile(registry *PresentFileRegistry, path string) (PresentFile, bool) {
	if registry == nil {
		return PresentFile{}, false
	}

	normalizedPath := normalizePresentRegistryPath(path)
	for _, file := range registry.List() {
		if file.Path == normalizedPath {
			return file, true
		}
	}
	return PresentFile{}, false
}

func normalizePresentRegistryPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "/mnt/") {
		return filepath.ToSlash(filepath.Clean(path))
	}
	if absPath, err := filepath.Abs(path); err == nil {
		return filepath.Clean(absPath)
	}
	return filepath.Clean(path)
}

func normalizePresentedFilePath(ctx context.Context, path string) (string, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", fmt.Errorf("path is required")
	}

	sourcePath := ResolveVirtualPath(ctx, path)
	if sourcePath == "" {
		sourcePath = path
	}
	absSourcePath, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", "", fmt.Errorf("resolve path: %w", err)
	}

	threadID := strings.TrimSpace(ThreadIDFromContext(ctx))
	if threadID == "" {
		return normalizePresentRegistryPath(path), absSourcePath, nil
	}

	outputsDir := filepath.Join(threadDataRootFromThreadID(threadID), "outputs")
	relPath, err := filepath.Rel(outputsDir, absSourcePath)
	if err != nil {
		return "", "", fmt.Errorf("resolve presented file: %w", err)
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("only files in /mnt/user-data/outputs can be presented: %s", path)
	}
	virtualPath := "/mnt/user-data/outputs"
	if relPath != "." {
		virtualPath += "/" + filepath.ToSlash(relPath)
	}
	return virtualPath, absSourcePath, nil
}

func newPresentFileID() string {
	seq := atomic.AddUint64(&presentFileSeq, 1)
	return fmt.Sprintf("file_%d_%d", time.Now().UTC().UnixNano(), seq)
}

func detectMimeType(path string, data []byte) string {
	if ext := strings.TrimSpace(filepath.Ext(path)); ext != "" {
		if detected := mime.TypeByExtension(ext); detected != "" {
			return detected
		}
	}
	if len(data) == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(data)
}

func encodePresentFileContent(mimeType string, data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if isTextMimeType(mimeType) && utf8.Valid(data) {
		return string(data)
	}
	return base64.StdEncoding.EncodeToString(data)
}

func isTextMimeType(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	switch mimeType {
	case "application/json", "application/xml", "application/yaml", "application/x-yaml", "image/svg+xml":
		return true
	default:
		return false
	}
}
