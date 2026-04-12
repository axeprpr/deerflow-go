package harnessruntime

import (
	"encoding/json"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type DispatchResultMarshaler interface {
	Encode(*DispatchResult) ([]byte, error)
	Decode([]byte) (*DispatchResult, error)
}

type portableRunLifecycle struct {
	ThreadID         string           `json:"thread_id"`
	AssistantID      string           `json:"assistant_id"`
	Model            string           `json:"model,omitempty"`
	AgentName        string           `json:"agent_name,omitempty"`
	ExistingMessages []models.Message `json:"existing_messages,omitempty"`
	Messages         []models.Message `json:"messages,omitempty"`
	Metadata         map[string]any   `json:"metadata,omitempty"`
}

type portableDispatchResult struct {
	Lifecycle portableRunLifecycle `json:"lifecycle"`
	Execution ExecutionDescriptor  `json:"execution"`
	HandleID  string               `json:"handle_id,omitempty"`
	EncodedAt time.Time            `json:"encoded_at,omitempty"`
}

type DispatchResultCodec struct {
	Handles ExecutionHandleRegistry
}

func defaultDispatchResultCodec(codec DispatchResultMarshaler) DispatchResultMarshaler {
	if codec != nil {
		return codec
	}
	return DispatchResultCodec{Handles: NewInMemoryExecutionHandleRegistry()}
}

func (c DispatchResultCodec) Encode(result *DispatchResult) ([]byte, error) {
	if result == nil {
		return json.Marshal(portableDispatchResult{})
	}
	var lifecycle portableRunLifecycle
	if result.Lifecycle != nil {
		lifecycle = portableRunLifecycle{
			ThreadID:         result.Lifecycle.ThreadID,
			AssistantID:      result.Lifecycle.AssistantID,
			Model:            result.Lifecycle.Model,
			AgentName:        result.Lifecycle.AgentName,
			ExistingMessages: append([]models.Message(nil), result.Lifecycle.ExistingMessages...),
			Messages:         append([]models.Message(nil), result.Lifecycle.Messages...),
			Metadata:         copyStringAnyMap(result.Lifecycle.Metadata),
		}
	}
	payload := portableDispatchResult{
		Lifecycle: lifecycle,
		Execution: result.Execution,
		EncodedAt: time.Now().UTC(),
	}
	if c.Handles != nil && result.Handle != nil {
		payload.HandleID = c.Handles.Register(result.Handle)
	}
	return json.Marshal(payload)
}

func (c DispatchResultCodec) Decode(data []byte) (*DispatchResult, error) {
	var payload portableDispatchResult
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	result := &DispatchResult{
		Lifecycle: &harness.RunState{
			ThreadID:         payload.Lifecycle.ThreadID,
			AssistantID:      payload.Lifecycle.AssistantID,
			Model:            payload.Lifecycle.Model,
			AgentName:        payload.Lifecycle.AgentName,
			ExistingMessages: append([]models.Message(nil), payload.Lifecycle.ExistingMessages...),
			Messages:         append([]models.Message(nil), payload.Lifecycle.Messages...),
			Metadata:         copyStringAnyMap(payload.Lifecycle.Metadata),
		},
		Execution: payload.Execution,
	}
	if c.Handles != nil && payload.HandleID != "" {
		if handle, ok := c.Handles.Resolve(payload.HandleID); ok {
			result.Handle = handle
		}
	}
	return result, nil
}

func copyStringAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
