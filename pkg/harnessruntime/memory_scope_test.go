package harnessruntime

import (
	"testing"

	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
)

func TestMemoryScopeServiceDefaultsToAgentAndSessionScopes(t *testing.T) {
	service := NewMemoryScopeService(nil)

	if got := service.Resolve("thread-1", "Planner"); got != (pkgmemory.AgentScope("planner")) {
		t.Fatalf("agent scope = %+v", got)
	}
	if got := service.Resolve("thread-1", ""); got != (pkgmemory.SessionScope("thread-1")) {
		t.Fatalf("session scope = %+v", got)
	}
}

func TestMemoryScopeServiceResolveKeyPreservesAgentCompatibility(t *testing.T) {
	service := NewMemoryScopeService(nil)
	if got := service.ResolveKey("thread-1", "Planner"); got != "agent:planner" {
		t.Fatalf("ResolveKey() = %q, want %q", got, "agent:planner")
	}
}
