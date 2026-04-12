package harnessruntime

import (
	"strings"

	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
)

type MemoryScopePolicy interface {
	Resolve(threadID string, agentName string) pkgmemory.Scope
}

type DefaultMemoryScopePolicy struct{}

func (DefaultMemoryScopePolicy) Resolve(threadID string, agentName string) pkgmemory.Scope {
	threadID = strings.TrimSpace(threadID)
	agentName = strings.ToLower(strings.TrimSpace(agentName))
	if agentName != "" {
		return pkgmemory.AgentScope(agentName)
	}
	return pkgmemory.SessionScope(threadID)
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

func (s MemoryScopeService) Resolve(threadID string, agentName string) pkgmemory.Scope {
	if s.policy == nil {
		return pkgmemory.Scope{}
	}
	return s.policy.Resolve(threadID, agentName).Normalized()
}

func (s MemoryScopeService) ResolveKey(threadID string, agentName string) string {
	return s.Resolve(threadID, agentName).Key()
}
