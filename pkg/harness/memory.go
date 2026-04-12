package harness

import (
	"context"
	"errors"
	"strings"

	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

// MemoryRuntime is the harness-owned entrypoint for durable memory concerns.
// API/compat layers should depend on this boundary instead of manipulating
// memory storage directly.
type MemoryRuntime struct {
	store   pkgmemory.Storage
	service *pkgmemory.Service
}

func NewMemoryRuntime(store pkgmemory.Storage, extractor pkgmemory.Extractor) *MemoryRuntime {
	if store == nil {
		return &MemoryRuntime{}
	}
	return &MemoryRuntime{
		store:   store,
		service: pkgmemory.NewService(store, extractor),
	}
}

func (m *MemoryRuntime) Enabled() bool {
	return m != nil && m.store != nil
}

func (m *MemoryRuntime) Store() pkgmemory.Storage {
	if m == nil {
		return nil
	}
	return m.store
}

func (m *MemoryRuntime) LoadDocument(ctx context.Context, sessionID string) (pkgmemory.Document, bool, error) {
	if !m.Enabled() {
		return pkgmemory.Document{}, false, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return pkgmemory.Document{}, false, errors.New("session id is required")
	}
	doc, err := m.store.Load(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pkgmemory.ErrNotFound) {
			return pkgmemory.Document{}, false, nil
		}
		return pkgmemory.Document{}, false, err
	}
	return doc, true, nil
}

func (m *MemoryRuntime) LoadScopeDocument(ctx context.Context, scope pkgmemory.Scope) (pkgmemory.Document, bool, error) {
	return m.LoadDocument(ctx, scope.Key())
}

func (m *MemoryRuntime) SaveDocument(ctx context.Context, doc pkgmemory.Document) error {
	if !m.Enabled() {
		return nil
	}
	return m.store.Save(ctx, doc)
}

func (m *MemoryRuntime) SaveScopeDocument(ctx context.Context, scope pkgmemory.Scope, doc pkgmemory.Document) error {
	doc.SessionID = scope.Key()
	return m.SaveDocument(ctx, doc)
}

func (m *MemoryRuntime) Inject(ctx context.Context, sessionID, currentContext string) string {
	if m == nil || m.service == nil || !m.Enabled() {
		return ""
	}
	return m.service.InjectWithContext(ctx, sessionID, currentContext)
}

func (m *MemoryRuntime) InjectScope(ctx context.Context, scope pkgmemory.Scope, currentContext string) string {
	return m.Inject(ctx, scope.Key(), currentContext)
}

func (m *MemoryRuntime) ScheduleUpdate(sessionID string, messages []models.Message) {
	if m == nil || m.service == nil || !m.Enabled() {
		return
	}
	m.service.ScheduleUpdate(sessionID, messages)
}

func (m *MemoryRuntime) ScheduleScopeUpdate(scope pkgmemory.Scope, messages []models.Message) {
	m.ScheduleUpdate(scope.Key(), messages)
}
