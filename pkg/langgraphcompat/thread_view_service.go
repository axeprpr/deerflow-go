package langgraphcompat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func (s *Server) threadValues(session *Session) map[string]any {
	values := copyMetadataMap(session.Values)
	if values == nil {
		values = map[string]any{}
	}
	agentName := strings.TrimSpace(stringValue(session.Metadata["agent_name"]))
	if agentName == "" {
		agentName = strings.TrimSpace(stringValue(values["created_agent_name"]))
	}
	threadKind := "chat"
	routePath := "/workspace/chats/" + session.ThreadID
	if agentName != "" {
		threadKind = "agent"
		routePath = "/workspace/agents/" + agentName + "/chats/" + session.ThreadID
	}
	for key, value := range map[string]any{
		"title":          stringValue(session.Metadata["title"]),
		"artifacts":      s.sessionArtifactPaths(session),
		"uploaded_files": s.messageUploadedFilesState(session),
		"viewed_images":  viewedImagesFromMetadata(session.Metadata["viewed_images"]),
		"agent_name":     agentName,
		"thread_kind":    threadKind,
		"route_path":     routePath,
	} {
		if _, exists := values[key]; !exists {
			values[key] = value
		}
	}
	if _, exists := values["todos"]; !exists {
		todos := todosFromMetadata(session.Metadata["todos"])
		if len(session.Todos) > 0 {
			todos = todosToAny(session.Todos)
		}
		values["todos"] = todos
	}
	if _, exists := values["sandbox"]; !exists {
		sandboxState := mapFromMetadata(session.Metadata["sandbox"])
		if len(sandboxState) == 0 {
			sandboxState = map[string]any{"sandbox_id": "local"}
		}
		values["sandbox"] = sandboxState
	}
	values["thread_data"] = s.restoredThreadDataState(session)
	values["messages"] = s.messagesToLangChain(session.Messages)
	return values
}

func (s *Server) threadConfigurable(session *Session) map[string]any {
	configurable := defaultThreadConfig(session.ThreadID)
	for key, value := range copyMetadataMap(session.Configurable) {
		configurable[key] = value
	}
	mode := strings.TrimSpace(stringValue(configurable["mode"]))
	if mode == "" {
		mode = deriveThreadMode(session.Metadata)
	}
	scope := threadMemoryScopeFromMetadata(session.Metadata).Merge(threadMemoryScopeFromConfigurable(session.Configurable))
	thinkingEnabled := mode != "flash"
	isPlanMode := mode == "pro" || mode == "ultra"
	subagentEnabled := mode == "ultra"
	for key, value := range map[string]any{
		"thread_id":        session.ThreadID,
		"agent_type":       stringValue(session.Metadata["agent_type"]),
		"agent_name":       stringValue(session.Metadata["agent_name"]),
		"model_name":       stringValue(session.Metadata["model_name"]),
		"mode":             mode,
		"reasoning_effort": deriveReasoningEffort(session.Metadata, mode),
		"is_plan_mode":     isPlanMode,
		"thinking_enabled": thinkingEnabled,
		"subagent_enabled": subagentEnabled,
		"memory_user_id":   scope.UserID,
		"memory_group_id":  scope.GroupID,
		"memory_namespace": scope.Namespace,
	} {
		if _, exists := configurable[key]; !exists || configurable[key] == "" {
			configurable[key] = value
		}
	}
	if subagentEnabled {
		if _, exists := configurable["max_concurrent_subagents"]; !exists {
			configurable["max_concurrent_subagents"] = int64(defaultGatewaySubagentMaxConcurrent)
		}
	}
	if value, ok := float64FromAny(session.Metadata["temperature"]); ok {
		if _, exists := configurable["temperature"]; !exists {
			configurable["temperature"] = value
		}
	}
	if value := toInt64(session.Metadata["max_tokens"]); value > 0 {
		if _, exists := configurable["max_tokens"]; !exists {
			configurable["max_tokens"] = value
		}
	}
	if value, ok := session.Metadata["thinking_enabled"].(bool); ok {
		if session.Configurable == nil {
			configurable["thinking_enabled"] = value
		} else if _, exists := session.Configurable["thinking_enabled"]; !exists {
			configurable["thinking_enabled"] = value
		}
	}
	if value, ok := session.Metadata["is_plan_mode"].(bool); ok {
		if session.Configurable == nil {
			configurable["is_plan_mode"] = value
		} else if _, exists := session.Configurable["is_plan_mode"]; !exists {
			configurable["is_plan_mode"] = value
		}
	}
	if value, ok := session.Metadata["subagent_enabled"].(bool); ok {
		if session.Configurable == nil {
			configurable["subagent_enabled"] = value
		} else if _, exists := session.Configurable["subagent_enabled"]; !exists {
			configurable["subagent_enabled"] = value
		}
	}
	if value := toInt64(session.Metadata["max_concurrent_subagents"]); value > 0 {
		if session.Configurable == nil {
			configurable["max_concurrent_subagents"] = value
		} else if _, exists := session.Configurable["max_concurrent_subagents"]; !exists {
			configurable["max_concurrent_subagents"] = value
		}
	}
	return configurable
}

func threadMetadata(session *Session) map[string]any {
	metadata := map[string]any{
		"thread_id": session.ThreadID,
	}
	for key, value := range session.Metadata {
		metadata[key] = value
	}
	if _, ok := metadata["step"]; !ok {
		metadata["step"] = 0
	}
	return metadata
}

func checkpointObjectFromMetadata(metadata map[string]any, prefix string) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := map[string]any{}
	if value := strings.TrimSpace(stringValue(metadata[prefix+"checkpoint_id"])); value != "" {
		out["checkpoint_id"] = value
	}
	if value := strings.TrimSpace(stringValue(metadata[prefix+"checkpoint_ns"])); value != "" {
		out["checkpoint_ns"] = value
	}
	if value := strings.TrimSpace(stringValue(metadata[prefix+"checkpoint_thread_id"])); value != "" {
		out["thread_id"] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstNonZeroTime(times ...time.Time) time.Time {
	for _, ts := range times {
		if !ts.IsZero() {
			return ts
		}
	}
	return time.Time{}
}

func float64FromAny(v any) (float64, bool) {
	switch value := v.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func deriveThreadMode(metadata map[string]any) string {
	if mode := strings.TrimSpace(stringValue(metadata["mode"])); mode != "" {
		return mode
	}
	subagentEnabled, _ := metadata["subagent_enabled"].(bool)
	isPlanMode, _ := metadata["is_plan_mode"].(bool)
	thinkingEnabled, thinkingSet := metadata["thinking_enabled"].(bool)
	if subagentEnabled {
		return "ultra"
	}
	if isPlanMode {
		return "pro"
	}
	if thinkingSet && thinkingEnabled {
		return "thinking"
	}
	return "flash"
}

func deriveReasoningEffort(metadata map[string]any, mode string) string {
	if effort := strings.TrimSpace(stringValue(metadata["reasoning_effort"])); effort != "" {
		return effort
	}
	switch mode {
	case "ultra":
		return "high"
	case "pro":
		return "medium"
	case "thinking":
		return "low"
	default:
		return "minimal"
	}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func collectPresentFiles(root, virtualPrefix string, markdownOnly bool) []tools.PresentFile {
	entries := collectArtifactFiles(root, virtualPrefix)
	files := make([]tools.PresentFile, 0, len(entries))
	for _, file := range entries {
		if markdownOnly && !strings.EqualFold(filepath.Ext(file.Path), ".md") {
			continue
		}
		files = append(files, file)
	}
	return files
}

func (s *Server) normalizePresentFile(threadID string, file tools.PresentFile) tools.PresentFile {
	file.Path = strings.TrimSpace(file.Path)
	if file.Path == "" {
		return tools.PresentFile{}
	}
	if strings.TrimSpace(file.SourcePath) == "" {
		file.SourcePath = s.resolveArtifactPath(threadID, file.Path)
	}
	if file.ID == "" {
		file.ID = autodiscoveredPresentFileID(file.Path)
	}
	if info, err := os.Stat(file.SourcePath); err == nil {
		if file.CreatedAt.IsZero() {
			file.CreatedAt = info.ModTime().UTC()
		}
		if file.Size == 0 {
			file.Size = info.Size()
		}
	}
	if file.MimeType == "" {
		file.MimeType = detectArtifactMimeType(file.SourcePath)
	}
	file.VirtualPath = file.Path
	file.ArtifactURL = artifactURLForThread(threadID, file.Path)
	file.Extension = strings.ToLower(filepath.Ext(file.Path))
	return file
}

func presentFileExists(file tools.PresentFile) bool {
	path := strings.TrimSpace(file.SourcePath)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func anyStringSlice(v any) []string {
	switch items := v.(type) {
	case []string:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if strings.TrimSpace(item) != "" {
				out = append(out, strings.TrimSpace(item))
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if text := strings.TrimSpace(stringFromAny(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func anySlice(v any) []any {
	switch items := v.(type) {
	case []any:
		return items
	case []string:
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, item)
		}
		return out
	default:
		if v != nil {
			return []any{v}
		}
		return nil
	}
}
