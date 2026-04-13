package harnessruntime

import (
	"context"
	"net"
	"net/http"
)

type RuntimeStateLaunchSpec struct {
	Addr string
}

type RuntimeStateLauncher struct {
	node  *HTTPRemoteStateNode
	state RuntimeStatePlane
}

func NewRuntimeStateLauncher(node *HTTPRemoteStateNode, state RuntimeStatePlane) *RuntimeStateLauncher {
	return &RuntimeStateLauncher{node: node, state: state}
}

func (l *RuntimeStateLauncher) Node() *HTTPRemoteStateNode {
	if l == nil {
		return nil
	}
	return l.node
}

func (l *RuntimeStateLauncher) State() RuntimeStatePlane {
	if l == nil {
		return RuntimeStatePlane{}
	}
	return l.state
}

func (l *RuntimeStateLauncher) Spec() RuntimeStateLaunchSpec {
	if l == nil || l.node == nil {
		return RuntimeStateLaunchSpec{}
	}
	return RuntimeStateLaunchSpec{Addr: l.node.Addr()}
}

func (l *RuntimeStateLauncher) Handler() http.Handler {
	if l == nil || l.node == nil {
		return nil
	}
	return l.node.Handler()
}

func (l *RuntimeStateLauncher) Start() error {
	if l == nil || l.node == nil {
		return nil
	}
	return l.node.Start()
}

func (l *RuntimeStateLauncher) Serve(listener net.Listener) error {
	if l == nil || l.node == nil {
		return nil
	}
	return l.node.Serve(listener)
}

func (l *RuntimeStateLauncher) Close(ctx context.Context) error {
	if l == nil || l.node == nil {
		return nil
	}
	return l.node.Shutdown(ctx)
}
