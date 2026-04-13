package harness

import (
	"context"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type SummarizationCompaction struct {
	Summary  string
	Messages []models.Message
	Changed  bool
}

type SummarizationLifecycleConfig struct {
	SummaryMetadataKey string
	Compact            func(context.Context, *RunState) (SummarizationCompaction, error)
	Persist            func(threadID string, summary string)
}

type MemoryLifecycleConfig struct {
	Runtime        *MemoryRuntime
	SessionKey     string
	ResolveSession func(*RunState) string
	PlanScopes     func(*RunState) MemoryScopePlan
}

type ClarificationLifecycleConfig struct {
	InterruptMetadataKey string
}

type TitleLifecycleConfig struct {
	TitleMetadataKey string
	Generate         func(context.Context, *RunState, *agent.RunResult) string
}

type MemorySessionResolver interface {
	ResolveMemorySession(*RunState) string
}

type MemoryScopeResolver interface {
	ResolveMemoryScope(*RunState) pkgmemory.Scope
}

type MemoryScopePlanner interface {
	PlanMemoryScopes(*RunState) MemoryScopePlan
}

type Summarizer interface {
	Compact(context.Context, *RunState) (SummarizationCompaction, error)
	PersistSummary(threadID string, summary string)
}

type TitleGenerator interface {
	GenerateTitle(context.Context, *RunState, *agent.RunResult) string
}

func MergeLifecycleHooks(items ...*LifecycleHooks) *LifecycleHooks {
	merged := &LifecycleHooks{}
	for _, item := range items {
		if item == nil {
			continue
		}
		merged.BeforeRun = append(merged.BeforeRun, item.BeforeRun...)
		merged.AfterRun = append(merged.AfterRun, item.AfterRun...)
	}
	if len(merged.BeforeRun) == 0 && len(merged.AfterRun) == 0 {
		return nil
	}
	return merged
}

func SummarizationLifecycleHooks(cfg SummarizationLifecycleConfig) *LifecycleHooks {
	key := strings.TrimSpace(cfg.SummaryMetadataKey)
	if key == "" || cfg.Compact == nil {
		return nil
	}
	return &LifecycleHooks{
		BeforeRun: []BeforeRunHook{
			func(ctx context.Context, state *RunState) error {
				if state == nil {
					return nil
				}
				compacted, err := cfg.Compact(ctx, state)
				if err != nil {
					return err
				}
				state.Messages = append([]models.Message(nil), compacted.Messages...)
				if compacted.Changed {
					if summary := strings.TrimSpace(compacted.Summary); summary != "" {
						if state.Metadata == nil {
							state.Metadata = map[string]any{}
						}
						state.Metadata[key] = summary
					}
				}
				return nil
			},
		},
		AfterRun: []AfterRunHook{
			func(_ context.Context, state *RunState, _ *agent.RunResult) error {
				if state == nil || cfg.Persist == nil {
					return nil
				}
				summary, _ := state.Metadata[key].(string)
				summary = strings.TrimSpace(summary)
				if summary == "" {
					return nil
				}
				cfg.Persist(state.ThreadID, summary)
				return nil
			},
		},
	}
}

func MemoryLifecycleHooks(cfg MemoryLifecycleConfig) *LifecycleHooks {
	if cfg.Runtime == nil || !cfg.Runtime.Enabled() || cfg.ResolveSession == nil {
		return nil
	}
	key := strings.TrimSpace(cfg.SessionKey)
	if key == "" {
		key = "memory_session_id"
	}
	return &LifecycleHooks{
		BeforeRun: []BeforeRunHook{
			func(ctx context.Context, state *RunState) error {
				if state == nil {
					return nil
				}
				plan := memoryLifecyclePlan(cfg, state)
				if plan.Primary.Key() == "" {
					return nil
				}
				if state.Metadata == nil {
					state.Metadata = map[string]any{}
				}
				state.Metadata[key] = plan.Primary.Key()
				injected := strings.TrimSpace(injectMemoryScopes(ctx, cfg.Runtime, plan.Inject, state.Spec.SystemPrompt))
				if injected == "" {
					return nil
				}
				if prompt := strings.TrimSpace(state.Spec.SystemPrompt); prompt != "" {
					state.Spec.SystemPrompt = strings.TrimSpace(prompt + "\n\n" + injected)
					return nil
				}
				state.Spec.SystemPrompt = injected
				return nil
			},
		},
		AfterRun: []AfterRunHook{
			func(_ context.Context, state *RunState, result *agent.RunResult) error {
				if state == nil || result == nil || len(result.Messages) == 0 {
					return nil
				}
				plan := memoryLifecyclePlan(cfg, state)
				if plan.Primary.Key() == "" || len(plan.Update) == 0 {
					return nil
				}
				state.Metadata[key] = plan.Primary.Key()
				for _, scope := range plan.Update {
					cfg.Runtime.ScheduleScopeUpdate(scope, result.Messages)
				}
				return nil
			},
		},
	}
}

func MemoryLifecycleHooksWithResolver(runtime *MemoryRuntime, resolver MemorySessionResolver, sessionKey string) *LifecycleHooks {
	if resolver == nil {
		return nil
	}
	return MemoryLifecycleHooks(MemoryLifecycleConfig{
		Runtime:    runtime,
		SessionKey: sessionKey,
		ResolveSession: func(state *RunState) string {
			return resolver.ResolveMemorySession(state)
		},
	})
}

func MemoryLifecycleHooksWithScopeResolver(runtime *MemoryRuntime, resolver MemoryScopeResolver, sessionKey string) *LifecycleHooks {
	if resolver == nil {
		return nil
	}
	return MemoryLifecycleHooksWithScopePlanner(runtime, memoryPlannerFunc(func(state *RunState) MemoryScopePlan {
		scope := resolver.ResolveMemoryScope(state)
		return NormalizeMemoryScopePlan(MemoryScopePlan{
			Primary: scope,
			Inject:  []pkgmemory.Scope{scope},
			Update:  []pkgmemory.Scope{scope},
		})
	}), sessionKey)
}

type memoryPlannerFunc func(*RunState) MemoryScopePlan

func (f memoryPlannerFunc) PlanMemoryScopes(state *RunState) MemoryScopePlan {
	return f(state)
}

func MemoryLifecycleHooksWithScopePlanner(runtime *MemoryRuntime, planner MemoryScopePlanner, sessionKey string) *LifecycleHooks {
	if planner == nil {
		return nil
	}
	return MemoryLifecycleHooks(MemoryLifecycleConfig{
		Runtime:    runtime,
		SessionKey: sessionKey,
		PlanScopes: func(state *RunState) MemoryScopePlan { return planner.PlanMemoryScopes(state) },
	})
}

func memoryLifecyclePlan(cfg MemoryLifecycleConfig, state *RunState) MemoryScopePlan {
	if cfg.PlanScopes != nil {
		return NormalizeMemoryScopePlan(cfg.PlanScopes(state))
	}
	if cfg.ResolveSession == nil {
		return MemoryScopePlan{}
	}
	sessionID := strings.TrimSpace(cfg.ResolveSession(state))
	if sessionID == "" {
		return MemoryScopePlan{}
	}
	scope := pkgmemory.ParseScopeKey(sessionID)
	return NormalizeMemoryScopePlan(MemoryScopePlan{
		Primary: scope,
		Inject:  []pkgmemory.Scope{scope},
		Update:  []pkgmemory.Scope{scope},
	})
}

func injectMemoryScopes(ctx context.Context, runtime *MemoryRuntime, scopes []pkgmemory.Scope, currentPrompt string) string {
	if runtime == nil || !runtime.Enabled() || len(scopes) == 0 {
		return ""
	}
	sections := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		injected := strings.TrimSpace(runtime.InjectScope(ctx, scope, currentPrompt))
		if injected == "" {
			continue
		}
		sections = append(sections, injected)
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func ClarificationInterruptFromMessages(messages []models.Message) map[string]any {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != models.RoleTool || msg.ToolResult == nil {
			continue
		}
		if strings.TrimSpace(msg.ToolResult.ToolName) != "ask_clarification" || msg.ToolResult.Status != models.CallStatusCompleted {
			continue
		}
		value := strings.TrimSpace(firstNonEmpty(msg.Content, msg.ToolResult.Content))
		if value == "" {
			value = "Clarification requested"
		}
		interrupt := map[string]any{"value": value}
		if len(msg.ToolResult.Data) > 0 {
			if id := stringValue(msg.ToolResult.Data["id"]); id != "" {
				interrupt["id"] = id
			}
			if question := stringValue(msg.ToolResult.Data["question"]); question != "" {
				interrupt["question"] = question
			}
			if clarificationType := stringValue(msg.ToolResult.Data["clarification_type"]); clarificationType != "" {
				interrupt["clarification_type"] = clarificationType
			}
		}
		return interrupt
	}
	return nil
}

func ClarificationLifecycleHooks(cfg ClarificationLifecycleConfig) *LifecycleHooks {
	key := strings.TrimSpace(cfg.InterruptMetadataKey)
	if key == "" {
		key = "clarification_interrupt"
	}
	return &LifecycleHooks{
		AfterRun: []AfterRunHook{
			func(_ context.Context, state *RunState, result *agent.RunResult) error {
				if state == nil || result == nil {
					return nil
				}
				interrupt := ClarificationInterruptFromMessages(result.Messages)
				if interrupt == nil {
					return nil
				}
				if state.Metadata == nil {
					state.Metadata = map[string]any{}
				}
				state.Metadata[key] = interrupt
				return nil
			},
		},
	}
}

func TitleLifecycleHooks(cfg TitleLifecycleConfig) *LifecycleHooks {
	key := strings.TrimSpace(cfg.TitleMetadataKey)
	if key == "" || cfg.Generate == nil {
		return nil
	}
	return &LifecycleHooks{
		AfterRun: []AfterRunHook{
			func(ctx context.Context, state *RunState, result *agent.RunResult) error {
				if state == nil || result == nil {
					return nil
				}
				title := strings.TrimSpace(cfg.Generate(ctx, state, result))
				if title == "" {
					return nil
				}
				if state.Metadata == nil {
					state.Metadata = map[string]any{}
				}
				state.Metadata[key] = title
				return nil
			},
		},
	}
}

func SummarizationLifecycleHooksWithSummarizer(summarizer Summarizer, summaryMetadataKey string) *LifecycleHooks {
	if summarizer == nil {
		return nil
	}
	return SummarizationLifecycleHooks(SummarizationLifecycleConfig{
		SummaryMetadataKey: summaryMetadataKey,
		Compact: func(ctx context.Context, state *RunState) (SummarizationCompaction, error) {
			return summarizer.Compact(ctx, state)
		},
		Persist: func(threadID string, summary string) {
			summarizer.PersistSummary(threadID, summary)
		},
	})
}

func TitleLifecycleHooksWithGenerator(generator TitleGenerator, titleMetadataKey string) *LifecycleHooks {
	if generator == nil {
		return nil
	}
	return TitleLifecycleHooks(TitleLifecycleConfig{
		TitleMetadataKey: titleMetadataKey,
		Generate: func(ctx context.Context, state *RunState, result *agent.RunResult) string {
			return generator.GenerateTitle(ctx, state, result)
		},
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func stringValue(raw any) string {
	if value, ok := raw.(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}
