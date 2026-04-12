package harnessruntime

import (
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"github.com/axeprpr/deerflow-go/pkg/tools/builtin"
)

// NewDefaultToolRuntime builds the default runtime tool surface owned by the
// harness/runtime layer rather than compat transport code.
func NewDefaultToolRuntime(provider llm.LLMProvider, clarify *clarification.Manager) harness.ToolRuntime {
	registry := tools.NewRegistry()
	for _, tool := range builtin.FileTools() {
		_ = registry.Register(tool)
	}
	_ = registry.Register(builtin.BashTool())
	if clarify != nil {
		_ = registry.Register(clarification.AskClarificationTool(clarify))
	}
	subagents := agent.NewSubagentPool(provider, registry, nil, 2, 2*time.Minute)
	_ = registry.Register(tools.TaskTool(subagents))
	return harness.NewStaticToolRuntime(registry, nil, subagents)
}
