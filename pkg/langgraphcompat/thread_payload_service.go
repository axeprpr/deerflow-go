package langgraphcompat

import "strings"

func applyThreadStatus(session *Session, raw map[string]any) {
	if session == nil || len(raw) == 0 {
		return
	}
	status := strings.TrimSpace(stringFromAny(raw["status"]))
	if status == "" {
		return
	}
	session.Status = status
}

func applyThreadConfigurable(session *Session, raw map[string]any) {
	if session == nil || len(raw) == 0 {
		return
	}
	config := normalizeThreadConfig(mapFromAny(raw["config"]))
	if len(config) == 0 {
		if configurable := mapFromAny(raw["configurable"]); len(configurable) > 0 {
			config = normalizeThreadConfig(map[string]any{"configurable": configurable})
		}
	}
	configurable := mapFromAny(config["configurable"])
	if len(configurable) == 0 {
		return
	}
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	if session.Configurable == nil {
		session.Configurable = defaultThreadConfig(session.ThreadID)
	}
	scope := threadMemoryScopeFromConfigurable(configurable)
	for _, key := range []string{
		"thread_id",
		"agent_type",
		"agent_name",
		"model_name",
		"mode",
		"reasoning_effort",
		"thinking_enabled",
		"is_plan_mode",
		"subagent_enabled",
		"temperature",
		"max_tokens",
		"memory_user_id",
		"memory_group_id",
		"memory_namespace",
	} {
		if value, ok := configurable[key]; ok {
			session.Metadata[key] = value
			session.Configurable[key] = value
		}
	}
	scope.ApplyMetadata(session.Metadata)
	scope.ApplyConfigurable(session.Configurable)
	if _, ok := session.Configurable["thread_id"]; !ok {
		session.Configurable["thread_id"] = session.ThreadID
	}
}

func normalizeUploadedFilesValue(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	case []any:
		return uploadedFilesFromMetadata(typed)
	default:
		return nil
	}
}

func (s *Server) applyThreadValues(session *Session, values map[string]any) {
	if session == nil || len(values) == 0 {
		return
	}
	if session.Values == nil {
		session.Values = map[string]any{}
	}
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	if hasAnyKey(values, "messages") {
		if rawMessages, ok := values["messages"].([]any); ok {
			session.Messages = s.convertToMessages(session.ThreadID, rawMessages)
		} else {
			session.Messages = nil
		}
	}
	if hasAnyKey(values, "title") {
		if title, ok := values["title"].(string); ok {
			title = strings.TrimSpace(title)
			session.Values["title"] = title
			if title == "" {
				delete(session.Metadata, "title")
			} else {
				session.Metadata["title"] = title
			}
		} else if values["title"] == nil {
			session.Values["title"] = ""
			delete(session.Metadata, "title")
		}
	}
	if hasAnyKey(values, "todos") {
		if todos, ok := normalizeTodos(values["todos"]); ok {
			session.Values["todos"] = todos
			if len(todos) > 0 {
				session.Metadata["todos"] = todos
			} else {
				delete(session.Metadata, "todos")
			}
		} else {
			session.Values["todos"] = []map[string]any{}
			delete(session.Metadata, "todos")
		}
	}
	if hasAnyKey(values, "sandbox") {
		if sandboxState, ok := normalizeStringMap(values["sandbox"]); ok {
			session.Values["sandbox"] = sandboxState
			if len(sandboxState) > 0 {
				session.Metadata["sandbox"] = sandboxState
			} else {
				delete(session.Metadata, "sandbox")
			}
		} else {
			session.Values["sandbox"] = map[string]any{}
			delete(session.Metadata, "sandbox")
		}
	}
	if hasAnyKey(values, "artifacts") {
		artifacts := anyStringSlice(values["artifacts"])
		session.Values["artifacts"] = artifacts
		if len(artifacts) > 0 {
			session.Metadata["artifacts"] = artifacts
		} else {
			delete(session.Metadata, "artifacts")
		}
	}
	if hasAnyKey(values, "viewed_images", "viewedImages") {
		if viewedImages, ok := normalizeViewedImages(firstNonNil(values["viewed_images"], values["viewedImages"])); ok {
			session.Values["viewed_images"] = viewedImages
			if len(viewedImages) > 0 {
				session.Metadata["viewed_images"] = viewedImages
			} else {
				delete(session.Metadata, "viewed_images")
			}
		} else {
			session.Values["viewed_images"] = map[string]any{}
			delete(session.Metadata, "viewed_images")
		}
	}
	if hasAnyKey(values, "uploaded_files", "uploadedFiles") {
		if uploadedFiles := normalizeUploadedFilesValue(firstNonNil(values["uploaded_files"], values["uploadedFiles"])); uploadedFiles != nil {
			session.Values["uploaded_files"] = uploadedFiles
			if len(uploadedFiles) > 0 {
				items := make([]any, 0, len(uploadedFiles))
				for _, file := range uploadedFiles {
					items = append(items, file)
				}
				session.Metadata["uploaded_files"] = items
			} else {
				delete(session.Metadata, "uploaded_files")
			}
		} else {
			session.Values["uploaded_files"] = []map[string]any{}
			delete(session.Metadata, "uploaded_files")
		}
	}
	if hasAnyKey(values, "thread_data", "threadData") {
		if threadData, ok := normalizeStringMap(firstNonNil(values["thread_data"], values["threadData"])); ok && len(threadData) > 0 {
			session.Metadata["thread_data"] = threadData
		} else {
			delete(session.Metadata, "thread_data")
		}
	}
	for key, value := range values {
		switch key {
		case "messages", "title", "todos", "sandbox", "artifacts", "viewed_images", "viewedImages", "uploaded_files", "uploadedFiles", "thread_data", "threadData":
			continue
		default:
			session.Values[key] = value
		}
	}
}

func hasAnyKey(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := values[key]; ok {
			return true
		}
	}
	return false
}

func applyThreadMetadata(session *Session, metadata map[string]any) {
	if session == nil || len(metadata) == 0 {
		return
	}
	normalized := make(map[string]any, len(metadata))
	for k, v := range metadata {
		normalized[k] = v
	}
	normalized = normalizePersistedThreadMetadata(normalized)
	for k, v := range normalized {
		session.Metadata[k] = v
	}
}

func extractThreadValues(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	values := map[string]any{}
	for canonicalKey, aliases := range map[string][]string{
		"messages":       {"messages"},
		"title":          {"title"},
		"todos":          {"todos"},
		"sandbox":        {"sandbox"},
		"artifacts":      {"artifacts"},
		"viewed_images":  {"viewed_images", "viewedImages"},
		"uploaded_files": {"uploaded_files", "uploadedFiles"},
		"thread_data":    {"thread_data", "threadData"},
	} {
		for _, alias := range aliases {
			if value, ok := raw[alias]; ok {
				values[canonicalKey] = value
				break
			}
		}
	}
	if nested, ok := raw["values"].(map[string]any); ok {
		for key, value := range nested {
			values[key] = value
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func uploadedFilesFromMetadata(raw any) []map[string]any {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		file, ok := item.(map[string]any)
		if !ok {
			continue
		}
		filename := strings.TrimSpace(stringFromAny(file["filename"]))
		if filename == "" {
			continue
		}
		normalized := map[string]any{
			"filename": filename,
		}
		if path := strings.TrimSpace(stringFromAny(firstNonNil(file["path"], file["virtual_path"]))); path != "" {
			normalized["path"] = path
		}
		if size := toInt64(file["size"]); size > 0 {
			normalized["size"] = size
		}
		if status := strings.TrimSpace(stringFromAny(file["status"])); status != "" {
			normalized["status"] = status
		}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func todosFromMetadata(raw any) []map[string]any {
	if todos, ok := normalizeTodos(raw); ok {
		return todos
	}
	return []map[string]any{}
}

func normalizeTodos(raw any) ([]map[string]any, bool) {
	var items []any
	switch typed := raw.(type) {
	case []any:
		items = typed
	case []map[string]any:
		items = make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
	default:
		return nil, false
	}
	todos := make([]map[string]any, 0, len(items))
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		todo := map[string]any{}
		if content := strings.TrimSpace(stringValue(itemMap["content"])); content != "" {
			todo["content"] = content
		}
		status := strings.TrimSpace(stringValue(itemMap["status"]))
		switch status {
		case "pending", "in_progress", "completed":
			todo["status"] = status
		case "":
		default:
			todo["status"] = "pending"
		}
		if len(todo) > 0 {
			todos = append(todos, todo)
		}
	}
	return todos, true
}

func mapFromMetadata(raw any) map[string]any {
	if values, ok := normalizeStringMap(raw); ok {
		return values
	}
	return map[string]any{}
}

func normalizeStringMap(raw any) (map[string]any, bool) {
	items, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	values := make(map[string]any, len(items))
	for key, value := range items {
		text := strings.TrimSpace(stringValue(value))
		if text == "" {
			continue
		}
		values[key] = text
	}
	return values, true
}

func viewedImagesFromMetadata(raw any) map[string]any {
	if values, ok := normalizeViewedImages(raw); ok {
		return values
	}
	return map[string]any{}
}

func normalizeViewedImages(raw any) (map[string]any, bool) {
	items, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	values := make(map[string]any, len(items))
	for path, item := range items {
		image, ok := item.(map[string]any)
		if !ok {
			continue
		}
		normalized := make(map[string]any, 2)
		if base64 := strings.TrimSpace(stringValue(image["base64"])); base64 != "" {
			normalized["base64"] = base64
		}
		if mimeType := strings.TrimSpace(stringValue(image["mime_type"])); mimeType != "" {
			normalized["mime_type"] = mimeType
		}
		values[path] = normalized
	}
	return values, true
}
