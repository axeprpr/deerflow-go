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

const defaultOpenAIBaseURL = "https://api.openai.com/v1"

type OpenAIProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewOpenAIProvider() *OpenAIProvider {
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	return &OpenAIProvider{
		apiKey:  strings.TrimSpace(os.Getenv("OPENAI_API_KEY")),
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if err := req.Validate(); err != nil {
		return ChatResponse{}, err
	}
	body, err := p.buildChatBody(req, false)
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq, err := p.newRequest(ctx, http.MethodPost, "/chat/completions", body)
	if err != nil {
		return ChatResponse{}, err
	}

	var resp openAIChatResponse
	if err := p.doJSON(httpReq, &resp); err != nil {
		return ChatResponse{}, err
	}
	if len(resp.Choices) == 0 {
		return ChatResponse{}, errors.New("openai: empty choices")
	}

	msg := openAIResponseMessageToModel(resp.Choices[0].Message)
	return ChatResponse{
		Model:   resp.Model,
		Message: msg,
		Usage: Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
		Stop: resp.Choices[0].FinishReason,
	}, nil
}

func (p *OpenAIProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	body, err := p.buildChatBody(req, true)
	if err != nil {
		return nil, err
	}
	httpReq, err := p.newRequest(ctx, http.MethodPost, "/chat/completions", body)
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
			fullText  strings.Builder
			toolBuf   = map[int]*streamToolCallBuffer{}
			modelName string
			stop      string
			usage     *Usage
		)

		emitErr := func(err error) {
			select {
			case ch <- StreamChunk{Err: err, Done: true}:
			case <-ctx.Done():
			}
		}

		for {
			data, err := readSSEData(reader)
			if err != nil {
				if errors.Is(err, io.EOF) {
					msg := buildAssistantMessage(fullText.String(), flattenToolCalls(toolBuf))
					select {
					case ch <- StreamChunk{Model: modelName, Message: &msg, Usage: usage, Stop: stop, Done: true}:
					case <-ctx.Done():
					}
					return
				}
				emitErr(err)
				return
			}
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				msg := buildAssistantMessage(fullText.String(), flattenToolCalls(toolBuf))
				select {
				case ch <- StreamChunk{Model: modelName, Message: &msg, Usage: usage, Stop: stop, Done: true}:
				case <-ctx.Done():
				}
				return
			}

			var evt openAIStreamResponse
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				emitErr(fmt.Errorf("openai stream decode: %w", err))
				return
			}
			if evt.Model != "" {
				modelName = evt.Model
			}
			if evt.Usage != nil {
				u := Usage{
					InputTokens:  evt.Usage.PromptTokens,
					OutputTokens: evt.Usage.CompletionTokens,
					TotalTokens:  evt.Usage.TotalTokens,
				}
				usage = &u
			}

			for _, choice := range evt.Choices {
				if choice.FinishReason != "" {
					stop = choice.FinishReason
				}
				delta := choice.Delta.Content
				if delta != "" {
					fullText.WriteString(delta)
				}
				toolCalls := applyOpenAIStreamToolDelta(toolBuf, choice.Delta.ToolCalls)
				chunk := StreamChunk{
					Model:     modelName,
					Delta:     delta,
					ToolCalls: toolCalls,
					Stop:      choice.FinishReason,
				}
				if usage != nil {
					chunk.Usage = usage
				}
				if delta != "" || len(toolCalls) > 0 || choice.FinishReason != "" || usage != nil {
					select {
					case ch <- chunk:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch, nil
}

func (p *OpenAIProvider) buildChatBody(req ChatRequest, stream bool) (map[string]any, error) {
	messages := make([]map[string]any, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.SystemPrompt) != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": req.SystemPrompt,
		})
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
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		body["max_tokens"] = *req.MaxTokens
	}
	if len(req.Tools) > 0 {
		body["tools"] = openAITools(req.Tools)
	}
	return body, nil
}

func (p *OpenAIProvider) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	if strings.TrimSpace(p.apiKey) == "" {
		return nil, errors.New("openai: OPENAI_API_KEY is not set")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai marshal request: %w", err)
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

func (p *OpenAIProvider) doJSON(req *http.Request, out any) error {
	res, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if err := requireOK(res); err != nil {
		return err
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("openai decode response: %w", err)
	}
	return nil
}

type openAIChatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIStreamResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content   string                  `json:"content"`
			ToolCalls []openAIStreamToolDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIStreamToolDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type streamToolCallBuffer struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

func openAITools(tools []models.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  emptyObjectIfNil(tool.InputSchema),
			},
		})
	}
	return out
}

func openAIRequestMessageFromModel(msg models.Message) (map[string]any, error) {
	switch msg.Role {
	case models.RoleSystem:
		return map[string]any{"role": "system", "content": msg.Content}, nil
	case models.RoleHuman:
		return map[string]any{"role": "user", "content": msg.Content}, nil
	case models.RoleAI:
		out := map[string]any{"role": "assistant"}
		if msg.Content != "" {
			out["content"] = msg.Content
		}
		if len(msg.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(msg.ToolCalls))
			for _, call := range msg.ToolCalls {
				argJSON, err := json.Marshal(emptyObjectIfNil(call.Arguments))
				if err != nil {
					return nil, fmt.Errorf("marshal tool call arguments: %w", err)
				}
				calls = append(calls, map[string]any{
					"id":   call.ID,
					"type": "function",
					"function": map[string]any{
						"name":      call.Name,
						"arguments": string(argJSON),
					},
				})
			}
			out["tool_calls"] = calls
		}
		if _, ok := out["content"]; !ok && len(msg.ToolCalls) == 0 {
			out["content"] = ""
		}
		return out, nil
	case models.RoleTool:
		if msg.ToolResult == nil {
			return nil, errors.New("tool message missing tool_result")
		}
		return map[string]any{
			"role":         "tool",
			"tool_call_id": msg.ToolResult.CallID,
			"content":      coalesce(msg.ToolResult.Content, msg.Content, msg.ToolResult.Error),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported message role %q", msg.Role)
	}
}

func openAIResponseMessageToModel(msg openAIMessage) models.Message {
	out := models.Message{
		Role:    models.RoleAI,
		Content: msg.Content,
	}
	if strings.EqualFold(msg.Role, "tool") {
		out.Role = models.RoleTool
	}
	if len(msg.ToolCalls) > 0 {
		out.ToolCalls = make([]models.ToolCall, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			out.ToolCalls = append(out.ToolCalls, models.ToolCall{
				ID:        call.ID,
				Name:      call.Function.Name,
				Arguments: parseJSONObjectString(call.Function.Arguments),
				Status:    models.CallStatusPending,
			})
		}
	}
	return out
}

func applyOpenAIStreamToolDelta(buf map[int]*streamToolCallBuffer, deltas []openAIStreamToolDelta) []models.ToolCall {
	if len(deltas) == 0 {
		return nil
	}
	out := make([]models.ToolCall, 0, len(deltas))
	for _, delta := range deltas {
		entry := buf[delta.Index]
		if entry == nil {
			entry = &streamToolCallBuffer{}
			buf[delta.Index] = entry
		}
		if delta.ID != "" {
			entry.ID = delta.ID
		}
		if delta.Function.Name != "" {
			entry.Name = delta.Function.Name
		}
		if delta.Function.Arguments != "" {
			entry.Arguments.WriteString(delta.Function.Arguments)
		}
		out = append(out, models.ToolCall{
			ID:        entry.ID,
			Name:      entry.Name,
			Arguments: parseJSONObjectString(entry.Arguments.String()),
			Status:    models.CallStatusPending,
		})
	}
	return out
}

func flattenToolCalls(buf map[int]*streamToolCallBuffer) []models.ToolCall {
	if len(buf) == 0 {
		return nil
	}
	maxIndex := -1
	for idx := range buf {
		if idx > maxIndex {
			maxIndex = idx
		}
	}
	out := make([]models.ToolCall, 0, len(buf))
	for i := 0; i <= maxIndex; i++ {
		entry := buf[i]
		if entry == nil {
			continue
		}
		out = append(out, models.ToolCall{
			ID:        entry.ID,
			Name:      entry.Name,
			Arguments: parseJSONObjectString(entry.Arguments.String()),
			Status:    models.CallStatusPending,
		})
	}
	return out
}

func readSSEData(r *bufio.Reader) (string, error) {
	var lines []string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && len(lines) > 0 {
				return strings.Join(lines, "\n"), nil
			}
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return strings.Join(lines, "\n"), nil
		}
		if strings.HasPrefix(line, "data:") {
			lines = append(lines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

func requireOK(res *http.Response) error {
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
	return fmt.Errorf("http %s: %s", res.Status, strings.TrimSpace(string(body)))
}

func parseJSONObjectString(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]any{"raw": raw}
	}
	return out
}

func emptyObjectIfNil(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	return in
}

func buildAssistantMessage(content string, toolCalls []models.ToolCall) models.Message {
	return models.Message{
		Role:      models.RoleAI,
		Content:   content,
		ToolCalls: toolCalls,
	}
}

func coalesce(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
