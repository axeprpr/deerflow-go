package harnessruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/sandbox"
)

type HTTPRemoteSandboxClient struct {
	client   *http.Client
	protocol RemoteSandboxProtocol
}

func NewHTTPRemoteSandboxClient(client *http.Client, protocol RemoteSandboxProtocol) *HTTPRemoteSandboxClient {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPRemoteSandboxClient{
		client:   client,
		protocol: defaultRemoteSandboxProtocol(protocol),
	}
}

func (c *HTTPRemoteSandboxClient) Acquire(ctx context.Context, endpoint string) (RemoteSandboxAcquireResponse, error) {
	var resp RemoteSandboxAcquireResponse
	body, err := c.do(ctx, http.MethodPost, joinRemoteSandboxURL(endpoint, DefaultRemoteSandboxLeasePath), nil, "application/json")
	if err != nil {
		return resp, err
	}
	err = jsonUnmarshal(body, &resp)
	return resp, err
}

func (c *HTTPRemoteSandboxClient) Heartbeat(ctx context.Context, endpoint, leaseID string) error {
	_, err := c.do(ctx, http.MethodPost, joinRemoteSandboxLeaseURL(endpoint, leaseID, DefaultRemoteSandboxHeartbeatPath), nil, "application/json")
	return err
}

func (c *HTTPRemoteSandboxClient) Release(ctx context.Context, endpoint, leaseID string) error {
	_, err := c.do(ctx, http.MethodDelete, joinRemoteSandboxLeaseURL(endpoint, leaseID, ""), nil, "")
	return err
}

func (c *HTTPRemoteSandboxClient) Exec(ctx context.Context, endpoint, leaseID, cmd string, timeout time.Duration) (*sandbox.Result, error) {
	payload, err := defaultRemoteSandboxProtocol(c.protocol).EncodeExecRequest(RemoteSandboxExecRequest{
		Cmd:           strings.TrimSpace(cmd),
		TimeoutMillis: timeout.Milliseconds(),
	})
	if err != nil {
		return nil, err
	}
	body, err := c.do(ctx, http.MethodPost, joinRemoteSandboxLeaseURL(endpoint, leaseID, DefaultRemoteSandboxExecPath), payload, "application/json")
	if err != nil {
		return nil, err
	}
	return defaultRemoteSandboxProtocol(c.protocol).DecodeExecResponse(body)
}

func (c *HTTPRemoteSandboxClient) WriteFile(ctx context.Context, endpoint, leaseID, path string, data []byte) error {
	_, err := c.do(ctx, http.MethodPut, joinRemoteSandboxFileURL(endpoint, leaseID, path), data, "application/octet-stream")
	return err
}

func (c *HTTPRemoteSandboxClient) ReadFile(ctx context.Context, endpoint, leaseID, path string) ([]byte, error) {
	return c.do(ctx, http.MethodGet, joinRemoteSandboxFileURL(endpoint, leaseID, path), nil, "")
}

func (c *HTTPRemoteSandboxClient) do(ctx context.Context, method, endpoint string, body []byte, contentType string) ([]byte, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("http remote sandbox client is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(contentType) != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(data) == 0 {
			return nil, fmt.Errorf("%s", resp.Status)
		}
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(data)))
	}
	return data, nil
}

func joinRemoteSandboxURL(endpoint, suffix string) string {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return suffix
	}
	return endpoint + suffix
}

func joinRemoteSandboxLeaseURL(endpoint, leaseID, suffix string) string {
	return joinRemoteSandboxURL(endpoint, DefaultRemoteSandboxLeasePath+"/"+url.PathEscape(strings.TrimSpace(leaseID))+suffix)
}

func joinRemoteSandboxFileURL(endpoint, leaseID, path string) string {
	raw := joinRemoteSandboxLeaseURL(endpoint, leaseID, DefaultRemoteSandboxFilePath)
	values := url.Values{}
	values.Set("path", path)
	return raw + "?" + values.Encode()
}

func jsonUnmarshal(data []byte, target any) error {
	return json.Unmarshal(data, target)
}
