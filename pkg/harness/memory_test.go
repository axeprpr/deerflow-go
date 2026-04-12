package harness

import (
	"context"
	"testing"

	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestMemoryRuntimeScopeMethodsUseScopedKeys(t *testing.T) {
	store, err := pkgmemory.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	runtime := NewMemoryRuntime(store, nil)
	scope := pkgmemory.UserScope("user-1")

	doc := pkgmemory.Document{
		User: pkgmemory.UserMemory{TopOfMind: "Ship runtime refactor"},
	}
	if err := runtime.SaveScopeDocument(context.Background(), scope, doc); err != nil {
		t.Fatalf("SaveScopeDocument() error = %v", err)
	}
	loaded, found, err := runtime.LoadScopeDocument(context.Background(), scope)
	if err != nil {
		t.Fatalf("LoadScopeDocument() error = %v", err)
	}
	if !found {
		t.Fatal("LoadScopeDocument() found = false, want true")
	}
	if loaded.SessionID != scope.Key() {
		t.Fatalf("session_id = %q, want %q", loaded.SessionID, scope.Key())
	}
}

func TestMemoryRuntimeScheduleScopeUpdateUsesUpdater(t *testing.T) {
	updater := &memoryUpdaterStub{}
	runtime := NewMemoryRuntimeWithUpdater(&memoryStorageStub{}, nil, updater)
	scope := pkgmemory.AgentScope("planner")

	runtime.ScheduleScopeUpdate(scope, []models.Message{{Role: models.RoleAI, Content: "done"}})

	if updater.scope.Key() != "agent:planner" {
		t.Fatalf("scope key = %q, want %q", updater.scope.Key(), "agent:planner")
	}
	if len(updater.messages) != 1 || updater.messages[0].Content != "done" {
		t.Fatalf("messages = %#v", updater.messages)
	}
}

type memoryUpdaterStub struct {
	scope    pkgmemory.Scope
	messages []models.Message
}

func (s *memoryUpdaterStub) Schedule(scope pkgmemory.Scope, messages []models.Message) {
	s.scope = scope
	s.messages = append([]models.Message(nil), messages...)
}

type memoryStorageStub struct{}

func (*memoryStorageStub) AutoMigrate(context.Context) error { return nil }
func (*memoryStorageStub) Load(context.Context, string) (pkgmemory.Document, error) {
	return pkgmemory.Document{}, pkgmemory.ErrNotFound
}
func (*memoryStorageStub) Save(context.Context, pkgmemory.Document) error { return nil }
