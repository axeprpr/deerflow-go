package harnessruntime

import (
	"errors"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type ExecutionHandle interface {
	Resolve() (*harness.Execution, error)
	Describe() ExecutionDescriptor
}

type ExecutionDescriptor struct {
	Kind      string
	SessionID string
}

const ExecutionKindLocalPrepared = "local-prepared"
const ExecutionKindRemoteCompleted = "remote-completed"

type staticExecutionHandle struct {
	execution *harness.Execution
	desc      ExecutionDescriptor
}

func NewStaticExecutionHandle(execution *harness.Execution, sessionID string) ExecutionHandle {
	return staticExecutionHandle{
		execution: execution,
		desc: ExecutionDescriptor{
			Kind:      ExecutionKindLocalPrepared,
			SessionID: sessionID,
		},
	}
}

func (h staticExecutionHandle) Resolve() (*harness.Execution, error) {
	if h.execution == nil {
		return nil, errors.New("execution is unavailable")
	}
	return h.execution, nil
}

func (h staticExecutionHandle) Describe() ExecutionDescriptor {
	return h.desc
}
