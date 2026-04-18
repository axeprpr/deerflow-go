package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestMemoryRuntimeScopePersistenceIsolatedAcrossSessionUserAndGroup(t *testing.T) {
	store, err := pkgmemory.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	runtime := NewMemoryRuntime(store, nil)

	sessionScope := pkgmemory.SessionScope("thread-1")
	userScope := pkgmemory.Scope{Type: pkgmemory.ScopeUser, ID: "user-42", Namespace: "workspace-a"}
	groupScope := pkgmemory.Scope{Type: pkgmemory.ScopeGroup, ID: "team-7", Namespace: "workspace-a"}

	if err := runtime.SaveScopeDocument(context.Background(), sessionScope, pkgmemory.Document{User: pkgmemory.UserMemory{TopOfMind: "session-memory"}}); err != nil {
		t.Fatalf("save session scope memory: %v", err)
	}
	if err := runtime.SaveScopeDocument(context.Background(), userScope, pkgmemory.Document{User: pkgmemory.UserMemory{TopOfMind: "user-memory"}}); err != nil {
		t.Fatalf("save user scope memory: %v", err)
	}
	if err := runtime.SaveScopeDocument(context.Background(), groupScope, pkgmemory.Document{User: pkgmemory.UserMemory{TopOfMind: "group-memory"}}); err != nil {
		t.Fatalf("save group scope memory: %v", err)
	}

	sessionDoc, found, err := runtime.LoadScopeDocument(context.Background(), sessionScope)
	if err != nil || !found {
		t.Fatalf("load session scope memory: found=%v err=%v", found, err)
	}
	userDoc, found, err := runtime.LoadScopeDocument(context.Background(), userScope)
	if err != nil || !found {
		t.Fatalf("load user scope memory: found=%v err=%v", found, err)
	}
	groupDoc, found, err := runtime.LoadScopeDocument(context.Background(), groupScope)
	if err != nil || !found {
		t.Fatalf("load group scope memory: found=%v err=%v", found, err)
	}

	if sessionDoc.User.TopOfMind != "session-memory" || sessionDoc.SessionID != sessionScope.Key() {
		t.Fatalf("session doc = %+v", sessionDoc)
	}
	if userDoc.User.TopOfMind != "user-memory" || userDoc.SessionID != userScope.Key() {
		t.Fatalf("user doc = %+v", userDoc)
	}
	if groupDoc.User.TopOfMind != "group-memory" || groupDoc.SessionID != groupScope.Key() {
		t.Fatalf("group doc = %+v", groupDoc)
	}
}

func TestMemoryLifecycleHooksInjectScopesInPlannerOrder(t *testing.T) {
	store, err := pkgmemory.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	runtime := NewMemoryRuntime(store, nil)

	groupScope := pkgmemory.Scope{Type: pkgmemory.ScopeGroup, ID: "team-7", Namespace: "workspace-a"}
	userScope := pkgmemory.Scope{Type: pkgmemory.ScopeUser, ID: "user-42", Namespace: "workspace-a"}
	agentScope := pkgmemory.Scope{Type: pkgmemory.ScopeAgent, ID: "planner", Namespace: "workspace-a"}

	for scope, text := range map[pkgmemory.Scope]string{
		groupScope: "group-scope-memory",
		userScope:  "user-scope-memory",
		agentScope: "agent-scope-memory",
	} {
		if err := runtime.SaveScopeDocument(context.Background(), scope, pkgmemory.Document{
			User: pkgmemory.UserMemory{TopOfMind: text},
		}); err != nil {
			t.Fatalf("SaveScopeDocument(%s) error = %v", scope.Key(), err)
		}
	}

	hooks := MemoryLifecycleHooksWithScopePlanner(runtime, fixedMemoryScopePlanner{
		plan: NormalizeMemoryScopePlan(MemoryScopePlan{
			Primary: agentScope,
			Inject:  []pkgmemory.Scope{groupScope, userScope, agentScope},
			Update:  []pkgmemory.Scope{groupScope, userScope, agentScope},
		}),
	}, "memory_session_id")
	if hooks == nil {
		t.Fatal("MemoryLifecycleHooksWithScopePlanner() = nil")
	}

	state := &RunState{
		ThreadID: "thread-1",
		Spec: AgentSpec{
			SystemPrompt: "base-system-prompt",
		},
		Metadata: map[string]any{},
	}
	if err := hooks.Before(context.Background(), state); err != nil {
		t.Fatalf("Before() error = %v", err)
	}
	if got := stringValue(state.Metadata["memory_session_id"]); got != agentScope.Key() {
		t.Fatalf("memory_session_id=%q want=%q", got, agentScope.Key())
	}

	prompt := state.Spec.SystemPrompt
	groupIdx := strings.Index(prompt, "group-scope-memory")
	userIdx := strings.Index(prompt, "user-scope-memory")
	agentIdx := strings.Index(prompt, "agent-scope-memory")
	if groupIdx < 0 || userIdx < 0 || agentIdx < 0 {
		t.Fatalf("system prompt missing scoped memory injection: %s", prompt)
	}
	if !(groupIdx < userIdx && userIdx < agentIdx) {
		t.Fatalf("injection order mismatch: group=%d user=%d agent=%d prompt=%s", groupIdx, userIdx, agentIdx, prompt)
	}
}

func TestMemoryLifecycleHooksSchedulesUpdatesForAllPlannedScopes(t *testing.T) {
	updater := &recordingMemoryUpdater{}
	runtime := NewMemoryRuntimeWithUpdater(&memoryStorageStub{}, nil, updater)

	groupScope := pkgmemory.Scope{Type: pkgmemory.ScopeGroup, ID: "team-7", Namespace: "workspace-a"}
	userScope := pkgmemory.Scope{Type: pkgmemory.ScopeUser, ID: "user-42", Namespace: "workspace-a"}
	agentScope := pkgmemory.Scope{Type: pkgmemory.ScopeAgent, ID: "planner", Namespace: "workspace-a"}

	hooks := MemoryLifecycleHooksWithScopePlanner(runtime, fixedMemoryScopePlanner{
		plan: NormalizeMemoryScopePlan(MemoryScopePlan{
			Primary: agentScope,
			Inject:  []pkgmemory.Scope{groupScope, userScope, agentScope},
			Update:  []pkgmemory.Scope{groupScope, userScope, agentScope},
		}),
	}, "memory_session_id")
	if hooks == nil {
		t.Fatal("MemoryLifecycleHooksWithScopePlanner() = nil")
	}

	state := &RunState{
		ThreadID:  "thread-1",
		AgentName: "planner",
		Metadata:  map[string]any{},
	}
	result := &agent.RunResult{
		Messages: []models.Message{{Role: models.RoleAI, Content: "done"}},
	}
	if err := hooks.After(context.Background(), state, result); err != nil {
		t.Fatalf("After() error = %v", err)
	}

	if got := stringValue(state.Metadata["memory_session_id"]); got != agentScope.Key() {
		t.Fatalf("memory_session_id=%q want=%q", got, agentScope.Key())
	}
	if len(updater.scopes) != 3 {
		t.Fatalf("scheduled scopes=%d want=3", len(updater.scopes))
	}
	if updater.scopes[0].Key() != groupScope.Key() || updater.scopes[1].Key() != userScope.Key() || updater.scopes[2].Key() != agentScope.Key() {
		t.Fatalf("scheduled scope keys=%q,%q,%q", updater.scopes[0].Key(), updater.scopes[1].Key(), updater.scopes[2].Key())
	}
	for i := range updater.messages {
		if len(updater.messages[i]) != 1 || updater.messages[i][0].Content != "done" {
			t.Fatalf("scheduled messages[%d]=%#v", i, updater.messages[i])
		}
	}
}

type fixedMemoryScopePlanner struct {
	plan MemoryScopePlan
}

func (p fixedMemoryScopePlanner) PlanMemoryScopes(_ *RunState) MemoryScopePlan {
	return p.plan
}

type recordingMemoryUpdater struct {
	scopes   []pkgmemory.Scope
	messages [][]models.Message
}

func (u *recordingMemoryUpdater) Schedule(scope pkgmemory.Scope, messages []models.Message) {
	u.scopes = append(u.scopes, scope)
	u.messages = append(u.messages, append([]models.Message(nil), messages...))
}
