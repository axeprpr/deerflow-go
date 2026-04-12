package langgraphcompat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

const maxThreadHistorySnapshots = 20

func derivePersistedThreadStatus(raw map[string]any, fallback string) string {
	if status := strings.TrimSpace(fallback); status != "" {
		return status
	}
	if items := anySlice(raw["interrupts"]); len(items) > 0 {
		return "interrupted"
	}
	if items := anySlice(raw["tasks"]); len(items) > 0 {
		return "busy"
	}
	if items := anySlice(raw["next"]); len(items) > 0 {
		return "busy"
	}
	return "idle"
}

func (s *Server) loadPersistedThreads() {
	root := filepath.Join(s.dataRoot, "threads")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "user-data", "thread.json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal(data, &wrapper); err == nil {
			if nested, ok := wrapper["thread"]; ok && len(nested) > 0 {
				data = nested
			} else if nested, ok := wrapper["data"]; ok && len(nested) > 0 {
				data = nested
			}
		}
		var persisted persistedThreadSession
		if err := json.Unmarshal(data, &persisted); err != nil {
			continue
		}
		var raw map[string]any
		_ = json.Unmarshal(data, &raw)
		if persisted.Metadata == nil {
			persisted.Metadata = mapFromAny(raw["metadata"])
		}
		values := normalizeThreadValues(mapFromAny(raw["values"]))
		if len(values) == 0 {
			values = normalizeThreadValues(raw)
		}
		if len(persisted.Messages) == 0 {
			if rawMessages, ok := values["messages"].([]any); ok {
				persisted.Messages = s.convertToMessages(entry.Name(), rawMessages)
			}
		}
		if persisted.Metadata == nil {
			persisted.Metadata = map[string]any{}
		}
		for metadataKey, valueKeys := range map[string][]string{
			"title":          {"title"},
			"todos":          {"todos"},
			"sandbox":        {"sandbox"},
			"viewed_images":  {"viewed_images", "viewedImages"},
			"artifacts":      {"artifacts"},
			"uploaded_files": {"uploaded_files", "uploadedFiles"},
			"thread_data":    {"thread_data", "threadData"},
		} {
			for _, valueKey := range valueKeys {
				if value, ok := values[valueKey]; ok {
					persisted.Metadata[metadataKey] = value
					break
				}
			}
		}
		config := normalizeThreadConfig(mapFromAny(raw["config"]))
		if len(config) == 0 {
			if configurable := mapFromAny(raw["configurable"]); len(configurable) > 0 {
				config = normalizeThreadConfig(map[string]any{"configurable": configurable})
			}
		}
		if len(config) == 0 {
			config = normalizeThreadConfig(raw)
		}
		if configurable := mapFromAny(config["configurable"]); len(configurable) > 0 {
			if persisted.Metadata == nil {
				persisted.Metadata = map[string]any{}
			}
			for _, key := range []string{"thread_id", "agent_type", "agent_name", "model_name", "mode", "reasoning_effort", "thinking_enabled", "is_plan_mode", "subagent_enabled", "temperature", "max_tokens"} {
				if value, ok := configurable[key]; ok {
					persisted.Metadata[key] = value
				}
			}
		}
		for key, aliases := range map[string][]string{
			"checkpoint_id":        {"checkpoint_id", "checkpointId"},
			"parent_checkpoint_id": {"parent_checkpoint_id", "parentCheckpointId"},
		} {
			for _, alias := range aliases {
				if value, ok := raw[alias]; ok {
					persisted.Metadata[key] = value
					break
				}
			}
		}
		for _, key := range []string{"interrupts", "tasks", "next"} {
			if value, ok := raw[key]; ok {
				persisted.Metadata[key] = value
			}
		}
		if checkpoint := normalizeCheckpointObject(mapFromAny(firstNonNil(raw["checkpoint"], raw["checkpointObject"]))); len(checkpoint) > 0 {
			if value, ok := checkpoint["checkpoint_id"]; ok {
				persisted.Metadata["checkpoint_id"] = value
			}
			if value, ok := checkpoint["checkpoint_ns"]; ok {
				persisted.Metadata["checkpoint_ns"] = value
			}
			if value, ok := checkpoint["thread_id"]; ok {
				persisted.Metadata["checkpoint_thread_id"] = value
			}
		}
		if checkpoint := normalizeCheckpointObject(mapFromAny(firstNonNil(raw["parent_checkpoint"], raw["parentCheckpoint"]))); len(checkpoint) > 0 {
			if value, ok := checkpoint["checkpoint_id"]; ok {
				persisted.Metadata["parent_checkpoint_id"] = value
			}
			if value, ok := checkpoint["checkpoint_ns"]; ok {
				persisted.Metadata["parent_checkpoint_ns"] = value
			}
			if value, ok := checkpoint["thread_id"]; ok {
				persisted.Metadata["parent_checkpoint_thread_id"] = value
			}
		}
		if persisted.ThreadID == "" {
			persisted.ThreadID = firstNonEmpty(stringValue(raw["threadId"]), stringValue(raw["thread_id"]))
		}
		if persisted.CreatedAt.IsZero() {
			persisted.CreatedAt = timeValue(firstNonNil(raw["createdAt"], raw["created_at"]))
		}
		if persisted.UpdatedAt.IsZero() {
			persisted.UpdatedAt = timeValue(firstNonNil(raw["updatedAt"], raw["updated_at"]))
		}
		if persisted.UpdatedAt.IsZero() {
			persisted.UpdatedAt = persisted.CreatedAt
		}
		if persisted.ThreadID == "" {
			persisted.ThreadID = entry.Name()
		}
		normalizedTopLevelMetadata := normalizePersistedThreadMetadata(mapFromAny(raw))
		persisted.Metadata = normalizePersistedThreadMetadata(persisted.Metadata)
		for _, key := range []string{
			"thread_id",
			"assistant_id",
			"graph_id",
			"run_id",
			"checkpoint_id",
			"parent_checkpoint_id",
			"checkpoint_ns",
			"parent_checkpoint_ns",
			"checkpoint_thread_id",
			"parent_checkpoint_thread_id",
			"step",
			"mode",
			"model_name",
			"reasoning_effort",
			"agent_name",
			"agent_type",
			"thinking_enabled",
			"is_plan_mode",
			"subagent_enabled",
			"temperature",
			"max_tokens",
		} {
			if _, ok := persisted.Metadata[key]; !ok {
				if value, ok := normalizedTopLevelMetadata[key]; ok {
					persisted.Metadata[key] = value
				}
			}
		}
		s.sessions[persisted.ThreadID] = &Session{
			ThreadID:     persisted.ThreadID,
			Messages:     persisted.Messages,
			Metadata:     persisted.Metadata,
			Status:       derivePersistedThreadStatus(raw, persisted.Status),
			PresentFiles: tools.NewPresentFileRegistry(),
			CreatedAt:    persisted.CreatedAt,
			UpdatedAt:    persisted.UpdatedAt,
		}
	}
}

func normalizePersistedThreadMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return metadata
	}
	if _, ok := metadata["viewed_images"]; !ok {
		if value, ok := metadata["viewedImages"]; ok {
			metadata["viewed_images"] = value
		}
	}
	if _, ok := metadata["model_name"]; !ok {
		if value, ok := metadata["modelName"]; ok {
			metadata["model_name"] = value
		} else if value, ok := metadata["model"]; ok {
			metadata["model_name"] = value
		}
	}
	if _, ok := metadata["thread_id"]; !ok {
		if value, ok := metadata["threadId"]; ok {
			metadata["thread_id"] = value
		}
	}
	if _, ok := metadata["assistant_id"]; !ok {
		if value, ok := metadata["assistantId"]; ok {
			metadata["assistant_id"] = value
		}
	}
	if _, ok := metadata["graph_id"]; !ok {
		if value, ok := metadata["graphId"]; ok {
			metadata["graph_id"] = value
		}
	}
	if _, ok := metadata["run_id"]; !ok {
		if value, ok := metadata["runId"]; ok {
			metadata["run_id"] = value
		}
	}
	if _, ok := metadata["checkpoint_id"]; !ok {
		if value, ok := metadata["checkpointId"]; ok {
			metadata["checkpoint_id"] = value
		}
	}
	if _, ok := metadata["parent_checkpoint_id"]; !ok {
		if value, ok := metadata["parentCheckpointId"]; ok {
			metadata["parent_checkpoint_id"] = value
		}
	}
	if _, ok := metadata["checkpoint_ns"]; !ok {
		if value, ok := metadata["checkpointNs"]; ok {
			metadata["checkpoint_ns"] = value
		}
	}
	if _, ok := metadata["parent_checkpoint_ns"]; !ok {
		if value, ok := metadata["parentCheckpointNs"]; ok {
			metadata["parent_checkpoint_ns"] = value
		}
	}
	if _, ok := metadata["checkpoint_thread_id"]; !ok {
		if value, ok := metadata["checkpointThreadId"]; ok {
			metadata["checkpoint_thread_id"] = value
		}
	}
	if _, ok := metadata["parent_checkpoint_thread_id"]; !ok {
		if value, ok := metadata["parentCheckpointThreadId"]; ok {
			metadata["parent_checkpoint_thread_id"] = value
		}
	}
	if _, ok := metadata["agent_type"]; !ok {
		if value, ok := metadata["agentType"]; ok {
			metadata["agent_type"] = value
		}
	}
	if _, ok := metadata["reasoning_effort"]; !ok {
		if value, ok := metadata["reasoningEffort"]; ok {
			metadata["reasoning_effort"] = value
		}
	}
	if _, ok := metadata["agent_name"]; !ok {
		if value, ok := metadata["agentName"]; ok {
			metadata["agent_name"] = value
		}
	}
	if _, ok := metadata["thinking_enabled"]; !ok {
		if value, ok := metadata["thinkingEnabled"]; ok {
			metadata["thinking_enabled"] = value
		}
	}
	if _, ok := metadata["is_plan_mode"]; !ok {
		if value, ok := metadata["isPlanMode"]; ok {
			metadata["is_plan_mode"] = value
		}
	}
	if _, ok := metadata["subagent_enabled"]; !ok {
		if value, ok := metadata["subagentEnabled"]; ok {
			metadata["subagent_enabled"] = value
		}
	}
	if _, ok := metadata["temperature"]; !ok {
		if value, ok := metadata["Temperature"]; ok {
			metadata["temperature"] = value
		}
	}
	if _, ok := metadata["max_tokens"]; !ok {
		if value, ok := metadata["maxTokens"]; ok {
			metadata["max_tokens"] = value
		}
	}
	if checkpoint := normalizeCheckpointObject(mapFromAny(firstNonNil(metadata["checkpoint"], metadata["checkpointObject"]))); len(checkpoint) > 0 {
		if value, ok := checkpoint["checkpoint_id"]; ok {
			metadata["checkpoint_id"] = value
		}
		if value, ok := checkpoint["checkpoint_ns"]; ok {
			metadata["checkpoint_ns"] = value
		}
		if value, ok := checkpoint["thread_id"]; ok {
			metadata["checkpoint_thread_id"] = value
		}
	}
	if checkpoint := normalizeCheckpointObject(mapFromAny(firstNonNil(metadata["parent_checkpoint"], metadata["parentCheckpoint"]))); len(checkpoint) > 0 {
		if value, ok := checkpoint["checkpoint_id"]; ok {
			metadata["parent_checkpoint_id"] = value
		}
		if value, ok := checkpoint["checkpoint_ns"]; ok {
			metadata["parent_checkpoint_ns"] = value
		}
		if value, ok := checkpoint["thread_id"]; ok {
			metadata["parent_checkpoint_thread_id"] = value
		}
	}
	return metadata
}

func normalizePersistedRunEvents(events []harnessruntime.RunEvent, rawItems []any) []harnessruntime.RunEvent {
	if len(events) == 0 || len(events) != len(rawItems) {
		return events
	}
	for i, rawItem := range rawItems {
		raw := mapFromAny(rawItem)
		if raw == nil {
			continue
		}
		if events[i].ID == "" {
			events[i].ID = stringValue(firstNonNil(raw["id"], raw["ID"]))
		}
		if events[i].Event == "" {
			events[i].Event = stringValue(firstNonNil(raw["event"], raw["Event"]))
		}
		if events[i].Data == nil {
			events[i].Data = firstNonNil(raw["data"], raw["Data"])
		}
		if events[i].RunID == "" {
			events[i].RunID = stringValue(firstNonNil(raw["runId"], raw["run_id"], raw["RunID"]))
		}
		if events[i].ThreadID == "" {
			events[i].ThreadID = stringValue(firstNonNil(raw["threadId"], raw["thread_id"], raw["ThreadID"]))
		}
	}
	return events
}

func (s *Server) loadPersistedRuns() {
	root := filepath.Join(s.dataRoot, "runs")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal(data, &wrapper); err == nil {
			if nested, ok := wrapper["run"]; ok && len(nested) > 0 {
				data = nested
			} else if nested, ok := wrapper["data"]; ok && len(nested) > 0 {
				data = nested
			}
		}
		var persisted persistedThreadRun
		if err := json.Unmarshal(data, &persisted); err != nil {
			continue
		}
		var raw map[string]any
		_ = json.Unmarshal(data, &raw)
		if persisted.RunID == "" {
			persisted.RunID = stringValue(raw["runId"])
		}
		if persisted.ThreadID == "" {
			persisted.ThreadID = stringValue(raw["threadId"])
		}
		if persisted.AssistantID == "" {
			persisted.AssistantID = stringValue(raw["assistantId"])
		}
		if persisted.Attempt == 0 {
			persisted.Attempt = intValue(firstNonNil(raw["attempt"], raw["Attempt"]))
			if persisted.Attempt == 0 {
				persisted.Attempt = 1
			}
		}
		if persisted.ResumeFromEvent == 0 {
			persisted.ResumeFromEvent = intValue(firstNonNil(raw["resumeFromEvent"], raw["resume_from_event"], raw["ResumeFromEvent"]))
		}
		if persisted.ResumeReason == "" {
			persisted.ResumeReason = stringValue(firstNonNil(raw["resumeReason"], raw["resume_reason"], raw["ResumeReason"]))
		}
		if persisted.CreatedAt.IsZero() {
			persisted.CreatedAt = timeValue(firstNonNil(raw["createdAt"], raw["created_at"]))
		}
		if persisted.UpdatedAt.IsZero() {
			persisted.UpdatedAt = timeValue(firstNonNil(raw["updatedAt"], raw["updated_at"]))
		}
		if rawEvents, ok := raw["events"].([]any); ok {
			persisted.Events = normalizePersistedRunEvents(persisted.Events, rawEvents)
		}
		if persisted.RunID == "" {
			continue
		}
		snapshot := harnessruntime.RunSnapshot{
			Record: harnessruntime.RunRecord{
				RunID:           persisted.RunID,
				ThreadID:        persisted.ThreadID,
				AssistantID:     persisted.AssistantID,
				Attempt:         persisted.Attempt,
				ResumeFromEvent: persisted.ResumeFromEvent,
				ResumeReason:    persisted.ResumeReason,
				Status:          persisted.Status,
				CreatedAt:       persisted.CreatedAt,
				UpdatedAt:       persisted.UpdatedAt,
				Error:           persisted.Error,
			},
			Events: persisted.Events,
		}
		if store, ok := s.ensureSnapshotStore().(*compatRunStateStore); ok && store.memory != nil {
			store.memory.SaveRunSnapshot(snapshot)
		}
		s.saveRunSnapshotState(snapshot, false)
	}
}

func timeValue(value any) time.Time {
	switch typed := value.(type) {
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(typed)); err == nil {
			return parsed
		}
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func (s *Server) appendThreadHistorySnapshot(threadID string) error {
	state := s.getThreadState(threadID)
	if state == nil {
		return nil
	}
	s.sessionsMu.RLock()
	session := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if session != nil {
		state.CreatedAt = firstNonZeroTime(session.UpdatedAt, session.CreatedAt).Format(time.RFC3339Nano)
	}
	history := s.loadThreadHistory(threadID)
	if len(history) > 0 && history[0].CreatedAt == state.CreatedAt {
		return nil
	}
	history = append([]ThreadState{*state}, history...)
	if len(history) > maxThreadHistorySnapshots {
		history = history[:maxThreadHistorySnapshots]
	}
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.threadHistoryPath(threadID), data, 0o644)
}

func (s *Server) loadThreadHistory(threadID string) []ThreadState {
	data, err := os.ReadFile(s.threadHistoryPath(threadID))
	if err != nil {
		return nil
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil {
		if nested, ok := wrapper["history"]; ok && len(nested) > 0 {
			data = nested
		} else if nested, ok := wrapper["items"]; ok && len(nested) > 0 {
			data = nested
		} else if nested, ok := wrapper["data"]; ok && len(nested) > 0 {
			data = nested
		}
	}
	var history []ThreadState
	if err := json.Unmarshal(data, &history); err == nil {
		var rawItems []map[string]any
		if err := json.Unmarshal(data, &rawItems); err == nil {
			return normalizeLoadedThreadHistory(history, rawItems)
		}
		return history
	}
	var rawItems []map[string]any
	if err := json.Unmarshal(data, &rawItems); err == nil {
		history = make([]ThreadState, len(rawItems))
		return normalizeLoadedThreadHistory(history, rawItems)
	}
	return nil
}

func normalizeCheckpointObject(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	out := map[string]any{}
	if value := firstNonEmpty(stringValue(raw["checkpoint_id"]), stringValue(raw["checkpointId"])); value != "" {
		out["checkpoint_id"] = value
	}
	if value := firstNonEmpty(stringValue(raw["checkpoint_ns"]), stringValue(raw["checkpointNs"])); value != "" {
		out["checkpoint_ns"] = value
	}
	if value := firstNonEmpty(stringValue(raw["thread_id"]), stringValue(raw["threadId"])); value != "" {
		out["thread_id"] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeThreadValues(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		out[key] = value
	}
	for canonicalKey, aliases := range map[string][]string{
		"title":          {"title"},
		"todos":          {"todos"},
		"sandbox":        {"sandbox"},
		"viewed_images":  {"viewed_images", "viewedImages"},
		"artifacts":      {"artifacts"},
		"uploaded_files": {"uploaded_files", "uploadedFiles"},
		"thread_data":    {"thread_data", "threadData"},
		"messages":       {"messages"},
	} {
		for _, alias := range aliases {
			if value, ok := raw[alias]; ok {
				out[canonicalKey] = value
				break
			}
		}
	}
	return out
}

func normalizeThreadConfig(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		out[key] = value
	}
	configurable := mapFromAny(out["configurable"])
	if len(configurable) == 0 {
		configurable = raw
	}
	if len(configurable) == 0 {
		return out
	}
	normalized := make(map[string]any, len(configurable))
	for key, value := range configurable {
		normalized[key] = value
	}
	for canonicalKey, aliases := range map[string][]string{
		"thread_id":        {"thread_id", "threadId"},
		"agent_type":       {"agent_type", "agentType"},
		"agent_name":       {"agent_name", "agentName"},
		"model_name":       {"model_name", "modelName", "model"},
		"reasoning_effort": {"reasoning_effort", "reasoningEffort"},
		"thinking_enabled": {"thinking_enabled", "thinkingEnabled"},
		"is_plan_mode":     {"is_plan_mode", "isPlanMode"},
		"subagent_enabled": {"subagent_enabled", "subagentEnabled"},
		"temperature":      {"temperature", "Temperature"},
		"max_tokens":       {"max_tokens", "maxTokens"},
	} {
		for _, alias := range aliases {
			if value, ok := configurable[alias]; ok {
				normalized[canonicalKey] = value
				break
			}
		}
	}
	out["configurable"] = normalized
	return out
}

func normalizeLoadedThreadHistory(history []ThreadState, rawItems []map[string]any) []ThreadState {
	if len(rawItems) == 0 {
		return history
	}
	if len(history) != len(rawItems) {
		history = make([]ThreadState, len(rawItems))
	}
	for i := range rawItems {
		if history[i].CheckpointID == "" {
			history[i].CheckpointID = firstNonEmpty(stringValue(rawItems[i]["checkpointId"]), stringValue(rawItems[i]["checkpoint_id"]))
		}
		if history[i].ParentCheckpointID == "" {
			history[i].ParentCheckpointID = firstNonEmpty(stringValue(rawItems[i]["parentCheckpointId"]), stringValue(rawItems[i]["parent_checkpoint_id"]))
		}
		if history[i].CreatedAt == "" {
			history[i].CreatedAt = firstNonEmpty(stringValue(rawItems[i]["createdAt"]), stringValue(rawItems[i]["created_at"]))
		}
		if len(history[i].Next) == 0 {
			history[i].Next = stringSliceFromAny(rawItems[i]["next"])
		}
		if len(history[i].Tasks) == 0 {
			history[i].Tasks = anySlice(rawItems[i]["tasks"])
		}
		if len(history[i].Interrupts) == 0 {
			history[i].Interrupts = anySlice(rawItems[i]["interrupts"])
		}
		if len(history[i].Values) == 0 {
			history[i].Values = normalizeThreadValues(mapFromAny(rawItems[i]["values"]))
			if len(history[i].Values) == 0 {
				history[i].Values = normalizeThreadValues(rawItems[i])
			}
		} else {
			history[i].Values = normalizeThreadValues(history[i].Values)
		}
		if len(history[i].Config) == 0 {
			history[i].Config = mapFromAny(rawItems[i]["config"])
		}
		if len(history[i].Config) == 0 {
			if configurable := mapFromAny(rawItems[i]["configurable"]); len(configurable) > 0 {
				history[i].Config = map[string]any{"configurable": configurable}
			}
		}
		if len(history[i].Config) == 0 {
			history[i].Config = normalizeThreadConfig(rawItems[i])
		}
		history[i].Config = normalizeThreadConfig(history[i].Config)
		if len(history[i].Metadata) == 0 {
			history[i].Metadata = mapFromAny(rawItems[i]["metadata"])
		}
		if history[i].Metadata == nil {
			history[i].Metadata = map[string]any{}
		}
		history[i].Metadata = normalizePersistedThreadMetadata(history[i].Metadata)
		normalizedTopLevelMetadata := normalizePersistedThreadMetadata(mapFromAny(rawItems[i]))
		for _, key := range []string{
			"thread_id",
			"assistant_id",
			"graph_id",
			"run_id",
			"checkpoint_id",
			"parent_checkpoint_id",
			"checkpoint_ns",
			"parent_checkpoint_ns",
			"checkpoint_thread_id",
			"parent_checkpoint_thread_id",
			"step",
			"mode",
			"model_name",
			"reasoning_effort",
			"agent_name",
			"agent_type",
			"thinking_enabled",
			"is_plan_mode",
			"subagent_enabled",
			"temperature",
			"max_tokens",
		} {
			if _, ok := history[i].Metadata[key]; !ok {
				if value, ok := normalizedTopLevelMetadata[key]; ok {
					history[i].Metadata[key] = value
				}
			}
		}
		if len(history[i].Checkpoint) == 0 {
			history[i].Checkpoint = checkpointObjectFromMetadata(history[i].Metadata, "")
		}
		if len(history[i].ParentCheckpoint) == 0 {
			history[i].ParentCheckpoint = checkpointObjectFromMetadata(history[i].Metadata, "parent_")
		}
		if checkpoint := mapFromAny(firstNonNil(rawItems[i]["checkpoint"], rawItems[i]["checkpointObject"])); len(checkpoint) > 0 {
			history[i].Checkpoint = normalizeCheckpointObject(checkpoint)
			if history[i].CheckpointID == "" {
				history[i].CheckpointID = stringValue(history[i].Checkpoint["checkpoint_id"])
			}
		}
		if checkpoint := mapFromAny(firstNonNil(rawItems[i]["parent_checkpoint"], rawItems[i]["parentCheckpoint"])); len(checkpoint) > 0 {
			history[i].ParentCheckpoint = normalizeCheckpointObject(checkpoint)
			if history[i].ParentCheckpointID == "" {
				history[i].ParentCheckpointID = stringValue(history[i].ParentCheckpoint["checkpoint_id"])
			}
		}
	}
	return history
}
