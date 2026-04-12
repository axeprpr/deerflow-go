package harnessruntime

import "github.com/axeprpr/deerflow-go/pkg/agent"

func NewDefaultRunPolicy() *agent.RunPolicy {
	return agent.DefaultRunPolicy()
}
