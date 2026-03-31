package langgraphcompat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestDefaultGatewayMCPConnectorSupportsSSE(t *testing.T) {
	restore := stubGatewayMCPConnectors(t)
	defer restore()

	called := false
	gatewayMCPConnectSSE = func(ctx context.Context, name, baseURL string, headers map[string]string, headerFunc func(context.Context) map[string]string) (gatewayMCPClient, error) {
		called = true
		if name != "demo" || baseURL != "https://example.com/sse" {
			t.Fatalf("name=%q baseURL=%q", name, baseURL)
		}
		if headers["X-Test"] != "sse-token" {
			t.Fatalf("headers=%#v", headers)
		}
		if headerFunc != nil {
			t.Fatal("unexpected dynamic header func")
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
	gatewayMCPConnectHTTP = func(ctx context.Context, name, baseURL string, headers map[string]string, headerFunc func(context.Context) map[string]string) (gatewayMCPClient, error) {
		called = true
		if name != "demo" || baseURL != "https://example.com/mcp" {
			t.Fatalf("name=%q baseURL=%q", name, baseURL)
		}
		if headers["X-Test"] != "http-token" {
			t.Fatalf("headers=%#v", headers)
		}
		if headerFunc != nil {
			t.Fatal("unexpected dynamic header func")
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
	gatewayMCPConnectHTTP = func(ctx context.Context, name, baseURL string, headers map[string]string, headerFunc func(context.Context) map[string]string) (gatewayMCPClient, error) {
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
	gatewayMCPConnectSSE = func(ctx context.Context, name, baseURL string, headers map[string]string, headerFunc func(context.Context) map[string]string) (gatewayMCPClient, error) {
		return &fakeGatewayMCPClientAdapter{}, nil
	}
	gatewayMCPConnectHTTP = func(ctx context.Context, name, baseURL string, headers map[string]string, headerFunc func(context.Context) map[string]string) (gatewayMCPClient, error) {
		return &fakeGatewayMCPClientAdapter{}, nil
	}
	return func() {
		gatewayMCPConnectStdio = prevStdio
		gatewayMCPConnectSSE = prevSSE
		gatewayMCPConnectHTTP = prevHTTP
	}
}

func TestDefaultGatewayMCPConnectorInjectsOAuthHeaders(t *testing.T) {
	restore := stubGatewayMCPConnectors(t)
	defer restore()

	prevClientFactory := gatewayMCPOAuthHTTPClient
	defer func() { gatewayMCPOAuthHTTPClient = prevClientFactory }()

	tokenCalls := 0
	gatewayMCPOAuthHTTPClient = func() *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			tokenCalls++
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "client_credentials" {
				t.Fatalf("grant_type=%q", got)
			}
			if got := r.Form.Get("client_id"); got != "demo-client" {
				t.Fatalf("client_id=%q", got)
			}
			if got := r.Form.Get("resource"); got != "github" {
				t.Fatalf("resource=%q", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"access_token":"token-1","token_type":"Bearer","expires_in":3600}`)),
			}, nil
		})}
	}

	gatewayMCPConnectHTTP = func(ctx context.Context, name, baseURL string, headers map[string]string, headerFunc func(context.Context) map[string]string) (gatewayMCPClient, error) {
		if got := headers["Authorization"]; got != "Bearer token-1" {
			t.Fatalf("authorization=%q", got)
		}
		if got := headers["X-Test"]; got != "1" {
			t.Fatalf("headers=%#v", headers)
		}
		if headerFunc == nil {
			t.Fatal("expected dynamic header func")
		}
		dynamic := headerFunc(context.Background())
		if got := dynamic["Authorization"]; got != "Bearer token-1" {
			t.Fatalf("dynamic authorization=%q", got)
		}
		return &fakeGatewayMCPClientAdapter{}, nil
	}

	client, err := defaultGatewayMCPConnector(context.Background(), "demo", gatewayMCPServerConfig{
		Type:    "http",
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"X-Test": "1"},
		OAuth: &gatewayMCPOAuthConfig{
			Enabled:          true,
			TokenURL:         "https://auth.example.com/token",
			GrantType:        "client_credentials",
			ClientID:         "demo-client",
			ClientSecret:     "demo-secret",
			ExtraTokenParams: map[string]string{"resource": "github"},
		},
	})
	if err != nil {
		t.Fatalf("connect http oauth: %v", err)
	}
	if client == nil {
		t.Fatal("expected client")
	}
	if tokenCalls != 1 {
		t.Fatalf("tokenCalls=%d want=1", tokenCalls)
	}
}

func TestGatewayMCPOAuthProviderRefreshTokenGrant(t *testing.T) {
	prevClientFactory := gatewayMCPOAuthHTTPClient
	defer func() { gatewayMCPOAuthHTTPClient = prevClientFactory }()
	gatewayMCPOAuthHTTPClient = func() *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "refresh_token" {
				t.Fatalf("grant_type=%q", got)
			}
			if got := r.Form.Get("refresh_token"); got != "seed-refresh-token" {
				t.Fatalf("refresh_token=%q", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"access_token":"fresh-token","token_type":"bearer","expires_in":"120","refresh_token":"next-refresh-token"}`)),
			}, nil
		})}
	}

	provider, err := newGatewayMCPOAuthProvider(&gatewayMCPOAuthConfig{
		Enabled:      true,
		TokenURL:     "https://auth.example.com/token",
		GrantType:    "refresh_token",
		RefreshToken: "seed-refresh-token",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	header, err := provider.HeaderValue(context.Background())
	if err != nil {
		t.Fatalf("header value: %v", err)
	}
	if header != "bearer fresh-token" {
		t.Fatalf("header=%q", header)
	}
	if provider.refreshToken != "next-refresh-token" {
		t.Fatalf("refreshToken=%q", provider.refreshToken)
	}
}

func TestGatewayMCPOAuthProviderReturnsErrorForInvalidConfig(t *testing.T) {
	_, err := newGatewayMCPOAuthProvider(&gatewayMCPOAuthConfig{
		Enabled: true,
	})
	if err == nil || !strings.Contains(err.Error(), "token_url") {
		t.Fatalf("err=%v", err)
	}
}

func TestParsePositiveInt(t *testing.T) {
	value, ok := parsePositiveInt(json.Number("42"))
	if !ok || value != 42 {
		t.Fatalf("value=%d ok=%v", value, ok)
	}
	if _, ok := parsePositiveInt("abc"); ok {
		t.Fatal("expected invalid string to fail")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

type fakeGatewayMCPClientAdapter struct{}

func (f *fakeGatewayMCPClientAdapter) Tools(ctx context.Context) ([]models.Tool, error) {
	return nil, nil
}

func (f *fakeGatewayMCPClientAdapter) Close() error {
	return nil
}
