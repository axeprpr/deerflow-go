package harness

import (
	"context"
	"testing"

	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
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
