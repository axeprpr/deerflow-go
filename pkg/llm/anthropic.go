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

const defaultAnthropicBaseURL = "https://api.anthropic.com/v1"

type AnthropicProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewAnthropicProvider() *AnthropicProvider {
	return &AnthropicProvider{
		apiKey:  strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
		baseURL: defaultAnthropicBaseURL,
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if err := req.Validate(); err != nil {
		return ChatResponse{}, err
	}
	body, err := p.buildMessagesBody(req, false)
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq, err := p.newRequest(ctx, "/messages", body)
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

	var resp anthropicMessageResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return ChatResponse{}, fmt.Errorf("anthropic decode response: %w", err)
	}

	msg := anthropicResponseToModel(resp)
	return ChatResponse{
		Model:   resp.Model,
		Message: msg,
		Usage: Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			TotalTokens:  resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
		Stop: resp.StopReason,
	}, nil
}

func (p *AnthropicProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	body, err := p.buildMessagesBody(req, true)
	if err != nil {
		return nil, err
	}
	httpReq, err := p.newRequest(ctx, "/messages", body)
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
		var (
			modelName string
			fullText  strings.Builder
			toolCalls []models.ToolCall
			usage     *Usage
			stop      string
		)

		emitErr := func(err error) {
			select {
			case ch <- StreamChunk{Err: err, Done: true}:
			case <-ctx.Done():
			}
		}

		for {
			payload, err := readSSEEventPayload(reader)
			if err != nil {
				if errors.Is(err, io.EOF) {
					msg := buildAssistantMessage(fullText.String(), toolCalls)
					select {
					case ch <- StreamChunk{Model: modelName, Message: &msg, Usage: usage, Stop: stop, Done: true}:
					case <-ctx.Done():
					}
					return
				}
				emitErr(err)
				return
			}
			if payload == "" {
				continue
			}

			var evt anthropicStreamEnvelope
			if err := json.Unmarshal([]byte(payload), &evt); err != nil {
				emitErr(fmt.Errorf("anthropic stream decode: %w", err))
				return
			}

			switch evt.Type {
			case "message_start":
				modelName = evt.Message.Model
				u := Usage{
					InputTokens:  evt.Message.Usage.InputTokens,
					OutputTokens: evt.Message.Usage.OutputTokens,
					TotalTokens:  evt.Message.Usage.InputTokens + evt.Message.Usage.OutputTokens,
				}
				usage = &u
			case "content_block_delta":
				if evt.Delta.Type == "text_delta" && evt.Delta.Text != "" {
					fullText.WriteString(evt.Delta.Text)
					select {
					case ch <- StreamChunk{Model: modelName, Delta: evt.Delta.Text}:
					case <-ctx.Done():
						return
					}
				}
				if evt.Delta.Type == "input_json_delta" && evt.Index >= 0 && evt.Index < len(toolCalls) {
					if toolCalls[evt.Index].Arguments == nil {
						toolCalls[evt.Index].Arguments = map[string]any{}
					}
					toolCalls[evt.Index].Arguments = parseJSONObjectString(coalesce(
						stringifyJSONObject(toolCalls[evt.Index].Arguments)+evt.Delta.PartialJSON,
						evt.Delta.PartialJSON,
					))
					select {
					case ch <- StreamChunk{Model: modelName, ToolCalls: []models.ToolCall{toolCalls[evt.Index]}}:
					case <-ctx.Done():
						return
					}
				}
			case "content_block_start":
				if evt.ContentBlock.Type == "tool_use" {
					call := models.ToolCall{
						ID:        evt.ContentBlock.ID,
						Name:      evt.ContentBlock.Name,
						Arguments: evt.ContentBlock.Input,
						Status:    models.CallStatusPending,
					}
					toolCalls = append(toolCalls, call)
					select {
					case ch <- StreamChunk{Model: modelName, ToolCalls: []models.ToolCall{call}}:
					case <-ctx.Done():
						return
					}
				}
			case "message_delta":
				stop = evt.Delta.StopReason
				if evt.Usage.OutputTokens > 0 {
					u := Usage{
						InputTokens:  usage.InputTokens,
						OutputTokens: evt.Usage.OutputTokens,
						TotalTokens:  usage.InputTokens + evt.Usage.OutputTokens,
					}
					usage = &u
				}
			case "message_stop":
				msg := buildAssistantMessage(fullText.String(), toolCalls)
				select {
				case ch <- StreamChunk{Model: modelName, Message: &msg, Usage: usage, Stop: stop, Done: true}:
				case <-ctx.Done():
				}
				return
			}
		}
	}()

	return ch, nil
}

func (p *AnthropicProvider) buildMessagesBody(req ChatRequest, stream bool) (map[string]any, error) {
	systemParts := make([]string, 0, 1)
	if strings.TrimSpace(req.SystemPrompt) != "" {
		systemParts = append(systemParts, req.SystemPrompt)
	}

	messages := make([]map[string]any, 0, len(req.Messages))
	for _, msg := range req.Messages {
		switch msg.Role {
		case models.RoleSystem:
			if strings.TrimSpace(msg.Content) != "" {
				systemParts = append(systemParts, msg.Content)
			}
		case models.RoleHuman:
			messages = append(messages, map[string]any{
				"role":    "user",
				"content": []map[string]any{{"type": "text", "text": msg.Content}},
			})
		case models.RoleAI:
			content := make([]map[string]any, 0, 1+len(msg.ToolCalls))
			if msg.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": msg.Content})
			}
			for _, call := range msg.ToolCalls {
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    call.ID,
					"name":  call.Name,
					"input": emptyObjectIfNil(call.Arguments),
				})
			}
			if len(content) == 0 {
				content = append(content, map[string]any{"type": "text", "text": ""})
			}
			messages = append(messages, map[string]any{"role": "assistant", "content": content})
		case models.RoleTool:
			if msg.ToolResult == nil {
				return nil, errors.New("anthropic: tool message missing tool_result")
			}
			messages = append(messages, map[string]any{
				"role": "user",
				"content": []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": msg.ToolResult.CallID,
					"content":     coalesce(msg.ToolResult.Content, msg.Content, msg.ToolResult.Error),
					"is_error":    msg.ToolResult.Status == models.CallStatusFailed,
				}},
			})
		default:
			return nil, fmt.Errorf("anthropic: unsupported role %q", msg.Role)
		}
	}

	body := map[string]any{
		"model":    req.Model,
		"messages": messages,
		"stream":   stream,
	}
	if system := strings.TrimSpace(strings.Join(systemParts, "\n\n")); system != "" {
		body["system"] = system
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		body["max_tokens"] = *req.MaxTokens
	}
	if len(req.Tools) > 0 {
		body["tools"] = anthropicTools(req.Tools)
	}
	return body, nil
}

func (p *AnthropicProvider) newRequest(ctx context.Context, path string, body any) (*http.Request, error) {
	if strings.TrimSpace(p.apiKey) == "" {
		return nil, errors.New("anthropic: ANTHROPIC_API_KEY is not set")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	return req, nil
}

type anthropicMessageResponse struct {
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type  string         `json:"type"`
		Text  string         `json:"text,omitempty"`
		ID    string         `json:"id,omitempty"`
		Name  string         `json:"name,omitempty"`
		Input map[string]any `json:"input,omitempty"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicStreamEnvelope struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	ContentBlock struct {
		Type  string         `json:"type"`
		ID    string         `json:"id"`
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func anthropicTools(tools []models.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": emptyObjectIfNil(tool.InputSchema),
		})
	}
	return out
}

func anthropicResponseToModel(resp anthropicMessageResponse) models.Message {
	out := models.Message{Role: models.RoleAI}
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			out.Content += block.Text
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, models.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: block.Input,
				Status:    models.CallStatusPending,
			})
		}
	}
	return out
}

func readSSEEventPayload(r *bufio.Reader) (string, error) {
	return readSSEData(r)
}

func stringifyJSONObject(v map[string]any) string {
	if len(v) == 0 {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
