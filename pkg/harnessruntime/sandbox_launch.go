package harnessruntime

import (
	"context"
	"net"
	"net/http"
)

type RuntimeSandboxLaunchSpec struct {
	Addr string
}

type RuntimeSandboxLauncher struct {
	node *HTTPRemoteSandboxNode
}

func NewRuntimeSandboxLauncher(node *HTTPRemoteSandboxNode) *RuntimeSandboxLauncher {
	return &RuntimeSandboxLauncher{node: node}
}

func (l *RuntimeSandboxLauncher) Node() *HTTPRemoteSandboxNode {
	if l == nil {
		return nil
	}
	return l.node
}

func (l *RuntimeSandboxLauncher) Spec() RuntimeSandboxLaunchSpec {
	if l == nil || l.node == nil {
		return RuntimeSandboxLaunchSpec{}
	}
	return RuntimeSandboxLaunchSpec{Addr: l.node.Addr()}
}

func (l *RuntimeSandboxLauncher) Handler() http.Handler {
	if l == nil || l.node == nil {
		return nil
	}
	return l.node.Handler()
}

func (l *RuntimeSandboxLauncher) Start() error {
	if l == nil || l.node == nil {
		return nil
	}
	return l.node.Start()
}

func (l *RuntimeSandboxLauncher) Serve(listener net.Listener) error {
	if l == nil || l.node == nil {
		return nil
	}
	return l.node.Serve(listener)
}

func (l *RuntimeSandboxLauncher) Close(ctx context.Context) error {
	if l == nil || l.node == nil {
		return nil
	}
	return l.node.Shutdown(ctx)
}
