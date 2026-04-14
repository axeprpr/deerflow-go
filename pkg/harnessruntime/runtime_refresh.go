package harnessruntime

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/llm"
)

type runtimeRefreshKey struct {
	ProviderID       string
	MaxTurns         int
	ProfileBuilderID uintptr
}

func makeRuntimeRefreshKey(provider llm.LLMProvider, maxTurns int, profileBuilder RuntimeProfileBuilderFactory) runtimeRefreshKey {
	return runtimeRefreshKey{
		ProviderID:       runtimeProviderIdentity(provider),
		MaxTurns:         maxTurns,
		ProfileBuilderID: runtimeProfileBuilderIdentity(profileBuilder),
	}
}

func runtimeProviderIdentity(provider llm.LLMProvider) string {
	if provider == nil {
		return ""
	}
	v := reflect.ValueOf(provider)
	if !v.IsValid() {
		return ""
	}
	typeName := v.Type().String()
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		if v.IsNil() {
			return typeName + ":nil"
		}
		return typeName + ":" + strconv.FormatUint(uint64(v.Pointer()), 16)
	default:
		if v.Type().Comparable() {
			return typeName + ":" + fmt.Sprint(v.Interface())
		}
		return typeName
	}
}

func runtimeProfileBuilderIdentity(profileBuilder RuntimeProfileBuilderFactory) uintptr {
	if profileBuilder == nil {
		return 0
	}
	return reflect.ValueOf(profileBuilder).Pointer()
}

// RefreshRuntimeView rebuilds and rebinds the runtime view owned by the node,
// and reuses the current view when refresh inputs are unchanged.
func (n *RuntimeNode) RefreshRuntimeView(provider llm.LLMProvider, maxTurns int, current *harness.Runtime, profileBuilder RuntimeProfileBuilderFactory) *harness.Runtime {
	if n == nil {
		return RefreshHarnessRuntime(nil, provider, maxTurns, current, profileBuilder)
	}
	key := makeRuntimeRefreshKey(provider, maxTurns, profileBuilder)

	n.runtimeMu.RLock()
	bound := n.Runtime
	lastKey := n.runtimeRefresh
	n.runtimeMu.RUnlock()

	base := current
	if base == nil {
		base = bound
	}
	if bound != nil && base == bound && lastKey == key {
		return bound
	}

	runtime := buildHarnessRuntime(n, provider, maxTurns, base, profileBuilder)
	n.runtimeMu.Lock()
	n.Runtime = runtime
	n.runtimeRefresh = key
	n.runtimeMu.Unlock()
	return runtime
}
