package langgraphcompat

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestDefaultGatewayMCPConnectorSupportsSSE(t *testing.T) {
	restore := stubGatewayMCPConnectors(t)
	defer restore()

	called := false
	gatewayMCPConnectSSE = func(ctx context.Context, name, baseURL string, headers map[string]string) (gatewayMCPClient, error) {
		called = true
		if name != "demo" || baseURL != "https://example.com/sse" {
			t.Fatalf("name=%q baseURL=%q", name, baseURL)
		}
		if headers["X-Test"] != "sse-token" {
			t.Fatalf("headers=%#v", headers)
		}
		return &fakeGatewayMCPClientAdapter{}, nil
	}

	client, err := defaultGatewayMCPConnector(context.Background(), "demo", gatewayMCPServerConfig{
		Type:    "sse",
		URL:     "https://example.com/sse",
		Headers: map[string]string{"X-Test": "sse-token"},
	})
	if err != nil {
		t.Fatalf("connect sse: %v", err)
	}
	if !called {
		t.Fatal("expected sse connector to be used")
	}
	if client == nil {
		t.Fatal("expected client")
	}
}

func TestDefaultGatewayMCPConnectorSupportsHTTP(t *testing.T) {
	restore := stubGatewayMCPConnectors(t)
	defer restore()

	called := false
	gatewayMCPConnectHTTP = func(ctx context.Context, name, baseURL string, headers map[string]string) (gatewayMCPClient, error) {
		called = true
		if name != "demo" || baseURL != "https://example.com/mcp" {
			t.Fatalf("name=%q baseURL=%q", name, baseURL)
		}
		if headers["X-Test"] != "http-token" {
			t.Fatalf("headers=%#v", headers)
		}
		return &fakeGatewayMCPClientAdapter{}, nil
	}

	client, err := defaultGatewayMCPConnector(context.Background(), "demo", gatewayMCPServerConfig{
		Type:    "http",
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"X-Test": "http-token"},
	})
	if err != nil {
		t.Fatalf("connect http: %v", err)
	}
	if !called {
		t.Fatal("expected http connector to be used")
	}
	if client == nil {
		t.Fatal("expected client")
	}
}

func TestDefaultGatewayMCPConnectorSupportsStreamableHTTPAlias(t *testing.T) {
	restore := stubGatewayMCPConnectors(t)
	defer restore()

	called := false
	gatewayMCPConnectHTTP = func(ctx context.Context, name, baseURL string, headers map[string]string) (gatewayMCPClient, error) {
		called = true
		return &fakeGatewayMCPClientAdapter{}, nil
	}

	client, err := defaultGatewayMCPConnector(context.Background(), "demo", gatewayMCPServerConfig{
		Type: "streamable_http",
		URL:  "https://example.com/mcp",
	})
	if err != nil {
		t.Fatalf("connect streamable_http: %v", err)
	}
	if !called {
		t.Fatal("expected http connector to be used")
	}
	if client == nil {
		t.Fatal("expected client")
	}
}

func stubGatewayMCPConnectors(t *testing.T) func() {
	t.Helper()
	prevStdio := gatewayMCPConnectStdio
	prevSSE := gatewayMCPConnectSSE
	prevHTTP := gatewayMCPConnectHTTP
	gatewayMCPConnectStdio = func(ctx context.Context, name, command string, env []string, args ...string) (gatewayMCPClient, error) {
		return &fakeGatewayMCPClientAdapter{}, nil
	}
	gatewayMCPConnectSSE = func(ctx context.Context, name, baseURL string, headers map[string]string) (gatewayMCPClient, error) {
		return &fakeGatewayMCPClientAdapter{}, nil
	}
	gatewayMCPConnectHTTP = func(ctx context.Context, name, baseURL string, headers map[string]string) (gatewayMCPClient, error) {
		return &fakeGatewayMCPClientAdapter{}, nil
	}
	return func() {
		gatewayMCPConnectStdio = prevStdio
		gatewayMCPConnectSSE = prevSSE
		gatewayMCPConnectHTTP = prevHTTP
	}
}

type fakeGatewayMCPClientAdapter struct{}

func (f *fakeGatewayMCPClientAdapter) Tools(ctx context.Context) ([]models.Tool, error) {
	return nil, nil
}

func (f *fakeGatewayMCPClientAdapter) Close() error {
	return nil
}
