package harnessruntime

import "encoding/json"

type RemoteWorkerRequest struct {
	Envelope WorkerDispatchEnvelope `json:"envelope"`
}

type RemoteWorkerResponse struct {
	Result []byte `json:"result"`
}

type JSONRemoteWorkerProtocol struct {
	Results DispatchResultMarshaler
}

func defaultRemoteWorkerProtocol(protocol RemoteWorkerProtocol, results DispatchResultMarshaler) RemoteWorkerProtocol {
	if protocol != nil {
		return protocol
	}
	return JSONRemoteWorkerProtocol{Results: defaultDispatchResultCodec(results)}
}

func (p JSONRemoteWorkerProtocol) EncodeRequest(env WorkerDispatchEnvelope) ([]byte, error) {
	return json.Marshal(RemoteWorkerRequest{Envelope: env})
}

func (p JSONRemoteWorkerProtocol) DecodeResponse(data []byte) (*DispatchResult, error) {
	var response RemoteWorkerResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	results := defaultDispatchResultCodec(p.Results)
	return results.Decode(response.Result)
}
