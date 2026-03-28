package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultSiliconFlowBaseURL = "https://api.siliconflow.cn/v1"

type SiliconFlowProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewSiliconFlowProvider() *SiliconFlowProvider {
	return &SiliconFlowProvider{
		apiKey:  strings.TrimSpace(os.Getenv("SILICONFLOW_API_KEY")),
		baseURL: defaultSiliconFlowBaseURL,
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

func (p *SiliconFlowProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	return p.asOpenAI().Chat(ctx, req)
}

func (p *SiliconFlowProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	return p.asOpenAI().Stream(ctx, req)
}

func (p *SiliconFlowProvider) asOpenAI() *OpenAIProvider {
	return &OpenAIProvider{
		apiKey:     p.apiKey,
		baseURL:    p.baseURL,
		httpClient: p.httpClient,
	}
}

func (p *SiliconFlowProvider) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	if strings.TrimSpace(p.apiKey) == "" {
		return nil, errors.New("siliconflow: SILICONFLOW_API_KEY is not set")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return req, nil
}
