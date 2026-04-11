package harnessruntime

import (
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type FeatureConfig struct {
	ClarificationEnabled bool
	MemoryEnabled        bool
	SummarizationEnabled bool
	TitleEnabled         bool
}

func (c FeatureConfig) BuildAssembly(manager *clarification.Manager) harness.FeatureAssembly {
	return harness.FeatureAssembly{
		Clarification: harness.ClarificationFeature{
			Enabled: c.ClarificationEnabled && manager != nil,
			Manager: manager,
		},
		Memory: harness.MemoryFeature{
			Enabled: c.MemoryEnabled,
		},
		Summarization: harness.SummarizationFeature{
			Enabled: c.SummarizationEnabled,
		},
		Title: harness.TitleFeature{
			Enabled: c.TitleEnabled,
		},
	}
}
