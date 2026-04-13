package harnessruntime

import "github.com/axeprpr/deerflow-go/pkg/harness"

type LifecycleConfig struct {
	SummaryMetadataKey   string
	MemorySessionKey     string
	InterruptMetadataKey string
	TitleMetadataKey     string
}

type LifecycleProviders struct {
	MemoryRuntime  *harness.MemoryRuntime
	Summarizer     harness.Summarizer
	MemoryResolver harness.MemoryScopeResolver
	MemoryPlanner  harness.MemoryScopePlanner
	TitleGenerator harness.TitleGenerator
}

func (c LifecycleConfig) BuildHooks(features harness.FeatureAssembly, providers LifecycleProviders) *harness.LifecycleHooks {
	titleMetadataKey := c.TitleMetadataKey
	if titleMetadataKey == "" {
		titleMetadataKey = "generated_title"
	}

	var titleHooks *harness.LifecycleHooks
	if features.Title.Enabled {
		titleHooks = harness.TitleLifecycleHooksWithGenerator(providers.TitleGenerator, titleMetadataKey)
	}

	var memoryHooks *harness.LifecycleHooks
	switch {
	case providers.MemoryPlanner != nil:
		memoryHooks = harness.MemoryLifecycleHooksWithScopePlanner(providers.MemoryRuntime, providers.MemoryPlanner, c.MemorySessionKey)
	case providers.MemoryResolver != nil:
		memoryHooks = harness.MemoryLifecycleHooksWithScopeResolver(providers.MemoryRuntime, providers.MemoryResolver, c.MemorySessionKey)
	}

	return harness.MergeLifecycleHooks(
		harness.SummarizationLifecycleHooksWithSummarizer(providers.Summarizer, c.SummaryMetadataKey),
		memoryHooks,
		harness.ClarificationLifecycleHooks(harness.ClarificationLifecycleConfig{
			InterruptMetadataKey: c.InterruptMetadataKey,
		}),
		titleHooks,
	)
}
