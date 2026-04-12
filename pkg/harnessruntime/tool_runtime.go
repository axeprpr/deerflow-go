package harnessruntime

import (
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"github.com/axeprpr/deerflow-go/pkg/tools/builtin"
)

// NewDefaultToolRuntime builds the default runtime tool surface owned by the
// harness/runtime layer rather than compat transport code.
func NewDefaultToolRuntime(provider llm.LLMProvider, clarify *clarification.Manager, sandboxRuntime harness.SandboxRuntime) harness.ToolRuntime {
	registry := tools.NewRegistry()
	for _, tool := range builtin.FileTools() {
		_ = registry.Register(tool)
	}
	for _, tool := range builtin.WebTools() {
		_ = registry.Register(tool)
	}
	_ = registry.Register(builtin.BashTool())
	if clarify != nil {
		_ = registry.Register(clarification.AskClarificationTool(clarify))
	}
	var sandboxProvider func() (*sandbox.Sandbox, error)
	if sandboxRuntime != nil {
		sandboxProvider = func() (*sandbox.Sandbox, error) {
			return sandboxRuntime.Resolve(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: true}})
		}
	}
	subagents := agent.NewSubagentPool(provider, registry, nil, sandboxProvider, 2, 2*time.Minute)
	_ = registry.Register(tools.TaskTool(subagents))
	return harness.NewStaticToolRuntime(registry, nil, subagents)
}
