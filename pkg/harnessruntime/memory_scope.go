package harnessruntime

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
)

type MemoryScopeHints struct {
	ThreadID  string
	AgentName string
	UserID    string
	GroupID   string
	Namespace string
}

type MemoryScopePolicy interface {
	Plan(MemoryScopeHints) harness.MemoryScopePlan
}

type DefaultMemoryScopePolicy struct{}

func (DefaultMemoryScopePolicy) Plan(hints MemoryScopeHints) harness.MemoryScopePlan {
	threadID := strings.TrimSpace(hints.ThreadID)
	agentName := strings.ToLower(strings.TrimSpace(hints.AgentName))
	userID := strings.TrimSpace(hints.UserID)
	groupID := strings.TrimSpace(hints.GroupID)
	namespace := strings.TrimSpace(hints.Namespace)

	sessionScope := pkgmemory.SessionScope(threadID)
	if namespace != "" {
		sessionScope.Namespace = namespace
	}

	inject := make([]pkgmemory.Scope, 0, 4)
	update := make([]pkgmemory.Scope, 0, 4)
	if groupID != "" {
		scope := pkgmemory.GroupScope(groupID)
		scope.Namespace = namespace
		inject = append(inject, scope)
		update = append(update, scope)
	}
	if userID != "" {
		scope := pkgmemory.UserScope(userID)
		scope.Namespace = namespace
		inject = append(inject, scope)
		update = append(update, scope)
	}
	if threadID != "" {
		inject = append(inject, sessionScope)
		update = append(update, sessionScope)
	}

	primary := sessionScope
	if agentName != "" {
		scope := pkgmemory.AgentScope(agentName)
		if namespace != "" {
			scope.Namespace = namespace
		}
		inject = append(inject, scope)
		update = append(update, scope)
		primary = scope
	}

	return harness.NormalizeMemoryScopePlan(harness.MemoryScopePlan{
		Primary: primary,
		Inject:  inject,
		Update:  update,
	})
}

type MemoryScopeService struct {
	policy MemoryScopePolicy
}

func NewMemoryScopeService(policy MemoryScopePolicy) MemoryScopeService {
	if policy == nil {
		policy = DefaultMemoryScopePolicy{}
	}
	return MemoryScopeService{policy: policy}
}

func (s MemoryScopeService) Plan(hints MemoryScopeHints) harness.MemoryScopePlan {
	if s.policy == nil {
		return harness.MemoryScopePlan{}
	}
	return harness.NormalizeMemoryScopePlan(s.policy.Plan(hints))
}

func (s MemoryScopeService) Resolve(threadID string, agentName string) pkgmemory.Scope {
	return s.Plan(MemoryScopeHints{
		ThreadID:  threadID,
		AgentName: agentName,
	}).Primary
}

func (s MemoryScopeService) ResolveKey(threadID string, agentName string) string {
	return s.Resolve(threadID, agentName).Key()
}
