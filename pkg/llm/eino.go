package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
	einoOpenAI "github.com/cloudwego/eino-ext/components/model/openai"
	einoModel "github.com/cloudwego/eino/components/model"
	einoSchema "github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
)

const (
	defaultOpenAIBaseURL       = "https://api.openai.com/v1"
	defaultSiliconFlowBaseURL  = "https://api.siliconflow.cn/v1"
	defaultHTTPDumpDir         = "/tmp/deerflow-eino-dump"
	defaultModelRequestTimeout = 10 * time.Minute
)

var httpDumpSeq uint64

type EinoProvider struct {
	provider string
	base     einoModel.ToolCallingChatModel
}

func (p *EinoProvider) PrefersStructuredToolCalls() bool {
	// Let the agent keep using the streaming path for tool turns.
	// In practice this matches upstream DeerFlow's agent loop better and
	// avoids stalling on a second synchronous completion after a tool result.
	return false
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
				ToolCalls: collectStreamToolCalls(chunks),
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

		streamedToolCalls := collectStreamToolCalls(chunks)
		if needsStructuredToolCallRepair(streamedToolCalls) {
			if repaired, repairErr := p.Chat(ctx, req); repairErr == nil && len(repaired.Message.ToolCalls) > 0 {
				streamedToolCalls = repairStructuredToolCalls(streamedToolCalls, repaired.Message.ToolCalls)
			}
		}

		send(StreamChunk{
			Model:   req.Model,
			Message: ptr(finalStreamMessage(finalMsg, streamedToolCalls)),
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
		if einoMsg := toEinoMessage(msg); einoMsg != nil {
			msgs = append(msgs, einoMsg)
		}
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
	timeout := resolveEinoRequestTimeout()

	httpClient := &http.Client{
		Timeout: timeout,
	}
	if transport := newHTTPDumpRoundTripper(http.DefaultTransport, provider); transport != nil {
		httpClient.Transport = transport
	}

	cfg := &einoOpenAI.ChatModelConfig{
		Model:      modelName,
		Timeout:    timeout,
		HTTPClient: httpClient,
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

func resolveEinoRequestTimeout() time.Duration {
	if timeout := timeoutFromEnv("DEERFLOW_LLM_REQUEST_TIMEOUT"); timeout > 0 {
		return timeout
	}
	if timeout := configuredModelRequestTimeout(); timeout > 0 {
		return timeout
	}
	return defaultModelRequestTimeout
}

func timeoutFromEnv(key string) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

func configuredModelRequestTimeout() time.Duration {
	path, ok := resolveLLMConfigPath()
	if !ok {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var raw struct {
		Models []map[string]any `yaml:"models"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return 0
	}
	var maxTimeout time.Duration
	for _, item := range raw.Models {
		if timeout := modelRequestTimeout(item); timeout > maxTimeout {
			maxTimeout = timeout
		}
	}
	return maxTimeout
}

func resolveLLMConfigPath() (string, bool) {
	for _, key := range []string{"DEERFLOW_CONFIG_PATH", "DEER_FLOW_CONFIG_PATH"} {
		if path := strings.TrimSpace(os.Getenv(key)); path != "" {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path, true
			}
			return "", false
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for _, candidate := range []string{
		filepath.Join(wd, "config.yaml"),
		filepath.Join(filepath.Dir(wd), "config.yaml"),
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func modelRequestTimeout(raw map[string]any) time.Duration {
	if raw == nil {
		return 0
	}
	for _, key := range []string{"request_timeout", "requestTimeout", "timeout"} {
		if seconds := floatSeconds(raw[key]); seconds > 0 {
			return time.Duration(seconds * float64(time.Second))
		}
	}
	return 0
}

func floatSeconds(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		n, _ := typed.Float64()
		return n
	case string:
		n, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return n
	default:
		return 0
	}
}

func newHTTPDumpRoundTripper(base http.RoundTripper, provider string) http.RoundTripper {
	if _, err := os.Stat(defaultHTTPDumpDir); err != nil {
		return nil
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &httpDumpRoundTripper{
		base:     base,
		provider: strings.TrimSpace(provider),
		dir:      defaultHTTPDumpDir,
	}
}

type httpDumpRoundTripper struct {
	base     http.RoundTripper
	provider string
	dir      string
}

func (rt *httpDumpRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return rt.base.RoundTrip(req)
	}

	seq := atomic.AddUint64(&httpDumpSeq, 1)
	prefix := filepath.Join(rt.dir, dumpFilePrefix(seq, rt.provider))
	reqCopy := req.Clone(req.Context())

	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		reqCopy.Body = io.NopCloser(bytes.NewReader(body))
		if writeErr := os.WriteFile(prefix+".request.json", body, 0o644); writeErr != nil {
			log.Printf("eino http dump request write failed: %v", writeErr)
		}
	}

	resp, err := rt.base.RoundTrip(reqCopy)
	if err != nil {
		if writeErr := os.WriteFile(prefix+".error.txt", []byte(err.Error()), 0o644); writeErr != nil {
			log.Printf("eino http dump error write failed: %v", writeErr)
		}
		return nil, err
	}

	headerText := dumpResponseHeaders(resp)
	if writeErr := os.WriteFile(prefix+".response.headers.txt", []byte(headerText), 0o644); writeErr != nil {
		log.Printf("eino http dump headers write failed: %v", writeErr)
	}
	resp.Body = &dumpingReadCloser{
		ReadCloser: resp.Body,
		path:       prefix + ".response.body.txt",
	}
	return resp, nil
}

func dumpFilePrefix(seq uint64, provider string) string {
	timestamp := time.Now().UTC().Format("20060102T150405.000000000")
	if provider == "" {
		provider = "provider"
	}
	return timestamp + "-" + provider + "-" + strconv.FormatUint(seq, 10)
}

func dumpResponseHeaders(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(resp.Proto)
	b.WriteByte(' ')
	b.WriteString(resp.Status)
	b.WriteByte('\n')
	for key, values := range resp.Header {
		for _, value := range values {
			b.WriteString(key)
			b.WriteString(": ")
			b.WriteString(value)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

type dumpingReadCloser struct {
	io.ReadCloser
	path string
	buf  bytes.Buffer
}

func (rc *dumpingReadCloser) Read(p []byte) (int, error) {
	n, err := rc.ReadCloser.Read(p)
	if n > 0 {
		_, _ = rc.buf.Write(p[:n])
	}
	return n, err
}

func (rc *dumpingReadCloser) Close() error {
	if rc.path != "" && rc.buf.Len() > 0 {
		if err := os.WriteFile(rc.path, rc.buf.Bytes(), 0o644); err != nil {
			log.Printf("eino http dump body write failed: %v", err)
		}
	}
	return rc.ReadCloser.Close()
}

func toEinoMessage(msg models.Message) *einoSchema.Message {
	out := &einoSchema.Message{
		Content: msg.Content,
	}

	switch msg.Role {
	case models.RoleHuman:
		out.Role = einoSchema.User
		if multi := userInputMultiContent(msg.Metadata); len(multi) > 0 {
			if hasNonTextUserInputPart(multi) {
				out.Content = ""
				out.UserInputMultiContent = multi
			}
		}
	case models.RoleSystem:
		out.Role = einoSchema.System
	case models.RoleTool:
		out.Role = einoSchema.Tool
		if msg.ToolResult != nil {
			normalized, ok := models.NormalizeToolResult(*msg.ToolResult)
			if !ok {
				return nil
			}
			out.ToolCallID = normalized.CallID
			out.ToolName = normalized.ToolName
			if out.Content == "" {
				if normalized.Error != "" {
					out.Content = normalized.Error
				} else {
					out.Content = normalized.Content
				}
			}
		}
	default:
		out.Role = einoSchema.Assistant
		out.ToolCalls = toEinoToolCalls(msg.ToolCalls)
	}
	if out.Role == einoSchema.Assistant && strings.TrimSpace(out.Content) == "" && len(out.ToolCalls) == 0 {
		return nil
	}
	if out.Role == einoSchema.Tool && msg.ToolResult == nil {
		return nil
	}

	return out
}

func hasNonTextUserInputPart(parts []einoSchema.MessageInputPart) bool {
	for _, part := range parts {
		if part.Type != einoSchema.ChatMessagePartTypeText {
			return true
		}
	}
	return false
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
	return NormalizeAssistantMessage(out)
}

func finalStreamMessage(msg *einoSchema.Message, streamedToolCalls []models.ToolCall) models.Message {
	out := fromEinoMessage(msg)
	if len(streamedToolCalls) > 0 {
		out.ToolCalls = append([]models.ToolCall(nil), streamedToolCalls...)
	}
	return NormalizeAssistantMessage(out)
}

func toEinoToolCalls(calls []models.ToolCall) []einoSchema.ToolCall {
	out := make([]einoSchema.ToolCall, 0, len(calls))
	for _, call := range calls {
		normalized, ok := models.NormalizeToolCall(call)
		if !ok {
			continue
		}
		raw, _ := json.Marshal(normalized.Arguments)
		out = append(out, einoSchema.ToolCall{
			ID:   normalized.ID,
			Type: "function",
			Function: einoSchema.FunctionCall{
				Name:      normalized.Name,
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
		normalized, ok := models.NormalizeToolCall(models.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: args,
			Status:    models.CallStatusPending,
		})
		if !ok {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

type streamToolCallAccumulator struct {
	id   string
	name string
	args strings.Builder
}

func collectStreamToolCalls(chunks []*einoSchema.Message) []models.ToolCall {
	if len(chunks) == 0 {
		return nil
	}

	accumulators := make(map[string]*streamToolCallAccumulator)
	order := make([]string, 0)
	for _, msg := range chunks {
		if msg == nil || len(msg.ToolCalls) == 0 {
			continue
		}
		for i, call := range msg.ToolCalls {
			id := strings.TrimSpace(call.ID)
			key := fmt.Sprintf("index:%d", i)

			acc, ok := accumulators[key]
			if !ok {
				acc = &streamToolCallAccumulator{id: id}
				accumulators[key] = acc
				order = append(order, key)
			}
			if name := strings.TrimSpace(call.Function.Name); name != "" {
				acc.name = name
			}
			if id != "" {
				acc.id = id
			}
			if raw := call.Function.Arguments; raw != "" {
				acc.args.WriteString(raw)
			}
		}
	}

	out := make([]models.ToolCall, 0, len(order))
	for _, key := range order {
		acc := accumulators[key]
		if acc == nil {
			continue
		}
		args := decodeToolArguments(acc.args.String())
		normalized, ok := models.NormalizeToolCall(models.ToolCall{
			ID:        acc.id,
			Name:      acc.name,
			Arguments: args,
			Status:    models.CallStatusPending,
		})
		if !ok {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func decodeToolArguments(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil
	}
	return args
}

func needsStructuredToolCallRepair(calls []models.ToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if len(call.Arguments) == 0 {
			return true
		}
	}
	return false
}

func repairStructuredToolCalls(streamed, repaired []models.ToolCall) []models.ToolCall {
	if len(streamed) == 0 {
		return append([]models.ToolCall(nil), repaired...)
	}

	out := append([]models.ToolCall(nil), streamed...)
	indexByID := make(map[string]int, len(out))
	for i, call := range out {
		if id := strings.TrimSpace(call.ID); id != "" {
			indexByID[id] = i
		}
	}

	for _, call := range repaired {
		id := strings.TrimSpace(call.ID)
		if id == "" {
			continue
		}
		idx, ok := indexByID[id]
		if !ok {
			continue
		}
		if out[idx].Name == "" {
			out[idx].Name = call.Name
		}
		if len(out[idx].Arguments) == 0 && len(call.Arguments) > 0 {
			out[idx].Arguments = call.Arguments
		}
		if out[idx].Status == "" && call.Status != "" {
			out[idx].Status = call.Status
		}
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
		InputTokens:       meta.Usage.PromptTokens,
		OutputTokens:      meta.Usage.CompletionTokens,
		TotalTokens:       meta.Usage.TotalTokens,
		ReasoningTokens:   meta.Usage.CompletionTokensDetails.ReasoningTokens,
		CachedInputTokens: meta.Usage.PromptTokenDetails.CachedTokens,
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
