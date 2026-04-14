package harnessruntime

import (
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/llm"
)

// RefreshRuntimeView rebuilds and rebinds the runtime view owned by the node.
func (n *RuntimeNode) RefreshRuntimeView(provider llm.LLMProvider, maxTurns int, current *harness.Runtime, profileBuilder RuntimeProfileBuilderFactory) *harness.Runtime {
	return RefreshHarnessRuntime(n, provider, maxTurns, current, profileBuilder)
}
