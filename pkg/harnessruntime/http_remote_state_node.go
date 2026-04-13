package harnessruntime

import (
	"context"
	"errors"
	"net"
	"net/http"
)

type HTTPRemoteStateNode struct {
	server *http.Server
}

func NewHTTPRemoteStateNode(server *http.Server) *HTTPRemoteStateNode {
	return &HTTPRemoteStateNode{server: server}
}

func (n *HTTPRemoteStateNode) Server() *http.Server {
	if n == nil {
		return nil
	}
	return n.server
}

func (n *HTTPRemoteStateNode) Addr() string {
	if n == nil || n.server == nil {
		return ""
	}
	return n.server.Addr
}

func (n *HTTPRemoteStateNode) Handler() http.Handler {
	if n == nil || n.server == nil {
		return nil
	}
	return n.server.Handler
}

func (n *HTTPRemoteStateNode) Start() error {
	if n == nil || n.server == nil {
		return errors.New("remote state server is not configured")
	}
	return n.server.ListenAndServe()
}

func (n *HTTPRemoteStateNode) Serve(listener net.Listener) error {
	if n == nil || n.server == nil {
		return errors.New("remote state server is not configured")
	}
	if listener == nil {
		return errors.New("remote state listener is not configured")
	}
	return n.server.Serve(listener)
}

func (n *HTTPRemoteStateNode) Shutdown(ctx context.Context) error {
	if n == nil || n.server == nil {
		return nil
	}
	return n.server.Shutdown(ctx)
}
