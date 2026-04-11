package harness

import "github.com/axeprpr/deerflow-go/pkg/clarification"

type ClarificationFeature struct {
	Enabled bool
	Manager *clarification.Manager
}

type MemoryFeature struct {
	Enabled bool
}

type SummarizationFeature struct {
	Enabled bool
}

type TitleFeature struct {
	Enabled bool
}

// FeatureAssembly describes which upstream-style runtime features are wired
// into one harness runtime instance.
type FeatureAssembly struct {
	Clarification ClarificationFeature
	Memory        MemoryFeature
	Summarization SummarizationFeature
	Title         TitleFeature
}
