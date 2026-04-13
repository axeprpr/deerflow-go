package harnessruntime

import (
	"encoding/json"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

func TestJSONRemoteWorkerProtocolRoundTrip(t *testing.T) {
	resultCodec := &fakeResultCodec{
		last: &DispatchResult{
			Lifecycle: &harness.RunState{ThreadID: "thread-1"},
			Execution: ExecutionDescriptor{Kind: ExecutionKindLocalPrepared, SessionID: "thread-1"},
		},
	}
	protocol := JSONRemoteWorkerProtocol{Results: resultCodec}

	request, err := protocol.EncodeRequest(WorkerDispatchEnvelope{
		RunID:    "run-1",
		ThreadID: "thread-1",
		Attempt:  2,
		Payload:  []byte("plan"),
	})
	if err != nil {
		t.Fatalf("EncodeRequest() error = %v", err)
	}
	var decodedRequest RemoteWorkerRequest
	if err := json.Unmarshal(request, &decodedRequest); err != nil {
		t.Fatalf("Unmarshal(request) error = %v", err)
	}
	if decodedRequest.Envelope.RunID != "run-1" || decodedRequest.Envelope.Attempt != 2 {
		t.Fatalf("decoded request = %+v", decodedRequest)
	}
	envelope, err := protocol.DecodeRequest(request)
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}
	if envelope.RunID != "run-1" || envelope.Attempt != 2 {
		t.Fatalf("envelope = %+v", envelope)
	}

	response, err := protocol.EncodeResponse(resultCodec.last)
	if err != nil {
		t.Fatalf("EncodeResponse() error = %v", err)
	}
	result, err := protocol.DecodeResponse(response)
	if err != nil {
		t.Fatalf("DecodeResponse() error = %v", err)
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-1" {
		t.Fatalf("result = %#v", result)
	}
}
