package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
	einoOpenAI "github.com/cloudwego/eino-ext/components/model/openai"
	einoModel "github.com/cloudwego/eino/components/model"
	einoSchema "github.com/cloudwego/eino/schema"
)

const (
	defaultOpenAIBaseURL      = "https://api.openai.com/v1"
	defaultSiliconFlowBaseURL = "https://api.siliconflow.cn/v1"
)

type EinoProvider struct {
	provider string
	base     einoModel.ToolCallingChatModel
}

func NewEinoProvider(name string) (*EinoProvider, error) {
	provider := strings.ToLower(strings.TrimSpace(name))
	if provider == "" {
		provider = "openai"
	}

	cfg, err := newEinoChatModelConfig(provider)
	if err != nil {
		return nil, err
	}

	model, err := einoOpenAI.NewChatModel(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("init eino %s model: %w", provider, err)
	}

	return &EinoProvider{
		provider: provider,
		base:     model,
	}, nil
}

func (p *EinoProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if err := req.Validate(); err != nil {
		return ChatResponse{}, err
	}

	msgs, callOpts, err := p.prepareRequest(req)
	if err != nil {
		return ChatResponse{}, err
	}

	resp, err := p.base.Generate(ctx, msgs, callOpts...)
	if err != nil {
		return ChatResponse{}, err
	}

	return ChatResponse{
		Model:   req.Model,
		Message: fromEinoMessage(resp),
		Usage:   fromEinoUsage(resp.ResponseMeta),
		Stop:    finishReason(resp.ResponseMeta),
	}, nil
}

func (p *EinoProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	msgs, callOpts, err := p.prepareRequest(req)
	if err != nil {
		return nil, err
	}

	stream, err := p.base.Stream(ctx, msgs, callOpts...)
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk)
	go func() {
		defer close(ch)
		defer stream.Close()

		var chunks []*einoSchema.Message

		send := func(chunk StreamChunk) bool {
			if req.OnChunk != nil {
				req.OnChunk(chunk)
			}
			select {
			case ch <- chunk:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for {
			msg, recvErr := stream.Recv()
			if recvErr != nil {
				if recvErr == io.EOF {
					break
				}
				send(StreamChunk{Err: recvErr, Done: true})
				return
			}

			chunks = append(chunks, msg)
			if !send(StreamChunk{
				Model:     req.Model,
				Delta:     msg.Content,
				ToolCalls: fromEinoToolCalls(msg.ToolCalls),
			}) {
				return
			}
		}

		if len(chunks) == 0 {
			send(StreamChunk{Err: io.ErrUnexpectedEOF, Done: true})
			return
		}

		finalMsg, err := einoSchema.ConcatMessages(chunks)
		if err != nil {
			send(StreamChunk{Err: err, Done: true})
			return
		}

		send(StreamChunk{
			Model:   req.Model,
			Message: ptr(fromEinoMessage(finalMsg)),
			Usage:   ptr(fromEinoUsage(finalMsg.ResponseMeta)),
			Stop:    finishReason(finalMsg.ResponseMeta),
			Done:    true,
		})
	}()

	return ch, nil
}

func (p *EinoProvider) prepareRequest(req ChatRequest) ([]*einoSchema.Message, []einoModel.Option, error) {
	msgs := make([]*einoSchema.Message, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.SystemPrompt) != "" {
		msgs = append(msgs, &einoSchema.Message{
			Role:    einoSchema.System,
			Content: req.SystemPrompt,
		})
	}
	for _, msg := range req.Messages {
		msgs = append(msgs, toEinoMessage(msg))
	}

	opts := make([]einoModel.Option, 0, 4)
	if req.Model != "" {
		opts = append(opts, einoModel.WithModel(req.Model))
	}
	if effort := strings.TrimSpace(req.ReasoningEffort); effort != "" {
		opts = append(opts, einoOpenAI.WithReasoningEffort(einoOpenAI.ReasoningEffortLevel(effort)))
	}
	if req.Temperature != nil {
		v := float32(*req.Temperature)
		opts = append(opts, einoModel.WithTemperature(v))
	}
	if req.MaxTokens != nil {
		opts = append(opts, einoModel.WithMaxTokens(*req.MaxTokens))
	}
	if len(req.Tools) > 0 {
		opts = append(opts, einoModel.WithTools(toEinoToolInfos(req.Tools)))
	}
	return msgs, opts, nil
}

func newEinoChatModelConfig(provider string) (*einoOpenAI.ChatModelConfig, error) {
	modelName := strings.TrimSpace(os.Getenv("DEFAULT_LLM_MODEL"))
	if modelName == "" {
		modelName = "gpt-4.1-mini"
	}

	cfg := &einoOpenAI.ChatModelConfig{
		Model:   modelName,
		Timeout: 2 * time.Minute,
		HTTPClient: &http.Client{
			Timeout: 2 * time.Minute,
		},
	}

	switch provider {
	case "", "openai":
		cfg.APIKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		cfg.BaseURL = strings.TrimSpace(os.Getenv("OPENAI_API_BASE_URL"))
		if cfg.BaseURL == "" {
			cfg.BaseURL = defaultOpenAIBaseURL
		}
	case "siliconflow":
		cfg.APIKey = strings.TrimSpace(os.Getenv("SILICONFLOW_API_KEY"))
		cfg.BaseURL = strings.TrimSpace(os.Getenv("OPENAI_API_BASE_URL"))
		if cfg.BaseURL == "" {
			cfg.BaseURL = defaultSiliconFlowBaseURL
		}
	case "anthropic":
		cfg.APIKey = strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
		cfg.BaseURL = strings.TrimSpace(os.Getenv("OPENAI_API_BASE_URL"))
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("anthropic requires OPENAI_API_BASE_URL to point at an OpenAI-compatible gateway")
		}
	default:
		return nil, fmt.Errorf("unsupported llm provider %q", provider)
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("%s api key is not set", provider)
	}
	return cfg, nil
}

func toEinoMessage(msg models.Message) *einoSchema.Message {
	out := &einoSchema.Message{
		Content: msg.Content,
	}

	switch msg.Role {
	case models.RoleHuman:
		out.Role = einoSchema.User
		if multi := userInputMultiContent(msg.Metadata); len(multi) > 0 {
			out.Content = ""
			out.UserInputMultiContent = multi
		}
	case models.RoleSystem:
		out.Role = einoSchema.System
	case models.RoleTool:
		out.Role = einoSchema.Tool
		if msg.ToolResult != nil {
			out.ToolCallID = msg.ToolResult.CallID
			out.ToolName = msg.ToolResult.ToolName
			if out.Content == "" {
				if msg.ToolResult.Error != "" {
					out.Content = msg.ToolResult.Error
				} else {
					out.Content = msg.ToolResult.Content
				}
			}
		}
	default:
		out.Role = einoSchema.Assistant
		out.ToolCalls = toEinoToolCalls(msg.ToolCalls)
	}

	return out
}

func userInputMultiContent(metadata map[string]string) []einoSchema.MessageInputPart {
	if len(metadata) == 0 {
		return nil
	}
	raw := strings.TrimSpace(metadata["multi_content"])
	if raw == "" {
		return nil
	}
	var parts []map[string]any
	if err := json.Unmarshal([]byte(raw), &parts); err != nil {
		return nil
	}
	out := make([]einoSchema.MessageInputPart, 0, len(parts))
	for _, part := range parts {
		partType := strings.TrimSpace(stringFromAny(part["type"]))
		switch partType {
		case "text":
			text := stringFromAny(part["text"])
			if strings.TrimSpace(text) == "" {
				continue
			}
			out = append(out, einoSchema.MessageInputPart{
				Type: einoSchema.ChatMessagePartTypeText,
				Text: text,
			})
		case "image_url":
			imageURL, _ := part["image_url"].(map[string]any)
			url := stringFromAny(imageURL["url"])
			if strings.TrimSpace(url) == "" {
				continue
			}
			out = append(out, einoSchema.MessageInputPart{
				Type: einoSchema.ChatMessagePartTypeImageURL,
				Image: &einoSchema.MessageInputImage{
					MessagePartCommon: einoSchema.MessagePartCommon{URL: ptr(url)},
				},
			})
		}
	}
	return out
}

func stringFromAny(v any) string {
	switch value := v.(type) {
	case string:
		return value
	default:
		return ""
	}
}

func fromEinoMessage(msg *einoSchema.Message) models.Message {
	if msg == nil {
		return models.Message{}
	}

	role := models.RoleAI
	switch msg.Role {
	case einoSchema.User:
		role = models.RoleHuman
	case einoSchema.System:
		role = models.RoleSystem
	case einoSchema.Tool:
		role = models.RoleTool
	}

	out := models.Message{
		Role:    role,
		Content: msg.Content,
	}
	if len(msg.ToolCalls) > 0 {
		out.ToolCalls = fromEinoToolCalls(msg.ToolCalls)
	}
	if msg.Role == einoSchema.Tool {
		out.ToolResult = &models.ToolResult{
			CallID:   msg.ToolCallID,
			ToolName: msg.ToolName,
			Content:  msg.Content,
			Status:   models.CallStatusCompleted,
		}
	}
	if stop := finishReason(msg.ResponseMeta); stop != "" {
		out.Metadata = map[string]string{"stop_reason": stop}
	}
	return out
}

func toEinoToolCalls(calls []models.ToolCall) []einoSchema.ToolCall {
	out := make([]einoSchema.ToolCall, 0, len(calls))
	for _, call := range calls {
		raw, _ := json.Marshal(call.Arguments)
		out = append(out, einoSchema.ToolCall{
			ID:   call.ID,
			Type: "function",
			Function: einoSchema.FunctionCall{
				Name:      call.Name,
				Arguments: string(raw),
			},
		})
	}
	return out
}

func fromEinoToolCalls(calls []einoSchema.ToolCall) []models.ToolCall {
	out := make([]models.ToolCall, 0, len(calls))
	for _, call := range calls {
		args := map[string]any{}
		if strings.TrimSpace(call.Function.Arguments) != "" {
			_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
		}
		out = append(out, models.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: args,
			Status:    models.CallStatusPending,
		})
	}
	return out
}

func toEinoToolInfos(tools []models.Tool) []*einoSchema.ToolInfo {
	out := make([]*einoSchema.ToolInfo, 0, len(tools))
	for _, t := range tools {
		info := &einoSchema.ToolInfo{
			Name: t.Name,
			Desc: t.Description,
		}
		if len(t.InputSchema) > 0 {
			info.ParamsOneOf = einoSchema.NewParamsOneOfByParams(jsonSchemaToParams(t.InputSchema))
		}
		out = append(out, info)
	}
	return out
}

func jsonSchemaToParams(schema map[string]any) map[string]*einoSchema.ParameterInfo {
	properties, _ := schema["properties"].(map[string]any)
	requiredSet := map[string]struct{}{}
	switch required := schema["required"].(type) {
	case []any:
		for _, item := range required {
			if s, ok := item.(string); ok {
				requiredSet[s] = struct{}{}
			}
		}
	case []string:
		for _, item := range required {
			requiredSet[item] = struct{}{}
		}
	}

	out := make(map[string]*einoSchema.ParameterInfo, len(properties))
	for name, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		info := jsonSchemaPropertyToParam(prop)
		_, info.Required = requiredSet[name]
		out[name] = info
	}
	return out
}

func jsonSchemaPropertyToParam(prop map[string]any) *einoSchema.ParameterInfo {
	info := &einoSchema.ParameterInfo{
		Type: toDataType(prop["type"]),
		Desc: stringValue(prop["description"]),
	}
	if items, ok := prop["items"].(map[string]any); ok {
		info.ElemInfo = jsonSchemaPropertyToParam(items)
	}
	if sub, ok := prop["properties"].(map[string]any); ok {
		info.SubParams = make(map[string]*einoSchema.ParameterInfo, len(sub))
		requiredSet := map[string]struct{}{}
		if required, ok := prop["required"].([]any); ok {
			for _, item := range required {
				if s, ok := item.(string); ok {
					requiredSet[s] = struct{}{}
				}
			}
		}
		for name, raw := range sub {
			child, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			info.SubParams[name] = jsonSchemaPropertyToParam(child)
			_, info.SubParams[name].Required = requiredSet[name]
		}
	}
	if enumValues, ok := prop["enum"].([]any); ok {
		info.Enum = make([]string, 0, len(enumValues))
		for _, item := range enumValues {
			if s, ok := item.(string); ok {
				info.Enum = append(info.Enum, s)
			}
		}
	}
	return info
}

func toDataType(v any) einoSchema.DataType {
	switch strings.ToLower(stringValue(v)) {
	case "object":
		return einoSchema.Object
	case "number":
		return einoSchema.Number
	case "integer":
		return einoSchema.Integer
	case "array":
		return einoSchema.Array
	case "boolean":
		return einoSchema.Boolean
	case "null":
		return einoSchema.Null
	default:
		return einoSchema.String
	}
}

func fromEinoUsage(meta *einoSchema.ResponseMeta) Usage {
	if meta == nil || meta.Usage == nil {
		return Usage{}
	}
	return Usage{
		InputTokens:  meta.Usage.PromptTokens,
		OutputTokens: meta.Usage.CompletionTokens,
		TotalTokens:  meta.Usage.TotalTokens,
	}
}

func finishReason(meta *einoSchema.ResponseMeta) string {
	if meta == nil {
		return ""
	}
	return meta.FinishReason
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func ptr[T any](v T) *T {
	return &v
}
