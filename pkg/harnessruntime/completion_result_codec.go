package harnessruntime

import "encoding/json"

type CompletionResultMarshaler interface {
	Encode(CompletionResult) ([]byte, error)
	Decode([]byte) (CompletionResult, error)
}

type portableCompletionResult struct {
	Run         RunRecord            `json:"run"`
	Interrupted bool                 `json:"interrupted,omitempty"`
	Outcome     RunOutcomeDescriptor `json:"outcome"`
}

type CompletionResultCodec struct{}

func (CompletionResultCodec) Encode(result CompletionResult) ([]byte, error) {
	return json.Marshal(portableCompletionResult{
		Run:         result.Run,
		Interrupted: result.Interrupted,
		Outcome:     result.Outcome,
	})
}

func (CompletionResultCodec) Decode(data []byte) (CompletionResult, error) {
	var payload portableCompletionResult
	if err := json.Unmarshal(data, &payload); err != nil {
		return CompletionResult{}, err
	}
	return CompletionResult{
		Run:         payload.Run,
		Interrupted: payload.Interrupted,
		Outcome:     payload.Outcome,
	}, nil
}
