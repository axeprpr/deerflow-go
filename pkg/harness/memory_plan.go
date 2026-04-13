package harness

import (
	"strings"

	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
)

type MemoryScopePlan struct {
	Primary pkgmemory.Scope
	Inject  []pkgmemory.Scope
	Update  []pkgmemory.Scope
}

func NormalizeMemoryScopePlan(plan MemoryScopePlan) MemoryScopePlan {
	normalized := MemoryScopePlan{
		Primary: plan.Primary.Normalized(),
		Inject:  normalizeMemoryScopes(plan.Inject),
		Update:  normalizeMemoryScopes(plan.Update),
	}
	if normalized.Primary.Key() == "" {
		switch {
		case len(normalized.Update) > 0:
			normalized.Primary = normalized.Update[0]
		case len(normalized.Inject) > 0:
			normalized.Primary = normalized.Inject[0]
		}
	}
	if normalized.Primary.Key() != "" {
		if len(normalized.Inject) == 0 {
			normalized.Inject = []pkgmemory.Scope{normalized.Primary}
		}
		if len(normalized.Update) == 0 {
			normalized.Update = []pkgmemory.Scope{normalized.Primary}
		}
	}
	return normalized
}

func normalizeMemoryScopes(scopes []pkgmemory.Scope) []pkgmemory.Scope {
	if len(scopes) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]pkgmemory.Scope, 0, len(scopes))
	for _, scope := range scopes {
		scope = scope.Normalized()
		if strings.TrimSpace(scope.Key()) == "" {
			continue
		}
		if _, ok := seen[scope.Key()]; ok {
			continue
		}
		seen[scope.Key()] = struct{}{}
		out = append(out, scope)
	}
	return out
}
