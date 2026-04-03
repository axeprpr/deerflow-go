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

func TestDefaultGatewayMCPConnectorSupportsStreamableHTTPTransportVariants(t *testing.T) {
	restore := stubGatewayMCPConnectors(t)
	defer restore()

	for _, transportType := range []string{"streamable-http", "streamableHttp", "STREAMABLE_HTTP"} {
		t.Run(transportType, func(t *testing.T) {
			called := false
			gatewayMCPConnectHTTP = func(ctx context.Context, name, baseURL string, headers map[string]string, headerFunc func(context.Context) map[string]string) (gatewayMCPClient, error) {
				called = true
				return &fakeGatewayMCPClientAdapter{}, nil
			}

			client, err := defaultGatewayMCPConnector(context.Background(), "demo", gatewayMCPServerConfig{
				Type: transportType,
				URL:  "https://example.com/mcp",
			})
			if err != nil {
				t.Fatalf("connect %s: %v", transportType, err)
			}
			if !called {
				t.Fatalf("expected http connector to be used for %s", transportType)
			}
			if client == nil {
				t.Fatalf("expected client for %s", transportType)
			}
		})
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

func TestDefaultGatewayMCPConnectorExpandsEnvInHeadersAndOAuth(t *testing.T) {
	restore := stubGatewayMCPConnectors(t)
	defer restore()

	t.Setenv("DEERFLOW_MCP_HEADER_TOKEN", "header-secret")
	t.Setenv("DEERFLOW_MCP_CLIENT_ID", "env-client")
	t.Setenv("DEERFLOW_MCP_CLIENT_SECRET", "env-secret")
	t.Setenv("DEERFLOW_MCP_RESOURCE", "env-resource")

	prevClientFactory := gatewayMCPOAuthHTTPClient
	defer func() { gatewayMCPOAuthHTTPClient = prevClientFactory }()

	gatewayMCPOAuthHTTPClient = func() *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("client_id"); got != "env-client" {
				t.Fatalf("client_id=%q", got)
			}
			if got := r.Form.Get("client_secret"); got != "env-secret" {
				t.Fatalf("client_secret=%q", got)
			}
			if got := r.Form.Get("resource"); got != "env-resource" {
				t.Fatalf("resource=%q", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"access_token":"token-env","token_type":"Bearer","expires_in":3600}`)),
			}, nil
		})}
	}

	gatewayMCPConnectHTTP = func(ctx context.Context, name, baseURL string, headers map[string]string, headerFunc func(context.Context) map[string]string) (gatewayMCPClient, error) {
		if got := headers["X-Api-Key"]; got != "header-secret" {
			t.Fatalf("X-Api-Key=%q", got)
		}
		if got := headers["Authorization"]; got != "Bearer token-env" {
			t.Fatalf("authorization=%q", got)
		}
		return &fakeGatewayMCPClientAdapter{}, nil
	}

	_, err := defaultGatewayMCPConnector(context.Background(), "demo", gatewayMCPServerConfig{
		Type:    "http",
		URL:     "https://example.com/mcp",
		Headers: map[string]string{"X-Api-Key": "$DEERFLOW_MCP_HEADER_TOKEN"},
		OAuth: &gatewayMCPOAuthConfig{
			Enabled:          true,
			TokenURL:         "https://auth.example.com/token",
			GrantType:        "client_credentials",
			ClientID:         "$DEERFLOW_MCP_CLIENT_ID",
			ClientSecret:     "$DEERFLOW_MCP_CLIENT_SECRET",
			ExtraTokenParams: map[string]string{"resource": "$DEERFLOW_MCP_RESOURCE"},
		},
	})
	if err != nil {
		t.Fatalf("connect http oauth env: %v", err)
	}
}

func TestDefaultGatewayMCPConnectorExpandsBracedAndDefaultEnvPlaceholders(t *testing.T) {
	restore := stubGatewayMCPConnectors(t)
	defer restore()

	t.Setenv("DEERFLOW_MCP_HOST", "docs.example.com")
	t.Setenv("DEERFLOW_MCP_CLIENT_ID", "env-client")

	prevClientFactory := gatewayMCPOAuthHTTPClient
	defer func() { gatewayMCPOAuthHTTPClient = prevClientFactory }()

	gatewayMCPOAuthHTTPClient = func() *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("client_id"); got != "env-client" {
				t.Fatalf("client_id=%q", got)
			}
			if got := r.Form.Get("client_secret"); got != "fallback-secret" {
				t.Fatalf("client_secret=%q", got)
			}
			if got := r.Form.Get("resource"); got != "docs-api" {
				t.Fatalf("resource=%q", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"access_token":"token-braced","token_type":"Bearer","expires_in":3600}`)),
			}, nil
		})}
	}

	gatewayMCPConnectHTTP = func(ctx context.Context, name, baseURL string, headers map[string]string, headerFunc func(context.Context) map[string]string) (gatewayMCPClient, error) {
		if got := baseURL; got != "https://docs.example.com/mcp" {
			t.Fatalf("baseURL=%q", got)
		}
		if got := headers["X-Api-Key"]; got != "fallback-header" {
			t.Fatalf("X-Api-Key=%q", got)
		}
		if got := headers["Authorization"]; got != "Bearer token-braced" {
			t.Fatalf("authorization=%q", got)
		}
		return &fakeGatewayMCPClientAdapter{}, nil
	}

	_, err := defaultGatewayMCPConnector(context.Background(), "demo", gatewayMCPServerConfig{
		Type: "streamableHttp",
		URL:  "https://${DEERFLOW_MCP_HOST}/mcp",
		Headers: map[string]string{
			"X-Api-Key": "${DEERFLOW_MCP_HEADER_TOKEN:-fallback-header}",
		},
		OAuth: &gatewayMCPOAuthConfig{
			Enabled:      true,
			TokenURL:     "https://${DEERFLOW_MCP_HOST}/oauth/token",
			GrantType:    "client_credentials",
			ClientID:     "${DEERFLOW_MCP_CLIENT_ID}",
			ClientSecret: "${DEERFLOW_MCP_CLIENT_SECRET:-fallback-secret}",
			ExtraTokenParams: map[string]string{
				"resource": "${DEERFLOW_MCP_RESOURCE:-docs-api}",
			},
		},
	})
	if err != nil {
		t.Fatalf("connect http oauth braced env: %v", err)
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

func TestGatewayMCPOAuthProviderCachesTokenAcrossCalls(t *testing.T) {
	prevClientFactory := gatewayMCPOAuthHTTPClient
	defer func() { gatewayMCPOAuthHTTPClient = prevClientFactory }()

	tokenCalls := 0
	gatewayMCPOAuthHTTPClient = func() *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			tokenCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"access_token":"cached-token","token_type":"Bearer","expires_in":3600}`)),
			}, nil
		})}
	}

	provider, err := newGatewayMCPOAuthProvider(&gatewayMCPOAuthConfig{
		Enabled:      true,
		TokenURL:     "https://auth.example.com/token",
		GrantType:    "client_credentials",
		ClientID:     "demo-client",
		ClientSecret: "demo-secret",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	first, err := provider.HeaderValue(context.Background())
	if err != nil {
		t.Fatalf("first header: %v", err)
	}
	second, err := provider.HeaderValue(context.Background())
	if err != nil {
		t.Fatalf("second header: %v", err)
	}
	if first != "Bearer cached-token" || second != first {
		t.Fatalf("headers=(%q,%q)", first, second)
	}
	if tokenCalls != 1 {
		t.Fatalf("tokenCalls=%d want=1", tokenCalls)
	}
}

func TestGatewayMCPOAuthProviderReturnsHTTPStatusErrorBeforeJSONDecode(t *testing.T) {
	prevClientFactory := gatewayMCPOAuthHTTPClient
	defer func() { gatewayMCPOAuthHTTPClient = prevClientFactory }()
	gatewayMCPOAuthHTTPClient = func() *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Header:     http.Header{"Content-Type": []string{"text/plain"}},
				Body:       io.NopCloser(strings.NewReader("upstream temporary failure")),
			}, nil
		})}
	}

	provider, err := newGatewayMCPOAuthProvider(&gatewayMCPOAuthConfig{
		Enabled:      true,
		TokenURL:     "https://auth.example.com/token",
		GrantType:    "client_credentials",
		ClientID:     "demo-client",
		ClientSecret: "demo-secret",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	_, err = provider.HeaderValue(context.Background())
	if err == nil {
		t.Fatal("expected oauth status error")
	}
	if !strings.Contains(err.Error(), "status 502") || !strings.Contains(err.Error(), "upstream temporary failure") {
		t.Fatalf("err=%v", err)
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

func TestNormalizeGatewayMCPOAuthConfigExpandsEnvValues(t *testing.T) {
	t.Setenv("DEERFLOW_MCP_TOKEN_URL", "https://auth.example.com/token")
	t.Setenv("DEERFLOW_MCP_REFRESH_TOKEN", "refresh-123")
	t.Setenv("DEERFLOW_MCP_SCOPE", "repo")
	t.Setenv("DEERFLOW_MCP_AUDIENCE", "mcp")
	t.Setenv("DEERFLOW_MCP_RESOURCE", "github")

	cfg := normalizeGatewayMCPOAuthConfig(gatewayMCPOAuthConfig{
		Enabled:      true,
		TokenURL:     "$DEERFLOW_MCP_TOKEN_URL",
		RefreshToken: "$DEERFLOW_MCP_REFRESH_TOKEN",
		Scope:        "$DEERFLOW_MCP_SCOPE",
		Audience:     "$DEERFLOW_MCP_AUDIENCE",
		ExtraTokenParams: map[string]string{
			"resource": "$DEERFLOW_MCP_RESOURCE",
		},
	})

	if cfg.TokenURL != "https://auth.example.com/token" {
		t.Fatalf("tokenURL=%q", cfg.TokenURL)
	}
	if cfg.RefreshToken != "refresh-123" {
		t.Fatalf("refreshToken=%q", cfg.RefreshToken)
	}
	if cfg.Scope != "repo" {
		t.Fatalf("scope=%q", cfg.Scope)
	}
	if cfg.Audience != "mcp" {
		t.Fatalf("audience=%q", cfg.Audience)
	}
	if got := cfg.ExtraTokenParams["resource"]; got != "github" {
		t.Fatalf("resource=%q", got)
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
