package langgraphcompat

import "strings"

type threadMemoryScope struct {
	UserID    string
	GroupID   string
	Namespace string
}

func threadMemoryScopeFromMetadata(metadata map[string]any) threadMemoryScope {
	if len(metadata) == 0 {
		return threadMemoryScope{}
	}
	return threadMemoryScope{
		UserID:    strings.TrimSpace(stringValue(metadata["memory_user_id"])),
		GroupID:   strings.TrimSpace(stringValue(metadata["memory_group_id"])),
		Namespace: strings.TrimSpace(stringValue(metadata["memory_namespace"])),
	}
}

func threadMemoryScopeFromConfigurable(configurable map[string]any) threadMemoryScope {
	if len(configurable) == 0 {
		return threadMemoryScope{}
	}
	return threadMemoryScope{
		UserID:    strings.TrimSpace(stringValue(configurable["memory_user_id"])),
		GroupID:   strings.TrimSpace(stringValue(configurable["memory_group_id"])),
		Namespace: strings.TrimSpace(stringValue(configurable["memory_namespace"])),
	}
}

func threadMemoryScopeFromRuntimeContext(runtimeContext map[string]any, cfg runConfig) threadMemoryScope {
	return threadMemoryScope{
		UserID: strings.TrimSpace(firstNonEmpty(
			stringFromAny(runtimeContext["memory_user_id"]),
			stringFromAny(runtimeContext["memoryUserId"]),
			cfg.MemoryUserID,
		)),
		GroupID: strings.TrimSpace(firstNonEmpty(
			stringFromAny(runtimeContext["memory_group_id"]),
			stringFromAny(runtimeContext["memoryGroupId"]),
			cfg.MemoryGroupID,
		)),
		Namespace: strings.TrimSpace(firstNonEmpty(
			stringFromAny(runtimeContext["memory_namespace"]),
			stringFromAny(runtimeContext["memoryNamespace"]),
			cfg.MemoryNamespace,
		)),
	}
}

func threadMemoryScopeFromAny(raw any) threadMemoryScope {
	scope := mapFromAny(raw)
	if len(scope) == 0 {
		return threadMemoryScope{}
	}
	return threadMemoryScope{
		UserID: strings.TrimSpace(firstNonEmpty(
			stringValue(scope["user_id"]),
			stringValue(scope["userId"]),
		)),
		GroupID: strings.TrimSpace(firstNonEmpty(
			stringValue(scope["group_id"]),
			stringValue(scope["groupId"]),
		)),
		Namespace: strings.TrimSpace(firstNonEmpty(
			stringValue(scope["namespace"]),
		)),
	}
}

func threadMemoryScopeFromRaw(raw map[string]any) threadMemoryScope {
	if len(raw) == 0 {
		return threadMemoryScope{}
	}
	scope := threadMemoryScopeFromAny(firstNonNil(raw["memory_scope"], raw["memoryScope"]))
	values := mapFromAny(raw["values"])
	if len(values) > 0 {
		scope = scope.Merge(threadMemoryScopeFromAny(firstNonNil(values["memory_scope"], values["memoryScope"])))
	}
	return scope
}

func (s threadMemoryScope) IsZero() bool {
	return s.UserID == "" && s.GroupID == "" && s.Namespace == ""
}

func (s threadMemoryScope) Merge(other threadMemoryScope) threadMemoryScope {
	merged := s
	if merged.UserID == "" {
		merged.UserID = other.UserID
	}
	if merged.GroupID == "" {
		merged.GroupID = other.GroupID
	}
	if merged.Namespace == "" {
		merged.Namespace = other.Namespace
	}
	return merged
}

func (s threadMemoryScope) ApplyMetadata(metadata map[string]any) {
	if metadata == nil {
		return
	}
	if s.UserID != "" {
		metadata["memory_user_id"] = s.UserID
	}
	if s.GroupID != "" {
		metadata["memory_group_id"] = s.GroupID
	}
	if s.Namespace != "" {
		metadata["memory_namespace"] = s.Namespace
	}
}

func (s threadMemoryScope) ApplyConfigurable(configurable map[string]any) {
	if configurable == nil {
		return
	}
	if s.UserID != "" {
		configurable["memory_user_id"] = s.UserID
	}
	if s.GroupID != "" {
		configurable["memory_group_id"] = s.GroupID
	}
	if s.Namespace != "" {
		configurable["memory_namespace"] = s.Namespace
	}
}

func (s threadMemoryScope) Response() map[string]any {
	if s.IsZero() {
		return map[string]any{}
	}
	return map[string]any{
		"user_id":   s.UserID,
		"group_id":  s.GroupID,
		"namespace": s.Namespace,
	}
}
