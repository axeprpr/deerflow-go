package harnessruntime

import "context"

type RuntimeNode struct {
	Config       RuntimeNodeConfig
	State        RuntimeStatePlane
	Dispatcher   RunDispatcher
	Sandbox      *SandboxResourceManager
	RemoteWorker *HTTPRemoteWorkerNode
}

func (c RuntimeNodeConfig) BuildRuntimeNode(runtime DispatchRuntimeConfig) (*RuntimeNode, error) {
	sandboxManager, err := c.BuildSandboxManager()
	if err != nil {
		return nil, err
	}
	return &RuntimeNode{
		Config:       c,
		State:        c.BuildStatePlane(),
		Dispatcher:   c.BuildDispatcher(runtime),
		Sandbox:      sandboxManager,
		RemoteWorker: c.BuildRemoteWorkerNode(runtime),
	}, nil
}

func (n *RuntimeNode) Close(ctx context.Context) error {
	if n == nil {
		return nil
	}
	var closeErr error
	if closer, ok := n.Dispatcher.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	if n.RemoteWorker != nil {
		if err := n.RemoteWorker.Shutdown(ctx); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	if n.Sandbox != nil {
		if err := n.Sandbox.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}
