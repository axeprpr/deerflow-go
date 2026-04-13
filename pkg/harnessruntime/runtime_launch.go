package harnessruntime

import (
	"context"
	"net"
	"net/http"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type RuntimeNodeLaunchSpec struct {
	Role                RuntimeNodeRole
	ServesRemoteWorker  bool
	RemoteWorkerAddr    string
	ServesRemoteSandbox bool
	RemoteSandboxAddr   string
	ServesRemoteState   bool
	RemoteStateAddr     string
}

type RuntimeNodeLauncher struct {
	node *RuntimeNode
}

func NewRuntimeNodeLauncher(node *RuntimeNode) *RuntimeNodeLauncher {
	return &RuntimeNodeLauncher{node: node}
}

func BuildAllInOneRuntimeNodeLauncher(name, root string, source func() *harness.Runtime, specs WorkerSpecRuntime) (*RuntimeNodeLauncher, error) {
	node, err := BuildAllInOneRuntimeNode(name, root, source, specs)
	if err != nil {
		return nil, err
	}
	return NewRuntimeNodeLauncher(node), nil
}

func BuildGatewayRuntimeNodeLauncher(name, root, endpoint string, source func() *harness.Runtime, specs WorkerSpecRuntime) (*RuntimeNodeLauncher, error) {
	node, err := BuildGatewayRuntimeNode(name, root, endpoint, source, specs)
	if err != nil {
		return nil, err
	}
	return NewRuntimeNodeLauncher(node), nil
}

func BuildWorkerRuntimeNodeLauncher(name, root string, source func() *harness.Runtime, specs WorkerSpecRuntime) (*RuntimeNodeLauncher, error) {
	node, err := BuildWorkerRuntimeNode(name, root, source, specs)
	if err != nil {
		return nil, err
	}
	return NewRuntimeNodeLauncher(node), nil
}

func (n *RuntimeNode) LaunchSpec() RuntimeNodeLaunchSpec {
	if n == nil {
		return RuntimeNodeLaunchSpec{}
	}
	spec := RuntimeNodeLaunchSpec{
		Role: n.Config.Role,
	}
	if n.RemoteWorker != nil {
		spec.ServesRemoteWorker = true
		spec.RemoteWorkerAddr = n.RemoteWorker.Addr()
	}
	if n.RemoteSandbox != nil {
		spec.ServesRemoteSandbox = true
		spec.RemoteSandboxAddr = spec.RemoteWorkerAddr
	}
	if n.RemoteState != nil {
		spec.ServesRemoteState = true
		spec.RemoteStateAddr = spec.RemoteWorkerAddr
	}
	return spec
}

func (l *RuntimeNodeLauncher) Node() *RuntimeNode {
	if l == nil {
		return nil
	}
	return l.node
}

func (l *RuntimeNodeLauncher) Spec() RuntimeNodeLaunchSpec {
	if l == nil {
		return RuntimeNodeLaunchSpec{}
	}
	return l.node.LaunchSpec()
}

func (l *RuntimeNodeLauncher) Handler() http.Handler {
	if l == nil || l.node == nil || l.node.RemoteWorker == nil {
		return nil
	}
	return l.node.RemoteWorker.Handler()
}

func (l *RuntimeNodeLauncher) Start() error {
	if l == nil || l.node == nil {
		return nil
	}
	return l.node.Start()
}

func (l *RuntimeNodeLauncher) Serve(listener net.Listener) error {
	if l == nil || l.node == nil {
		return nil
	}
	return l.node.Serve(listener)
}

func (l *RuntimeNodeLauncher) Close(ctx context.Context) error {
	if l == nil || l.node == nil {
		return nil
	}
	return l.node.Close(ctx)
}
