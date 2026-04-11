package langgraphcompat

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func selectThreadFields(thread map[string]any, selectFields []string) map[string]any {
	if len(selectFields) == 0 {
		return thread
	}
	selected := make(map[string]any, len(selectFields))
	for _, field := range selectFields {
		field = normalizeThreadFieldName(field)
		if field == "" {
			continue
		}
		if value, ok := thread[field]; ok {
			selected[field] = value
		}
	}
	if _, ok := selected["thread_id"]; !ok {
		if value, exists := thread["thread_id"]; exists {
			selected["thread_id"] = value
		}
	}
	if _, ok := selected["created_at"]; !ok {
		if value, exists := thread["created_at"]; exists {
			selected["created_at"] = value
		}
	}
	if _, ok := selected["updated_at"]; !ok {
		if value, exists := thread["updated_at"]; exists {
			selected["updated_at"] = value
		}
	}
	return selected
}

func normalizeThreadFieldName(field string) string {
	switch strings.TrimSpace(field) {
	case "threadId":
		return "thread_id"
	case "createdAt":
		return "created_at"
	case "updatedAt":
		return "updated_at"
	case "assistantId":
		return "assistant_id"
	case "graphId":
		return "graph_id"
	case "runId":
		return "run_id"
	case "agentName":
		return "agent_name"
	case "agentType":
		return "agent_type"
	case "modelName":
		return "model_name"
	case "reasoningEffort":
		return "reasoning_effort"
	case "thinkingEnabled":
		return "thinking_enabled"
	case "isPlanMode":
		return "is_plan_mode"
	case "subagentEnabled":
		return "subagent_enabled"
	case "maxTokens":
		return "max_tokens"
	case "viewedImages":
		return "viewed_images"
	case "threadData":
		return "thread_data"
	case "uploadedFiles":
		return "uploaded_files"
	case "checkpointId":
		return "checkpoint_id"
	case "parentCheckpointId":
		return "parent_checkpoint_id"
	case "checkpointNs":
		return "checkpoint_ns"
	case "parentCheckpointNs":
		return "parent_checkpoint_ns"
	case "checkpointThreadId":
		return "checkpoint_thread_id"
	case "parentCheckpointThreadId":
		return "parent_checkpoint_thread_id"
	case "parentCheckpoint":
		return "parent_checkpoint"
	default:
		return strings.TrimSpace(field)
	}
}

func boolRank(v any) int {
	value, _ := v.(bool)
	if value {
		return 1
	}
	return 0
}

func numberFromAny(v any) float64 {
	value, _ := float64FromAny(v)
	return value
}

func threadIDStatusCode(r *http.Request) int {
	if r != nil && strings.HasPrefix(strings.TrimSpace(r.URL.Path), "/api/") {
		return http.StatusUnprocessableEntity
	}
	return http.StatusBadRequest
}

func (s *Server) threadMatchesSearch(session *Session, thread map[string]any, query, status string, metadataFilter, valuesFilter map[string]any) bool {
	if status != "" && !strings.EqualFold(strings.TrimSpace(asString(thread["status"])), strings.TrimSpace(status)) {
		return false
	}
	if len(metadataFilter) > 0 && !mapContainsFilter(mapFromAny(thread["metadata"]), metadataFilter) {
		return false
	}
	if len(valuesFilter) > 0 && !mapContainsFilter(mapFromAny(thread["values"]), valuesFilter) {
		return false
	}
	return s.threadMatchesQuery(session, thread, query)
}

func (s *Server) threadMatchesQuery(session *Session, thread map[string]any, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	candidates := make([]string, 0, 32)
	appendSearchStrings(&candidates, thread)
	if session != nil {
		appendSearchStrings(&candidates, todosToAny(session.Todos))
		for _, msg := range session.Messages {
			appendSearchStrings(&candidates, msg.Content)
			appendSearchStrings(&candidates, msg.Metadata)
			for _, call := range msg.ToolCalls {
				appendSearchStrings(&candidates, call.Name, call.Arguments)
			}
			if msg.ToolResult != nil {
				appendSearchStrings(&candidates, msg.ToolResult.ToolName, msg.ToolResult.Content, msg.ToolResult.Error, msg.ToolResult.Data)
			}
		}
	}
	for _, value := range candidates {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return s.threadDiskMatchesQuery(asString(thread["thread_id"]), query)
}

func appendSearchStrings(dst *[]string, values ...any) {
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			text := strings.TrimSpace(typed)
			if text != "" {
				*dst = append(*dst, text)
			}
		case map[string]any:
			for _, nested := range typed {
				appendSearchStrings(dst, nested)
			}
		case []map[string]any:
			for _, nested := range typed {
				appendSearchStrings(dst, nested)
			}
		case []any:
			for _, nested := range typed {
				appendSearchStrings(dst, nested)
			}
		default:
			if value == nil {
				continue
			}
			if encoded, err := json.Marshal(value); err == nil && string(encoded) != "null" {
				*dst = append(*dst, string(encoded))
			}
		}
	}
}

func mapContainsFilter(actual map[string]any, expected map[string]any) bool {
	for key, want := range expected {
		got, ok := actual[key]
		if !ok {
			return false
		}
		wantMap, wantIsMap := want.(map[string]any)
		gotMap, gotIsMap := got.(map[string]any)
		if wantIsMap {
			if !gotIsMap || !mapContainsFilter(gotMap, wantMap) {
				return false
			}
			continue
		}
		if !valuesEqualForSearch(got, want) {
			return false
		}
	}
	return true
}

func valuesEqualForSearch(actual, expected any) bool {
	switch want := expected.(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(asString(actual)), strings.TrimSpace(want))
	case bool:
		value, ok := actual.(bool)
		return ok && value == want
	default:
		return strings.TrimSpace(asString(actual)) == strings.TrimSpace(asString(expected))
	}
}

func (s *Server) threadDiskMatchesQuery(threadID, query string) bool {
	for _, root := range []string{s.workspaceDir(threadID), s.uploadsDir(threadID), s.outputsDir(threadID)} {
		if threadPathMatchesQuery(root, query) {
			return true
		}
	}
	return false
}

func threadPathMatchesQuery(root, query string) bool {
	matched := false
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if strings.Contains(strings.ToLower(path), query) {
			matched = true
			return io.EOF
		}
		if info == nil || info.IsDir() || info.Size() > 1<<20 {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if strings.Contains(strings.ToLower(string(data)), query) {
			matched = true
			return io.EOF
		}
		return nil
	})
	return matched
}

func compareThreadMaps(left, right map[string]any, sortBy, sortOrder string) int {
	primary := compareThreadField(left, right, sortBy)
	if primary == 0 && sortBy != "created_at" {
		primary = compareThreadField(left, right, "created_at")
	}
	if primary == 0 && sortBy != "thread_id" {
		primary = compareThreadField(left, right, "thread_id")
	}
	if strings.EqualFold(sortOrder, "desc") {
		primary = -primary
	}
	return primary
}

func compareThreadField(left, right map[string]any, sortBy string) int {
	switch sortBy {
	case "created_at":
		return compareStrings(asString(left["created_at"]), asString(right["created_at"]))
	case "updated_at", "":
		return compareStrings(asString(left["updated_at"]), asString(right["updated_at"]))
	case "step":
		return compareFloats(numberFromAny(left["step"]), numberFromAny(right["step"]))
	case "title":
		return compareStrings(asString(left["title"]), asString(right["title"]))
	case "status":
		return compareStrings(asString(left["status"]), asString(right["status"]))
	case "assistant_id":
		return compareStrings(asString(mapFromAny(left["metadata"])["assistant_id"]), asString(mapFromAny(right["metadata"])["assistant_id"]))
	case "graph_id":
		return compareStrings(asString(mapFromAny(left["metadata"])["graph_id"]), asString(mapFromAny(right["metadata"])["graph_id"]))
	case "run_id":
		return compareStrings(asString(mapFromAny(left["metadata"])["run_id"]), asString(mapFromAny(right["metadata"])["run_id"]))
	case "agent_name":
		return compareStrings(asString(left["agent_name"]), asString(right["agent_name"]))
	case "agent_type":
		return compareStrings(asString(left["agent_type"]), asString(right["agent_type"]))
	case "model_name":
		return compareStrings(asString(left["model_name"]), asString(right["model_name"]))
	case "mode":
		return compareStrings(asString(left["mode"]), asString(right["mode"]))
	case "reasoning_effort":
		return compareStrings(asString(left["reasoning_effort"]), asString(right["reasoning_effort"]))
	case "thinking_enabled":
		return compareFloats(float64(boolRank(left["thinking_enabled"])), float64(boolRank(right["thinking_enabled"])))
	case "is_plan_mode":
		return compareFloats(float64(boolRank(left["is_plan_mode"])), float64(boolRank(right["is_plan_mode"])))
	case "subagent_enabled":
		return compareFloats(float64(boolRank(left["subagent_enabled"])), float64(boolRank(right["subagent_enabled"])))
	case "temperature":
		return compareFloats(numberFromAny(left["temperature"]), numberFromAny(right["temperature"]))
	case "max_tokens":
		return compareFloats(numberFromAny(left["max_tokens"]), numberFromAny(right["max_tokens"]))
	case "checkpoint_id":
		return compareStrings(asString(left["checkpoint_id"]), asString(right["checkpoint_id"]))
	case "parent_checkpoint_id":
		return compareStrings(asString(left["parent_checkpoint_id"]), asString(right["parent_checkpoint_id"]))
	case "checkpoint_ns":
		return compareStrings(asString(left["checkpoint_ns"]), asString(right["checkpoint_ns"]))
	case "parent_checkpoint_ns":
		return compareStrings(asString(left["parent_checkpoint_ns"]), asString(right["parent_checkpoint_ns"]))
	case "checkpoint_thread_id":
		return compareStrings(asString(left["checkpoint_thread_id"]), asString(right["checkpoint_thread_id"]))
	case "parent_checkpoint_thread_id":
		return compareStrings(asString(left["parent_checkpoint_thread_id"]), asString(right["parent_checkpoint_thread_id"]))
	case "thread_id":
		return compareStrings(asString(left["thread_id"]), asString(right["thread_id"]))
	default:
		return compareStrings(asString(left["updated_at"]), asString(right["updated_at"]))
	}
}

func compareStrings(left, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareFloats(left, right float64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
