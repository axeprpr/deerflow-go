package harnessruntime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type HTTPRemoteWorkerClient struct {
	client *http.Client
}

func NewHTTPRemoteWorkerClient(client *http.Client) *HTTPRemoteWorkerClient {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPRemoteWorkerClient{client: client}
}

func (c *HTTPRemoteWorkerClient) Submit(ctx context.Context, endpoint string, payload []byte) ([]byte, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("http remote worker client is not configured")
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, errors.New("remote worker endpoint is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(body) == 0 {
			return nil, errors.New(resp.Status)
		}
		return nil, errors.New(strings.TrimSpace(string(body)))
	}
	return body, nil
}
