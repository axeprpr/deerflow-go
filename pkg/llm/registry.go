package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

const defaultOllamaBaseURL = "http://localhost:11434"

func NewProvider(name string) LLMProvider {
	provider := strings.TrimSpace(name)
	if provider == "" {
		provider = strings.TrimSpace(os.Getenv("DEFAULT_LLM_PROVIDER"))
	}
	switch strings.ToLower(provider) {
	case "", "openai":
		return NewOpenAIProvider()
	case "siliconflow":
		return NewSiliconFlowProvider()
	case "anthropic":
		return NewAnthropicProvider()
	case "ollama":
		return newOllamaProvider()
	default:
		return nil
	}
}

type ollamaProvider struct {
	baseURL    string
	httpClient *http.Client
}

func newOllamaProvider() *ollamaProvider {
	baseURL := strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	return &ollamaProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

func (p *ollamaProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if err := req.Validate(); err != nil {
		return ChatResponse{}, err
	}
	body, err := p.requestBody(req, false)
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq, err := p.newRequest(ctx, body)
	if err != nil {
		return ChatResponse{}, err
	}
	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer res.Body.Close()
	if err := requireOK(res); err != nil {
		return ChatResponse{}, err
	}

	var resp ollamaResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return ChatResponse{}, fmt.Errorf("ollama decode response: %w", err)
	}
	msg := models.Message{
		Role:    models.RoleAI,
		Content: resp.Message.Content,
	}
	if len(resp.Message.ToolCalls) > 0 {
		msg.ToolCalls = make([]models.ToolCall, 0, len(resp.Message.ToolCalls))
		for _, call := range resp.Message.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, models.ToolCall{
				ID:        call.Function.Name,
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
				Status:    models.CallStatusPending,
			})
		}
	}
	return ChatResponse{
		Model:   req.Model,
		Message: msg,
		Usage: Usage{
			InputTokens:  resp.PromptEvalCount,
			OutputTokens: resp.EvalCount,
			TotalTokens:  resp.PromptEvalCount + resp.EvalCount,
		},
		Stop: "stop",
	}, nil
}

func (p *ollamaProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	body, err := p.requestBody(req, true)
	if err != nil {
		return nil, err
	}
	httpReq, err := p.newRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if err := requireOK(res); err != nil {
		res.Body.Close()
		return nil, err
	}

	ch := make(chan StreamChunk)
	go func() {
		defer close(ch)
		defer res.Body.Close()

		reader := bufio.NewReader(res.Body)
		var fullText strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					msg := models.Message{Role: models.RoleAI, Content: fullText.String()}
					select {
					case ch <- StreamChunk{Model: req.Model, Message: &msg, Done: true}:
					case <-ctx.Done():
					}
					return
				}
				select {
				case ch <- StreamChunk{Err: err, Done: true}:
				case <-ctx.Done():
				}
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var evt ollamaResponse
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				select {
				case ch <- StreamChunk{Err: fmt.Errorf("ollama stream decode: %w", err), Done: true}:
				case <-ctx.Done():
				}
				return
			}
			if evt.Message.Content != "" {
				fullText.WriteString(evt.Message.Content)
				select {
				case ch <- StreamChunk{Model: req.Model, Delta: evt.Message.Content}:
				case <-ctx.Done():
					return
				}
			}
			if evt.Done {
				msg := models.Message{Role: models.RoleAI, Content: fullText.String()}
				select {
				case ch <- StreamChunk{
					Model:   req.Model,
					Message: &msg,
					Usage: &Usage{
						InputTokens:  evt.PromptEvalCount,
						OutputTokens: evt.EvalCount,
						TotalTokens:  evt.PromptEvalCount + evt.EvalCount,
					},
					Done: true,
					Stop: "stop",
				}:
				case <-ctx.Done():
				}
				return
			}
		}
	}()
	return ch, nil
}

func (p *ollamaProvider) requestBody(req ChatRequest, stream bool) (map[string]any, error) {
	messages := make([]map[string]any, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.SystemPrompt) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": req.SystemPrompt})
	}
	for _, msg := range req.Messages {
		converted, err := openAIRequestMessageFromModel(msg)
		if err != nil {
			return nil, err
		}
		messages = append(messages, converted)
	}
	body := map[string]any{
		"model":    req.Model,
		"messages": messages,
		"stream":   stream,
	}
	if req.Temperature != nil || req.MaxTokens != nil {
		options := map[string]any{}
		if req.Temperature != nil {
			options["temperature"] = *req.Temperature
		}
		if req.MaxTokens != nil {
			options["num_predict"] = *req.MaxTokens
		}
		body["options"] = options
	}
	if len(req.Tools) > 0 {
		body["tools"] = openAITools(req.Tools)
	}
	return body, nil
}

func (p *ollamaProvider) newRequest(ctx context.Context, body any) (*http.Request, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return req, nil
}

type ollamaResponse struct {
	Message struct {
		Content   string `json:"content"`
		ToolCalls []struct {
			Function struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"message"`
	Done            bool `json:"done"`
	PromptEvalCount int  `json:"prompt_eval_count"`
	EvalCount       int  `json:"eval_count"`
}
