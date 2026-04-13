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

func TestMemoryScopeServicePlansSharedScopes(t *testing.T) {
	service := NewMemoryScopeService(nil)
	plan := service.Plan(MemoryScopeHints{
		ThreadID:  "thread-1",
		AgentName: "Planner",
		UserID:    "user-1",
		GroupID:   "group-1",
		Namespace: "project-a",
	})
	if plan.Primary.Key() != "__scope__:agent:planner:project-a" {
		t.Fatalf("Primary = %q", plan.Primary.Key())
	}
	if len(plan.Inject) != 4 {
		t.Fatalf("Inject len = %d", len(plan.Inject))
	}
	if len(plan.Update) != 4 {
		t.Fatalf("Update len = %d", len(plan.Update))
	}
	if plan.Inject[0].Key() != "__scope__:group:group-1:project-a" {
		t.Fatalf("first inject = %q", plan.Inject[0].Key())
	}
}
