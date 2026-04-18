package harnessruntime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPRemoteStateClient struct {
	client *http.Client
}

func NewHTTPRemoteStateClient(client *http.Client) *HTTPRemoteStateClient {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPRemoteStateClient{client: client}
}

func (c *HTTPRemoteStateClient) Do(ctx context.Context, method, endpoint string, body []byte, contentType string) ([]byte, int, error) {
	if c == nil || c.client == nil {
		return nil, 0, fmt.Errorf("http remote state client is not configured")
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, 0, fmt.Errorf("remote state endpoint is required")
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(contentType) != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(data) == 0 {
			return nil, resp.StatusCode, fmt.Errorf("%s", resp.Status)
		}
		return nil, resp.StatusCode, fmt.Errorf("%s", strings.TrimSpace(string(data)))
	}
	return data, resp.StatusCode, nil
}

func joinRemoteStateURL(endpoint, suffix string) string {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return suffix
	}
	return endpoint + suffix
}

func joinRemoteStateSnapshotURL(endpoint, runID string) string {
	return joinRemoteStateURL(endpoint, DefaultRemoteStateSnapshotsPath+"/"+url.PathEscape(strings.TrimSpace(runID)))
}

func joinRemoteStateSnapshotCancelURL(endpoint, runID string) string {
	return joinRemoteStateSnapshotURL(endpoint, runID) + "/cancel"
}

func joinRemoteStateSnapshotsListURL(endpoint, threadID string) string {
	raw := joinRemoteStateURL(endpoint, DefaultRemoteStateSnapshotsPath)
	if strings.TrimSpace(threadID) == "" {
		return raw
	}
	values := url.Values{}
	values.Set("thread_id", strings.TrimSpace(threadID))
	return raw + "?" + values.Encode()
}

func joinRemoteStateEventsURL(endpoint, runID string) string {
	return joinRemoteStateURL(endpoint, DefaultRemoteStateEventsPath+"/"+url.PathEscape(strings.TrimSpace(runID)))
}

func joinRemoteStateEventNextIndexURL(endpoint, runID string) string {
	return joinRemoteStateEventsURL(endpoint, runID) + "/next-index"
}

func joinRemoteStateThreadsURL(endpoint string) string {
	return joinRemoteStateURL(endpoint, DefaultRemoteStateThreadsPath)
}

func joinRemoteStateThreadURL(endpoint, threadID string) string {
	return joinRemoteStateURL(endpoint, DefaultRemoteStateThreadsPath+"/"+url.PathEscape(strings.TrimSpace(threadID)))
}

func joinRemoteStateThreadStatusURL(endpoint, threadID string) string {
	return joinRemoteStateThreadURL(endpoint, threadID) + "/status"
}

func joinRemoteStateThreadMetadataURL(endpoint, threadID, key string) string {
	return joinRemoteStateThreadURL(endpoint, threadID) + "/metadata/" + url.PathEscape(strings.TrimSpace(key))
}
