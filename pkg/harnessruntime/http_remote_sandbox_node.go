package harnessruntime

import (
	"context"
	"errors"
	"net"
	"net/http"
)

type HTTPRemoteSandboxNode struct {
	server *http.Server
}

func NewHTTPRemoteSandboxNode(server *http.Server) *HTTPRemoteSandboxNode {
	return &HTTPRemoteSandboxNode{server: server}
}

func (n *HTTPRemoteSandboxNode) Server() *http.Server {
	if n == nil {
		return nil
	}
	return n.server
}

func (n *HTTPRemoteSandboxNode) Addr() string {
	if n == nil || n.server == nil {
		return ""
	}
	return n.server.Addr
}

func (n *HTTPRemoteSandboxNode) Handler() http.Handler {
	if n == nil || n.server == nil {
		return nil
	}
	return n.server.Handler
}

func (n *HTTPRemoteSandboxNode) Start() error {
	if n == nil || n.server == nil {
		return errors.New("remote sandbox server is not configured")
	}
	return n.server.ListenAndServe()
}

func (n *HTTPRemoteSandboxNode) Serve(listener net.Listener) error {
	if n == nil || n.server == nil {
		return errors.New("remote sandbox server is not configured")
	}
	if listener == nil {
		return errors.New("remote sandbox listener is not configured")
	}
	return n.server.Serve(listener)
}

func (n *HTTPRemoteSandboxNode) Shutdown(ctx context.Context) error {
	if n == nil || n.server == nil {
		return nil
	}
	return n.server.Shutdown(ctx)
}
