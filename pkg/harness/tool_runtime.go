package harness

import (
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

// ToolRuntime owns the default runtime tool surface. Compat layers should
// consume this boundary instead of constructing registries and task tools
// inline.
type ToolRuntime interface {
	Registry() *tools.Registry
	DeferredTools() []models.Tool
	Subagents() *subagent.Pool
}

type StaticToolRuntime struct {
	registry      *tools.Registry
	deferredTools []models.Tool
	subagents     *subagent.Pool
}

func NewStaticToolRuntime(registry *tools.Registry, deferred []models.Tool, subagents *subagent.Pool) *StaticToolRuntime {
	return &StaticToolRuntime{
		registry:      registry,
		deferredTools: append([]models.Tool(nil), deferred...),
		subagents:     subagents,
	}
}

func (r *StaticToolRuntime) Registry() *tools.Registry {
	if r == nil {
		return nil
	}
	return r.registry
}

func (r *StaticToolRuntime) DeferredTools() []models.Tool {
	if r == nil {
		return nil
	}
	return append([]models.Tool(nil), r.deferredTools...)
}

func (r *StaticToolRuntime) Subagents() *subagent.Pool {
	if r == nil {
		return nil
	}
	return r.subagents
}
