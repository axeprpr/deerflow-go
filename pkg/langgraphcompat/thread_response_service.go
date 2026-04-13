package langgraphcompat

import (
	"sort"
	"time"
)

type threadSearchRequest struct {
	Query      string
	Status     string
	Limit      int
	Offset     int
	SortBy     string
	SortOrder  string
	Select     []string
	Metadata   map[string]any
	Values     map[string]any
}

func (s *Server) findThreadResponse(threadID string) (map[string]any, bool) {
	s.sessionsMu.RLock()
	session, exists := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if !exists {
		return nil, false
	}
	return s.threadResponse(session), true
}

func (s *Server) searchThreadResponses(req threadSearchRequest) []map[string]any {
	s.sessionsMu.RLock()
	threads := make([]map[string]any, 0, len(s.sessions))
	for _, session := range s.sessions {
		thread := s.threadResponse(session)
		if !s.threadMatchesSearch(session, thread, req.Query, req.Status, req.Metadata, req.Values) {
			continue
		}
		threads = append(threads, thread)
	}
	s.sessionsMu.RUnlock()

	sortThreadResponses(threads, req.SortBy, req.SortOrder)

	start := req.Offset
	if start > len(threads) {
		start = len(threads)
	}
	end := start + req.Limit
	if end > len(threads) {
		end = len(threads)
	}

	selected := make([]map[string]any, 0, end-start)
	for _, thread := range threads[start:end] {
		selected = append(selected, selectThreadFields(thread, req.Select))
	}
	return selected
}

func sortThreadResponses(threads []map[string]any, sortBy, sortOrder string) {
	sort.Slice(threads, func(i, j int) bool {
		return compareThreadMaps(threads[i], threads[j], sortBy, sortOrder) < 0
	})
}

func (s *Server) threadResponse(session *Session) map[string]any {
	values := s.threadValues(session)
	checkpoint := checkpointObjectFromMetadata(session.Metadata, "")
	parentCheckpoint := checkpointObjectFromMetadata(session.Metadata, "parent_")
	configurable := s.threadConfigurable(session)
	scope := threadMemoryScopeFromMetadata(session.Metadata).Merge(threadMemoryScopeFromConfigurable(configurable))
	next := stringSliceFromAny(session.Metadata["next"])
	tasks := anySlice(session.Metadata["tasks"])
	interrupts := anySlice(session.Metadata["interrupts"])
	return map[string]any{
		"thread_id":                   session.ThreadID,
		"created_at":                  session.CreatedAt.Format(time.RFC3339Nano),
		"updated_at":                  session.UpdatedAt.Format(time.RFC3339Nano),
		"assistant_id":                stringValue(session.Metadata["assistant_id"]),
		"graph_id":                    stringValue(session.Metadata["graph_id"]),
		"run_id":                      stringValue(session.Metadata["run_id"]),
		"agent_name":                  stringValue(configurable["agent_name"]),
		"agent_type":                  stringValue(configurable["agent_type"]),
		"title":                       stringValue(values["title"]),
		"model_name":                  stringValue(configurable["model_name"]),
		"mode":                        stringValue(configurable["mode"]),
		"reasoning_effort":            stringValue(configurable["reasoning_effort"]),
		"thinking_enabled":            configurable["thinking_enabled"],
		"is_plan_mode":                configurable["is_plan_mode"],
		"subagent_enabled":            configurable["subagent_enabled"],
		"temperature":                 configurable["temperature"],
		"max_tokens":                  configurable["max_tokens"],
		"checkpoint_id":               firstNonEmpty(stringValue(session.Metadata["checkpoint_id"]), session.CheckpointID),
		"parent_checkpoint_id":        stringValue(session.Metadata["parent_checkpoint_id"]),
		"checkpoint_ns":               stringValue(session.Metadata["checkpoint_ns"]),
		"parent_checkpoint_ns":        stringValue(session.Metadata["parent_checkpoint_ns"]),
		"checkpoint_thread_id":        stringValue(session.Metadata["checkpoint_thread_id"]),
		"parent_checkpoint_thread_id": stringValue(session.Metadata["parent_checkpoint_thread_id"]),
		"step":                        session.Metadata["step"],
		"artifacts":                   values["artifacts"],
		"todos":                       values["todos"],
		"sandbox":                     values["sandbox"],
		"thread_data":                 values["thread_data"],
		"uploaded_files":              values["uploaded_files"],
		"viewed_images":               values["viewed_images"],
		"memory_scope":                scope.Response(),
		"next":                        append([]string(nil), next...),
		"tasks":                       append([]any(nil), tasks...),
		"interrupts":                  append([]any(nil), interrupts...),
		"checkpoint":                  checkpoint,
		"parent_checkpoint":           parentCheckpoint,
		"metadata":                    threadMetadata(session),
		"status":                      session.Status,
		"config": map[string]any{
			"configurable": configurable,
		},
		"values": values,
	}
}
