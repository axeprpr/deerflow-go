package langgraphcompat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
)

const gatewayMemorySessionID = "gateway:global"

type memoryFactCreateRequest struct {
	Content    string  `json:"content"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
}

type memoryFactPatchRequest struct {
	Content    *string  `json:"content"`
	Category   *string  `json:"category"`
	Confidence *float64 `json:"confidence"`
}

func isGatewayGlobalMemoryScope(scope pkgmemory.Scope) bool {
	return strings.TrimSpace(string(scope.Type)) == "" &&
		strings.TrimSpace(scope.ID) == "" &&
		strings.TrimSpace(scope.Namespace) == ""
}

func gatewayMemoryScopeFromRequest(s *Server, r *http.Request) (pkgmemory.Scope, error) {
	if r == nil || r.URL == nil {
		return pkgmemory.Scope{}, nil
	}
	query := r.URL.Query()
	scopeType := strings.TrimSpace(firstNonEmpty(query.Get("scope")))
	scopeID := strings.TrimSpace(firstNonEmpty(query.Get("scope_id"), query.Get("scopeId")))
	namespace := strings.TrimSpace(firstNonEmpty(query.Get("namespace")))
	threadID := strings.TrimSpace(firstNonEmpty(query.Get("thread_id"), query.Get("threadId")))
	threadScope, threadFound := s.resolveThreadMemoryScope(threadID)
	if scopeType == "" && scopeID == "" && namespace == "" {
		if threadID == "" {
			return pkgmemory.Scope{}, nil
		}
		if !threadFound {
			return pkgmemory.Scope{}, fmt.Errorf("memory thread_id not found")
		}
		return defaultGatewayMemoryScopeFromThread(threadID, threadScope), nil
	}
	if threadID != "" && !threadFound {
		return pkgmemory.Scope{}, fmt.Errorf("memory thread_id not found")
	}
	if namespace == "" {
		namespace = threadScope.Namespace
	}
	if scopeType == "" && scopeID == "" && namespace != "" {
		if threadID == "" {
			return pkgmemory.Scope{}, nil
		}
		if !threadFound {
			return pkgmemory.Scope{}, fmt.Errorf("memory thread_id not found")
		}
		return defaultGatewayMemoryScopeFromThread(threadID, threadScope), nil
	}
	if strings.EqualFold(scopeType, "global") {
		if scopeID != "" || namespace != "" || threadID != "" {
			return pkgmemory.Scope{}, fmt.Errorf("global memory scope cannot use scope_id, namespace, or thread_id")
		}
		return pkgmemory.Scope{}, nil
	}
	if scopeType == "" {
		scopeType = string(defaultMemoryScopeTypeFromThread(threadScope))
	}
	if scopeID == "" {
		switch pkgmemory.ScopeType(scopeType) {
		case pkgmemory.ScopeSession:
			scopeID = threadID
		case pkgmemory.ScopeUser:
			scopeID = threadScope.UserID
		case pkgmemory.ScopeGroup:
			scopeID = threadScope.GroupID
		}
	}
	if scopeID == "" {
		return pkgmemory.Scope{}, fmt.Errorf("memory scope_id required")
	}
	scope := pkgmemory.Scope{
		Type:      pkgmemory.ScopeType(scopeType),
		ID:        scopeID,
		Namespace: namespace,
	}.Normalized()
	switch scope.Type {
	case pkgmemory.ScopeSession, pkgmemory.ScopeUser, pkgmemory.ScopeGroup, pkgmemory.ScopeAgent:
		return scope, nil
	default:
		return pkgmemory.Scope{}, fmt.Errorf("unsupported memory scope %q", scopeType)
	}
}

func (s *Server) resolveThreadMemoryScope(threadID string) (threadMemoryScope, bool) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return threadMemoryScope{}, false
	}
	scope := threadMemoryScope{}
	found := false
	if store := s.ensureThreadStateStore(); store != nil {
		if state, ok := store.LoadThreadRuntimeState(threadID); ok {
			scope = scope.Merge(threadMemoryScopeFromMetadata(state.Metadata))
			found = true
		}
	}
	if thread, ok := s.findThreadResponse(threadID); ok {
		threadScope := threadMemoryScopeFromRaw(thread)
		if metadata := mapFromAny(thread["metadata"]); len(metadata) > 0 {
			threadScope = threadMemoryScopeFromMetadata(metadata).Merge(threadScope)
		}
		scope = scope.Merge(threadScope)
		found = true
	}
	if found {
		return scope, true
	}
	return threadMemoryScope{}, false
}

func (s *Server) gatewayMemoryScopeFromThreadID(threadID string) (pkgmemory.Scope, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return pkgmemory.Scope{}, fmt.Errorf("thread ID required")
	}
	threadScope, ok := s.resolveThreadMemoryScope(threadID)
	if !ok {
		return pkgmemory.Scope{}, fmt.Errorf("memory thread_id not found")
	}
	return defaultGatewayMemoryScopeFromThread(threadID, threadScope), nil
}

func defaultMemoryScopeTypeFromThread(scope threadMemoryScope) pkgmemory.ScopeType {
	switch {
	case strings.TrimSpace(scope.GroupID) != "":
		return pkgmemory.ScopeGroup
	case strings.TrimSpace(scope.UserID) != "":
		return pkgmemory.ScopeUser
	default:
		return pkgmemory.ScopeSession
	}
}

func defaultGatewayMemoryScopeFromThread(threadID string, scope threadMemoryScope) pkgmemory.Scope {
	switch defaultMemoryScopeTypeFromThread(scope) {
	case pkgmemory.ScopeGroup:
		derived := pkgmemory.GroupScope(scope.GroupID)
		derived.Namespace = scope.Namespace
		return derived.Normalized()
	case pkgmemory.ScopeUser:
		derived := pkgmemory.UserScope(scope.UserID)
		derived.Namespace = scope.Namespace
		return derived.Normalized()
	default:
		derived := pkgmemory.SessionScope(strings.TrimSpace(threadID))
		derived.Namespace = scope.Namespace
		return derived.Normalized()
	}
}

func (s *Server) bootstrapGatewayMemory(ctx context.Context) error {
	runtimeMemory := s.runtimeMemory()
	if s == nil || runtimeMemory == nil || !runtimeMemory.Enabled() {
		return nil
	}
	mem, ok, err := s.loadGatewayMemoryFromStore(ctx)
	if err != nil {
		return err
	}
	if ok {
		s.uiStateMu.Lock()
		s.setMemoryLocked(mem)
		s.uiStateMu.Unlock()
		return nil
	}

	s.uiStateMu.RLock()
	current := s.getMemoryLocked()
	s.uiStateMu.RUnlock()
	if !gatewayMemoryHasContent(current) {
		return nil
	}
	return s.persistGatewayMemoryToStore(ctx, current)
}

func (s *Server) gatewayMemoryGet(ctx context.Context, scope pkgmemory.Scope) gatewayMemoryResponse {
	if !isGatewayGlobalMemoryScope(scope) {
		scope = scope.Normalized()
		mem, ok, err := s.loadScopedMemoryFromStore(ctx, scope)
		if err != nil || !ok {
			return defaultGatewayMemory()
		}
		mem.Facts = s.memoryFactsWithSourceThreads(mem.Facts)
		return mem
	}
	s.uiStateMu.RLock()
	mem := s.getMemoryLocked()
	s.uiStateMu.RUnlock()
	mem.Facts = s.memoryFactsWithSourceThreads(mem.Facts)
	return mem
}

func mergeGatewayMemorySnapshot(state *gatewayPersistedState, raw map[string]any) {
	if state == nil || raw == nil {
		return
	}
	memRaw := mapFromAny(raw["memory"])
	if memRaw == nil {
		return
	}
	compatMem := gatewayMemoryResponseFromMap(memRaw)
	if state.Memory.Version == "" {
		state.Memory = compatMem
		return
	}
	if state.Memory.LastUpdated == "" {
		state.Memory.LastUpdated = compatMem.LastUpdated
	}
	if state.Memory.User == (memoryUser{}) {
		state.Memory.User = compatMem.User
	}
	if state.Memory.History == (memoryHistory{}) {
		state.Memory.History = compatMem.History
	}
	if len(state.Memory.Facts) == 0 && len(compatMem.Facts) > 0 {
		state.Memory.Facts = compatMem.Facts
	}
}

func (s *Server) hydrateGatewayMemoryCacheLocked(state gatewayPersistedState) {
	if s == nil {
		return
	}
	if state.Memory.Version != "" {
		s.setMemoryLocked(state.Memory)
	}
	if mem, ok := s.loadMemoryFromFile(); ok {
		if !gatewayMemoryHasContent(s.getMemoryLocked()) {
			s.setMemoryLocked(mem)
		}
	}
}

func (s *Server) gatewayMemorySnapshotLocked() gatewayMemoryResponse {
	if s == nil {
		return defaultGatewayMemory()
	}
	return s.getMemoryLocked()
}

func (s *Server) gatewayMemoryExport(ctx context.Context, scope pkgmemory.Scope) gatewayMemoryResponse {
	return s.gatewayMemoryGet(ctx, scope)
}

func (s *Server) gatewayMemoryReload(ctx context.Context, scope pkgmemory.Scope) (gatewayMemoryResponse, error) {
	if !isGatewayGlobalMemoryScope(scope) {
		scope = scope.Normalized()
		if mem, ok, err := s.loadScopedMemoryFromStore(ctx, scope); err != nil {
			return gatewayMemoryResponse{}, err
		} else if ok {
			return mem, nil
		}
		return defaultGatewayMemory(), nil
	}
	if mem, ok, err := s.loadGatewayMemoryFromStore(ctx); err != nil {
		return gatewayMemoryResponse{}, err
	} else if ok {
		s.uiStateMu.Lock()
		s.setMemoryLocked(mem)
		s.uiStateMu.Unlock()
		if err := s.persistMemoryFile(); err != nil {
			return gatewayMemoryResponse{}, err
		}
		if err := s.persistGatewayState(); err != nil {
			return gatewayMemoryResponse{}, err
		}
		return s.gatewayMemoryGet(ctx, pkgmemory.Scope{}), nil
	}
	if mem, ok, err := s.loadMemoryFromFileStrict(); err != nil {
		return gatewayMemoryResponse{}, err
	} else if ok {
		s.uiStateMu.Lock()
		s.setMemoryLocked(mem)
		s.uiStateMu.Unlock()
		if err := s.persistGatewayMemoryToStore(ctx, mem); err != nil {
			return gatewayMemoryResponse{}, err
		}
		if err := s.persistGatewayState(); err != nil {
			return gatewayMemoryResponse{}, err
		}
	}
	return s.gatewayMemoryGet(ctx, pkgmemory.Scope{}), nil
}

func (s *Server) gatewayMemoryImport(ctx context.Context, scope pkgmemory.Scope, mem gatewayMemoryResponse) (gatewayMemoryResponse, error) {
	if !isGatewayGlobalMemoryScope(scope) {
		scope = scope.Normalized()
		normalized := normalizeGatewayMemoryResponse(mem)
		if normalized.LastUpdated == "" {
			normalized.LastUpdated = time.Now().UTC().Format(time.RFC3339)
		}
		if err := s.persistScopedMemoryToStore(ctx, scope, normalized); err != nil {
			return gatewayMemoryResponse{}, err
		}
		return s.gatewayMemoryGet(ctx, scope), nil
	}
	normalized := normalizeGatewayMemoryResponse(mem)
	if normalized.LastUpdated == "" {
		normalized.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	}
	if err := s.setAndPersistGatewayMemory(ctx, normalized); err != nil {
		return gatewayMemoryResponse{}, err
	}
	return s.gatewayMemoryGet(ctx, pkgmemory.Scope{}), nil
}

func (s *Server) gatewayMemoryClear(ctx context.Context, scope pkgmemory.Scope) (gatewayMemoryResponse, error) {
	if !isGatewayGlobalMemoryScope(scope) {
		scope = scope.Normalized()
		mem := defaultGatewayMemory()
		mem.LastUpdated = time.Now().UTC().Format(time.RFC3339)
		if err := s.persistScopedMemoryToStore(ctx, scope, mem); err != nil {
			return gatewayMemoryResponse{}, err
		}
		return s.gatewayMemoryGet(ctx, scope), nil
	}
	if err := s.setAndPersistGatewayMemory(ctx, defaultGatewayMemory()); err != nil {
		return gatewayMemoryResponse{}, err
	}
	return s.gatewayMemoryGet(ctx, pkgmemory.Scope{}), nil
}

func (s *Server) gatewayMemoryDeleteFact(ctx context.Context, scope pkgmemory.Scope, factID string) (gatewayMemoryResponse, error) {
	factID = strings.TrimSpace(factID)
	if factID == "" {
		return gatewayMemoryResponse{}, fmt.Errorf("Memory fact '%s' not found", factID)
	}
	mem, err := s.gatewayMemoryEditable(ctx, scope)
	if err != nil {
		return gatewayMemoryResponse{}, err
	}
	s.uiStateMu.Lock()
	newFacts := make([]memoryFact, 0, len(mem.Facts))
	found := false
	for _, fact := range mem.Facts {
		if fact.ID == factID {
			found = true
			continue
		}
		newFacts = append(newFacts, fact)
	}
	if !found {
		s.uiStateMu.Unlock()
		return gatewayMemoryResponse{}, fmt.Errorf("Memory fact '%s' not found", factID)
	}
	mem.Facts = newFacts
	mem.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	s.uiStateMu.Unlock()
	if err := s.persistGatewayMemorySelection(ctx, scope, mem); err != nil {
		return gatewayMemoryResponse{}, err
	}
	return s.gatewayMemoryGet(ctx, scope), nil
}

func (s *Server) gatewayMemoryCreateFact(ctx context.Context, scope pkgmemory.Scope, req memoryFactCreateRequest) (gatewayMemoryResponse, error) {
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return gatewayMemoryResponse{}, fmt.Errorf("Memory fact content cannot be empty.")
	}
	if req.Confidence < 0 || req.Confidence > 1 {
		return gatewayMemoryResponse{}, fmt.Errorf("Invalid confidence value; must be between 0 and 1.")
	}
	category := strings.TrimSpace(req.Category)
	if category == "" {
		category = "context"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	mem, err := s.gatewayMemoryEditable(ctx, scope)
	if err != nil {
		return gatewayMemoryResponse{}, err
	}

	s.uiStateMu.Lock()
	mem.Facts = append(mem.Facts, memoryFact{
		ID:         fmt.Sprintf("fact_%d", time.Now().UTC().UnixNano()),
		Content:    content,
		Category:   category,
		Confidence: req.Confidence,
		CreatedAt:  now,
		Source:     "manual",
	})
	mem.LastUpdated = now
	s.uiStateMu.Unlock()

	if err := s.persistGatewayMemorySelection(ctx, scope, mem); err != nil {
		return gatewayMemoryResponse{}, err
	}
	return s.gatewayMemoryGet(ctx, scope), nil
}

func (s *Server) gatewayMemoryUpdateFact(ctx context.Context, scope pkgmemory.Scope, factID string, req memoryFactPatchRequest) (gatewayMemoryResponse, error) {
	factID = strings.TrimSpace(factID)
	if factID == "" {
		return gatewayMemoryResponse{}, fmt.Errorf("Memory fact '%s' not found", factID)
	}
	mem, err := s.gatewayMemoryEditable(ctx, scope)
	if err != nil {
		return gatewayMemoryResponse{}, err
	}

	s.uiStateMu.Lock()
	found := false
	for i := range mem.Facts {
		if mem.Facts[i].ID != factID {
			continue
		}
		found = true
		if req.Content != nil {
			content := strings.TrimSpace(*req.Content)
			if content == "" {
				s.uiStateMu.Unlock()
				return gatewayMemoryResponse{}, fmt.Errorf("Memory fact content cannot be empty.")
			}
			mem.Facts[i].Content = content
		}
		if req.Category != nil {
			category := strings.TrimSpace(*req.Category)
			if category == "" {
				category = "context"
			}
			mem.Facts[i].Category = category
		}
		if req.Confidence != nil {
			if *req.Confidence < 0 || *req.Confidence > 1 {
				s.uiStateMu.Unlock()
				return gatewayMemoryResponse{}, fmt.Errorf("Invalid confidence value; must be between 0 and 1.")
			}
			mem.Facts[i].Confidence = *req.Confidence
		}
		break
	}
	if !found {
		s.uiStateMu.Unlock()
		return gatewayMemoryResponse{}, fmt.Errorf("Memory fact '%s' not found", factID)
	}
	mem.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	s.uiStateMu.Unlock()

	if err := s.persistGatewayMemorySelection(ctx, scope, mem); err != nil {
		return gatewayMemoryResponse{}, err
	}
	return s.gatewayMemoryGet(ctx, scope), nil
}

func (s *Server) loadGatewayMemoryFromStore(ctx context.Context) (gatewayMemoryResponse, bool, error) {
	runtimeMemory := s.runtimeMemory()
	if s == nil || runtimeMemory == nil || !runtimeMemory.Enabled() {
		return gatewayMemoryResponse{}, false, nil
	}
	doc, ok, err := runtimeMemory.LoadDocument(ctx, gatewayMemorySessionID)
	if err != nil {
		return gatewayMemoryResponse{}, false, err
	}
	if !ok {
		return gatewayMemoryResponse{}, false, nil
	}
	return gatewayMemoryResponseFromDocument(doc), true, nil
}

func (s *Server) persistGatewayMemoryToStore(ctx context.Context, mem gatewayMemoryResponse) error {
	runtimeMemory := s.runtimeMemory()
	if s == nil || runtimeMemory == nil || !runtimeMemory.Enabled() {
		return nil
	}
	return runtimeMemory.SaveDocument(ctx, gatewayMemoryDocument(mem))
}

func (s *Server) loadScopedMemoryFromStore(ctx context.Context, scope pkgmemory.Scope) (gatewayMemoryResponse, bool, error) {
	runtimeMemory := s.runtimeMemory()
	if s == nil || runtimeMemory == nil || !runtimeMemory.Enabled() {
		return gatewayMemoryResponse{}, false, nil
	}
	doc, ok, err := runtimeMemory.LoadScopeDocument(ctx, scope.Normalized())
	if err != nil {
		return gatewayMemoryResponse{}, false, err
	}
	if !ok {
		return gatewayMemoryResponse{}, false, nil
	}
	return gatewayMemoryResponseFromDocument(doc), true, nil
}

func (s *Server) persistScopedMemoryToStore(ctx context.Context, scope pkgmemory.Scope, mem gatewayMemoryResponse) error {
	runtimeMemory := s.runtimeMemory()
	if s == nil || runtimeMemory == nil || !runtimeMemory.Enabled() {
		return fmt.Errorf("memory runtime is not configured")
	}
	return runtimeMemory.SaveScopeDocument(ctx, scope.Normalized(), gatewayMemoryDocument(mem))
}

func (s *Server) gatewayMemoryEditable(ctx context.Context, scope pkgmemory.Scope) (gatewayMemoryResponse, error) {
	if isGatewayGlobalMemoryScope(scope) {
		s.uiStateMu.RLock()
		mem := s.getMemoryLocked()
		s.uiStateMu.RUnlock()
		return mem, nil
	}
	scope = scope.Normalized()
	if mem, ok, err := s.loadScopedMemoryFromStore(ctx, scope); err != nil {
		return gatewayMemoryResponse{}, err
	} else if ok {
		return mem, nil
	}
	return defaultGatewayMemory(), nil
}

func (s *Server) persistGatewayMemorySelection(ctx context.Context, scope pkgmemory.Scope, mem gatewayMemoryResponse) error {
	if isGatewayGlobalMemoryScope(scope) {
		return s.setAndPersistGatewayMemory(ctx, mem)
	}
	scope = scope.Normalized()
	return s.persistScopedMemoryToStore(ctx, scope, mem)
}

func (s *Server) setAndPersistGatewayMemory(ctx context.Context, mem gatewayMemoryResponse) error {
	normalized := normalizeGatewayMemoryResponse(mem)
	if normalized.LastUpdated == "" {
		normalized.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	}
	s.uiStateMu.Lock()
	s.setMemoryLocked(normalized)
	s.uiStateMu.Unlock()
	if err := s.persistGatewayMemoryToStore(ctx, normalized); err != nil {
		return err
	}
	if err := s.persistMemoryFile(); err != nil {
		return err
	}
	if err := s.persistGatewayState(); err != nil {
		return err
	}
	return nil
}

func gatewayMemoryDocument(mem gatewayMemoryResponse) pkgmemory.Document {
	mem = normalizeGatewayMemoryResponse(mem)
	updatedAt := parseGatewayMemoryTimestamp(mem.LastUpdated)
	doc := pkgmemory.Document{
		SessionID: gatewayMemorySessionID,
		Source:    gatewayMemorySessionID,
		UpdatedAt: updatedAt,
		User: pkgmemory.UserMemory{
			WorkContext:     strings.TrimSpace(mem.User.WorkContext.Summary),
			PersonalContext: strings.TrimSpace(mem.User.PersonalContext.Summary),
			TopOfMind:       strings.TrimSpace(mem.User.TopOfMind.Summary),
		},
		History: pkgmemory.HistoryMemory{
			RecentMonths:       strings.TrimSpace(mem.History.RecentMonths.Summary),
			EarlierContext:     strings.TrimSpace(mem.History.EarlierContext.Summary),
			LongTermBackground: strings.TrimSpace(mem.History.LongTermBackground.Summary),
		},
		Facts: make([]pkgmemory.Fact, 0, len(mem.Facts)),
	}
	for _, fact := range mem.Facts {
		doc.Facts = append(doc.Facts, pkgmemory.Fact{
			ID:         strings.TrimSpace(fact.ID),
			Content:    strings.TrimSpace(fact.Content),
			Category:   strings.TrimSpace(fact.Category),
			Confidence: fact.Confidence,
			Source:     strings.TrimSpace(fact.Source),
			CreatedAt:  parseGatewayMemoryTimestamp(fact.CreatedAt),
			UpdatedAt:  updatedAt,
		})
	}
	return doc
}

func parseGatewayMemoryTimestamp(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return ts.UTC()
}

func gatewayMemoryHasContent(mem gatewayMemoryResponse) bool {
	if len(mem.Facts) > 0 {
		return true
	}
	fields := []string{
		mem.User.WorkContext.Summary,
		mem.User.PersonalContext.Summary,
		mem.User.TopOfMind.Summary,
		mem.History.RecentMonths.Summary,
		mem.History.EarlierContext.Summary,
		mem.History.LongTermBackground.Summary,
	}
	for _, field := range fields {
		if strings.TrimSpace(field) != "" {
			return true
		}
	}
	return false
}

func (s *Server) loadMemoryFromFile() (gatewayMemoryResponse, bool) {
	mem, ok, err := s.loadMemoryFromFileStrict()
	if err != nil {
		return gatewayMemoryResponse{}, false
	}
	return mem, ok
}

func (s *Server) loadMemoryFromFileStrict() (gatewayMemoryResponse, bool, error) {
	data, err := os.ReadFile(s.memoryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return gatewayMemoryResponse{}, false, nil
		}
		return gatewayMemoryResponse{}, false, err
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil {
		if nested, ok := wrapper["memory"]; ok && len(nested) > 0 {
			data = nested
		} else if nested, ok := wrapper["data"]; ok && len(nested) > 0 {
			data = nested
		}
	}
	var mem gatewayMemoryResponse
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return gatewayMemoryResponse{}, false, err
	}
	compat := gatewayMemoryResponseFromMap(raw)
	if err := json.Unmarshal(data, &mem); err != nil {
		if compat.Version == "" {
			return gatewayMemoryResponse{}, false, err
		}
		return compat, true, nil
	}
	if mem.Version == "" {
		mem.Version = compat.Version
	}
	if mem.LastUpdated == "" {
		mem.LastUpdated = compat.LastUpdated
	}
	if mem.User == (memoryUser{}) {
		mem.User = compat.User
	}
	if mem.History == (memoryHistory{}) {
		mem.History = compat.History
	}
	if len(mem.Facts) == 0 && len(compat.Facts) > 0 {
		mem.Facts = compat.Facts
	} else if len(mem.Facts) == len(compat.Facts) {
		for i := range mem.Facts {
			if mem.Facts[i].ID == "" {
				mem.Facts[i].ID = compat.Facts[i].ID
			}
			if mem.Facts[i].Content == "" {
				mem.Facts[i].Content = compat.Facts[i].Content
			}
			if mem.Facts[i].Category == "" {
				mem.Facts[i].Category = compat.Facts[i].Category
			}
			if mem.Facts[i].Confidence == 0 {
				mem.Facts[i].Confidence = compat.Facts[i].Confidence
			}
			if mem.Facts[i].CreatedAt == "" {
				mem.Facts[i].CreatedAt = compat.Facts[i].CreatedAt
			}
			if mem.Facts[i].Source == "" {
				mem.Facts[i].Source = compat.Facts[i].Source
			}
		}
	}
	if mem.Version == "" {
		return gatewayMemoryResponse{}, false, nil
	}
	return mem, true, nil
}

func gatewayMemoryResponseFromMap(raw map[string]any) gatewayMemoryResponse {
	if raw == nil {
		return gatewayMemoryResponse{}
	}
	userRaw := mapFromAny(raw["user"])
	historyRaw := mapFromAny(raw["history"])
	return gatewayMemoryResponse{
		Version:     firstNonEmpty(stringFromAny(raw["version"])),
		LastUpdated: firstNonEmpty(stringFromAny(raw["lastUpdated"]), stringFromAny(raw["last_updated"])),
		User: memoryUser{
			WorkContext: memorySectionFromMap(mapFromAny(firstNonNil(raw["workContext"], raw["work_context"], userRaw["workContext"], userRaw["work_context"]))),
			PersonalContext: memorySectionFromMap(mapFromAny(firstNonNil(
				raw["personalContext"], raw["personal_context"], userRaw["personalContext"], userRaw["personal_context"],
			))),
			TopOfMind: memorySectionFromMap(mapFromAny(firstNonNil(
				raw["topOfMind"], raw["top_of_mind"], userRaw["topOfMind"], userRaw["top_of_mind"],
			))),
		},
		History: memoryHistory{
			RecentMonths: memorySectionFromMap(mapFromAny(firstNonNil(
				raw["recentMonths"], raw["recent_months"], historyRaw["recentMonths"], historyRaw["recent_months"],
			))),
			EarlierContext: memorySectionFromMap(mapFromAny(firstNonNil(
				raw["earlierContext"], raw["earlier_context"], historyRaw["earlierContext"], historyRaw["earlier_context"],
			))),
			LongTermBackground: memorySectionFromMap(mapFromAny(firstNonNil(
				raw["longTermBackground"], raw["long_term_background"], historyRaw["longTermBackground"], historyRaw["long_term_background"],
			))),
		},
		Facts: memoryFactsFromAny(raw["facts"]),
	}
}

func memorySectionFromMap(raw map[string]any) memorySection {
	if raw == nil {
		return memorySection{}
	}
	return memorySection{
		Summary:   firstNonEmpty(stringFromAny(raw["summary"])),
		UpdatedAt: firstNonEmpty(stringFromAny(raw["updatedAt"]), stringFromAny(raw["updated_at"])),
	}
}

func memoryFactsFromAny(raw any) []memoryFact {
	if wrapped := mapFromAny(raw); wrapped != nil {
		raw = firstNonNil(wrapped["items"], wrapped["facts"])
	}
	items, _ := raw.([]any)
	facts := make([]memoryFact, 0, len(items))
	for _, item := range items {
		factMap := mapFromAny(item)
		if factMap == nil {
			continue
		}
		facts = append(facts, memoryFact{
			ID:       firstNonEmpty(stringFromAny(factMap["id"])),
			Content:  firstNonEmpty(stringFromAny(factMap["content"])),
			Category: firstNonEmpty(stringFromAny(factMap["category"])),
			Confidence: func() float64 {
				if v := floatPointerFromAny(factMap["confidence"]); v != nil {
					return *v
				}
				return 0
			}(),
			CreatedAt: firstNonEmpty(stringFromAny(factMap["createdAt"]), stringFromAny(factMap["created_at"])),
			Source:    firstNonEmpty(stringFromAny(factMap["source"])),
		})
	}
	return facts
}

func (s *Server) persistMemoryFile() error {
	s.uiStateMu.RLock()
	mem := s.getMemoryLocked()
	s.uiStateMu.RUnlock()
	data, err := json.MarshalIndent(mem, "", "  ")
	if err != nil {
		return err
	}
	path := s.memoryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func normalizeGatewayMemoryResponse(mem gatewayMemoryResponse) gatewayMemoryResponse {
	if mem.Version == "" {
		mem.Version = "1"
	}
	if mem.Facts == nil {
		mem.Facts = []memoryFact{}
	}
	for i := range mem.Facts {
		mem.Facts[i].ID = strings.TrimSpace(mem.Facts[i].ID)
		mem.Facts[i].Content = strings.TrimSpace(mem.Facts[i].Content)
		if mem.Facts[i].Category == "" {
			mem.Facts[i].Category = "context"
		}
	}
	return mem
}
