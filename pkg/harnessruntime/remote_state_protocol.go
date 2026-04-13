package harnessruntime

import "encoding/json"

const (
	DefaultRemoteStateBasePath      = "/state"
	DefaultRemoteStateHealthPath    = DefaultRemoteStateBasePath + "/health"
	DefaultRemoteStateSnapshotsPath = DefaultRemoteStateBasePath + "/snapshots"
	DefaultRemoteStateEventsPath    = DefaultRemoteStateBasePath + "/events"
	DefaultRemoteStateThreadsPath   = DefaultRemoteStateBasePath + "/threads"
)

type RemoteStateProtocol interface {
	EncodeRunSnapshot(RunSnapshot) ([]byte, error)
	DecodeRunSnapshot([]byte) (RunSnapshot, error)
	EncodeRunSnapshots([]RunSnapshot) ([]byte, error)
	DecodeRunSnapshots([]byte) ([]RunSnapshot, error)
	EncodeRunEvent(RunEvent) ([]byte, error)
	DecodeRunEvent([]byte) (RunEvent, error)
	EncodeRunEvents([]RunEvent) ([]byte, error)
	DecodeRunEvents([]byte) ([]RunEvent, error)
	EncodeThreadRuntimeState(ThreadRuntimeState) ([]byte, error)
	DecodeThreadRuntimeState([]byte) (ThreadRuntimeState, error)
	EncodeThreadRuntimeStates([]ThreadRuntimeState) ([]byte, error)
	DecodeThreadRuntimeStates([]byte) ([]ThreadRuntimeState, error)
	EncodeInt(int) ([]byte, error)
	DecodeInt([]byte) (int, error)
	EncodeAny(any) ([]byte, error)
	DecodeAny([]byte) (any, error)
}

type JSONRemoteStateProtocol struct{}

func (JSONRemoteStateProtocol) EncodeRunSnapshot(snapshot RunSnapshot) ([]byte, error) {
	return json.Marshal(snapshot)
}

func (JSONRemoteStateProtocol) DecodeRunSnapshot(data []byte) (RunSnapshot, error) {
	var snapshot RunSnapshot
	err := json.Unmarshal(data, &snapshot)
	return snapshot, err
}

func (JSONRemoteStateProtocol) EncodeRunSnapshots(snapshots []RunSnapshot) ([]byte, error) {
	return json.Marshal(snapshots)
}

func (JSONRemoteStateProtocol) DecodeRunSnapshots(data []byte) ([]RunSnapshot, error) {
	var snapshots []RunSnapshot
	err := json.Unmarshal(data, &snapshots)
	return snapshots, err
}

func (JSONRemoteStateProtocol) EncodeRunEvent(event RunEvent) ([]byte, error) {
	return json.Marshal(event)
}

func (JSONRemoteStateProtocol) DecodeRunEvent(data []byte) (RunEvent, error) {
	var event RunEvent
	err := json.Unmarshal(data, &event)
	return event, err
}

func (JSONRemoteStateProtocol) EncodeRunEvents(events []RunEvent) ([]byte, error) {
	return json.Marshal(events)
}

func (JSONRemoteStateProtocol) DecodeRunEvents(data []byte) ([]RunEvent, error) {
	var events []RunEvent
	err := json.Unmarshal(data, &events)
	return events, err
}

func (JSONRemoteStateProtocol) EncodeThreadRuntimeState(state ThreadRuntimeState) ([]byte, error) {
	return json.Marshal(state)
}

func (JSONRemoteStateProtocol) DecodeThreadRuntimeState(data []byte) (ThreadRuntimeState, error) {
	var state ThreadRuntimeState
	err := json.Unmarshal(data, &state)
	return state, err
}

func (JSONRemoteStateProtocol) EncodeThreadRuntimeStates(states []ThreadRuntimeState) ([]byte, error) {
	return json.Marshal(states)
}

func (JSONRemoteStateProtocol) DecodeThreadRuntimeStates(data []byte) ([]ThreadRuntimeState, error) {
	var states []ThreadRuntimeState
	err := json.Unmarshal(data, &states)
	return states, err
}

func (JSONRemoteStateProtocol) EncodeInt(value int) ([]byte, error) {
	return json.Marshal(struct {
		Value int `json:"value"`
	}{Value: value})
}

func (JSONRemoteStateProtocol) DecodeInt(data []byte) (int, error) {
	var payload struct {
		Value int `json:"value"`
	}
	err := json.Unmarshal(data, &payload)
	return payload.Value, err
}

func (JSONRemoteStateProtocol) EncodeAny(value any) ([]byte, error) {
	return json.Marshal(struct {
		Value any `json:"value"`
	}{Value: value})
}

func (JSONRemoteStateProtocol) DecodeAny(data []byte) (any, error) {
	var payload struct {
		Value any `json:"value"`
	}
	err := json.Unmarshal(data, &payload)
	return payload.Value, err
}

func defaultRemoteStateProtocol(protocol RemoteStateProtocol) RemoteStateProtocol {
	if protocol != nil {
		return protocol
	}
	return JSONRemoteStateProtocol{}
}
