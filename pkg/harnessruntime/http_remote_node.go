package harnessruntime

import (
	"context"
	"errors"
	"net"
	"net/http"
)

type HTTPRemoteWorkerNode struct {
	server *http.Server
}

func NewHTTPRemoteWorkerNode(server *http.Server) *HTTPRemoteWorkerNode {
	return &HTTPRemoteWorkerNode{server: server}
}

func (n *HTTPRemoteWorkerNode) Server() *http.Server {
	if n == nil {
		return nil
	}
	return n.server
}

func (n *HTTPRemoteWorkerNode) Addr() string {
	if n == nil || n.server == nil {
		return ""
	}
	return n.server.Addr
}

func (n *HTTPRemoteWorkerNode) Start() error {
	if n == nil || n.server == nil {
		return errors.New("remote worker server is not configured")
	}
	return n.server.ListenAndServe()
}

func (n *HTTPRemoteWorkerNode) Serve(listener net.Listener) error {
	if n == nil || n.server == nil {
		return errors.New("remote worker server is not configured")
	}
	if listener == nil {
		return errors.New("remote worker listener is not configured")
	}
	return n.server.Serve(listener)
}

func (n *HTTPRemoteWorkerNode) Shutdown(ctx context.Context) error {
	if n == nil || n.server == nil {
		return nil
	}
	return n.server.Shutdown(ctx)
}
