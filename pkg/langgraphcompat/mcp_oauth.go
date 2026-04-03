package langgraphcompat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type gatewayMCPOAuthProvider struct {
	cfg          gatewayMCPOAuthConfig
	httpClient   *http.Client
	mu           sync.Mutex
	token        string
	tokenType    string
	expiresAt    time.Time
	refreshToken string
}

var gatewayMCPOAuthHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

func newGatewayMCPOAuthProvider(cfg *gatewayMCPOAuthConfig) (*gatewayMCPOAuthProvider, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}
	tokenURL := strings.TrimSpace(cfg.TokenURL)
	if tokenURL == "" {
		return nil, fmt.Errorf("oauth token_url is required")
	}
	provider := &gatewayMCPOAuthProvider{
		cfg:        normalizeGatewayMCPOAuthConfig(*cfg),
		httpClient: gatewayMCPOAuthHTTPClient(),
	}
	provider.refreshToken = provider.cfg.RefreshToken
	return provider, nil
}

func normalizeGatewayMCPOAuthConfig(cfg gatewayMCPOAuthConfig) gatewayMCPOAuthConfig {
	cfg.TokenURL = strings.TrimSpace(expandGatewayEnvString(cfg.TokenURL))
	cfg.GrantType = strings.TrimSpace(cfg.GrantType)
	cfg.ClientID = strings.TrimSpace(expandGatewayEnvString(cfg.ClientID))
	cfg.ClientSecret = strings.TrimSpace(expandGatewayEnvString(cfg.ClientSecret))
	cfg.RefreshToken = strings.TrimSpace(expandGatewayEnvString(cfg.RefreshToken))
	cfg.Scope = strings.TrimSpace(expandGatewayEnvString(cfg.Scope))
	cfg.Audience = strings.TrimSpace(expandGatewayEnvString(cfg.Audience))
	if cfg.GrantType == "" {
		cfg.GrantType = "client_credentials"
	}
	if strings.TrimSpace(cfg.TokenField) == "" {
		cfg.TokenField = "access_token"
	}
	if strings.TrimSpace(cfg.TokenTypeField) == "" {
		cfg.TokenTypeField = "token_type"
	}
	if strings.TrimSpace(cfg.ExpiresInField) == "" {
		cfg.ExpiresInField = "expires_in"
	}
	if strings.TrimSpace(cfg.DefaultTokenType) == "" {
		cfg.DefaultTokenType = "Bearer"
	}
	if cfg.RefreshSkewSeconds < 0 {
		cfg.RefreshSkewSeconds = 0
	}
	if len(cfg.ExtraTokenParams) > 0 {
		expanded := make(map[string]string, len(cfg.ExtraTokenParams))
		for key, value := range cfg.ExtraTokenParams {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			expanded[key] = expandGatewayEnvString(value)
		}
		cfg.ExtraTokenParams = expanded
	}
	return cfg
}

func (p *gatewayMCPOAuthProvider) HeaderValue(ctx context.Context) (string, error) {
	if p == nil {
		return "", nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.token != "" && time.Now().UTC().Add(time.Duration(p.cfg.RefreshSkewSeconds)*time.Second).Before(p.expiresAt) {
		return p.tokenType + " " + p.token, nil
	}

	token, tokenType, expiresAt, refreshToken, err := p.fetchToken(ctx)
	if err != nil {
		return "", err
	}
	p.token = token
	p.tokenType = tokenType
	p.expiresAt = expiresAt
	if refreshToken != "" {
		p.refreshToken = refreshToken
	}
	return p.tokenType + " " + p.token, nil
}

func (p *gatewayMCPOAuthProvider) fetchToken(ctx context.Context) (string, string, time.Time, string, error) {
	form := url.Values{}
	form.Set("grant_type", p.cfg.GrantType)
	for key, value := range p.cfg.ExtraTokenParams {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		form.Set(key, value)
	}
	if p.cfg.Scope != "" {
		form.Set("scope", p.cfg.Scope)
	}
	if p.cfg.Audience != "" {
		form.Set("audience", p.cfg.Audience)
	}

	switch p.cfg.GrantType {
	case "client_credentials":
		if p.cfg.ClientID == "" || p.cfg.ClientSecret == "" {
			return "", "", time.Time{}, "", fmt.Errorf("oauth client_credentials requires client_id and client_secret")
		}
		form.Set("client_id", p.cfg.ClientID)
		form.Set("client_secret", p.cfg.ClientSecret)
	case "refresh_token":
		refreshToken := strings.TrimSpace(p.refreshToken)
		if refreshToken == "" {
			return "", "", time.Time{}, "", fmt.Errorf("oauth refresh_token grant requires refresh_token")
		}
		form.Set("refresh_token", refreshToken)
		if p.cfg.ClientID != "" {
			form.Set("client_id", p.cfg.ClientID)
		}
		if p.cfg.ClientSecret != "" {
			form.Set("client_secret", p.cfg.ClientSecret)
		}
	default:
		return "", "", time.Time{}, "", fmt.Errorf("unsupported oauth grant type %q", p.cfg.GrantType)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", time.Time{}, "", fmt.Errorf("create oauth token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", "", time.Time{}, "", fmt.Errorf("request oauth token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", time.Time{}, "", fmt.Errorf("read oauth token response: %w", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", time.Time{}, "", fmt.Errorf("decode oauth token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", time.Time{}, "", fmt.Errorf("oauth token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	accessToken := strings.TrimSpace(asString(payload[p.cfg.TokenField]))
	if accessToken == "" {
		return "", "", time.Time{}, "", fmt.Errorf("oauth token response missing %q", p.cfg.TokenField)
	}
	tokenType := strings.TrimSpace(asString(payload[p.cfg.TokenTypeField]))
	if tokenType == "" {
		tokenType = p.cfg.DefaultTokenType
	}
	expiresIn := 3600
	if raw := payload[p.cfg.ExpiresInField]; raw != nil {
		if parsed, ok := parsePositiveInt(raw); ok {
			expiresIn = parsed
		}
	}
	refreshToken := strings.TrimSpace(asString(payload["refresh_token"]))
	return accessToken, tokenType, time.Now().UTC().Add(time.Duration(max(expiresIn, 1)) * time.Second), refreshToken, nil
}

func parsePositiveInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, v > 0
	case int64:
		return int(v), v > 0
	case float64:
		i := int(v)
		return i, i > 0
	case json.Number:
		i, err := v.Int64()
		return int(i), err == nil && i > 0
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		return i, err == nil && i > 0
	default:
		return 0, false
	}
}
