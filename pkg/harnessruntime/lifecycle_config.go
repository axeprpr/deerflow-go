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
	MemoryResolver harness.MemorySessionResolver
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

	return harness.MergeLifecycleHooks(
		harness.SummarizationLifecycleHooksWithSummarizer(providers.Summarizer, c.SummaryMetadataKey),
		harness.MemoryLifecycleHooksWithResolver(providers.MemoryRuntime, providers.MemoryResolver, c.MemorySessionKey),
		harness.ClarificationLifecycleHooks(harness.ClarificationLifecycleConfig{
			InterruptMetadataKey: c.InterruptMetadataKey,
		}),
		titleHooks,
	)
}
