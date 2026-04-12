package harnessruntime

import (
	"errors"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type ExecutionHandle interface {
	Resolve() (*harness.Execution, error)
}

type staticExecutionHandle struct {
	execution *harness.Execution
}

func NewStaticExecutionHandle(execution *harness.Execution) ExecutionHandle {
	return staticExecutionHandle{execution: execution}
}

func (h staticExecutionHandle) Resolve() (*harness.Execution, error) {
	if h.execution == nil {
		return nil, errors.New("execution is unavailable")
	}
	return h.execution, nil
}
