package harnessruntime

import "encoding/json"

type RunSnapshotMarshaler interface {
	Encode(RunSnapshot) ([]byte, error)
	Decode([]byte) (RunSnapshot, error)
}

type ThreadRuntimeStateMarshaler interface {
	Encode(ThreadRuntimeState) ([]byte, error)
	Decode([]byte) (ThreadRuntimeState, error)
}

type RunEventLogMarshaler interface {
	Encode([]RunEvent) ([]byte, error)
	Decode([]byte) ([]RunEvent, error)
}

type RunSnapshotCodec struct{}

func (RunSnapshotCodec) Encode(snapshot RunSnapshot) ([]byte, error) {
	return json.MarshalIndent(snapshot, "", "  ")
}

func (RunSnapshotCodec) Decode(data []byte) (RunSnapshot, error) {
	var snapshot RunSnapshot
	err := json.Unmarshal(data, &snapshot)
	return snapshot, err
}

type ThreadRuntimeStateCodec struct{}
type RunEventLogCodec struct{}

func (ThreadRuntimeStateCodec) Encode(state ThreadRuntimeState) ([]byte, error) {
	return json.MarshalIndent(state, "", "  ")
}

func (ThreadRuntimeStateCodec) Decode(data []byte) (ThreadRuntimeState, error) {
	var state ThreadRuntimeState
	err := json.Unmarshal(data, &state)
	return state, err
}

func (RunEventLogCodec) Encode(events []RunEvent) ([]byte, error) {
	return json.MarshalIndent(events, "", "  ")
}

func (RunEventLogCodec) Decode(data []byte) ([]RunEvent, error) {
	var events []RunEvent
	err := json.Unmarshal(data, &events)
	return events, err
}

func defaultRunSnapshotCodec(codec RunSnapshotMarshaler) RunSnapshotMarshaler {
	if codec != nil {
		return codec
	}
	return RunSnapshotCodec{}
}

func defaultThreadRuntimeStateCodec(codec ThreadRuntimeStateMarshaler) ThreadRuntimeStateMarshaler {
	if codec != nil {
		return codec
	}
	return ThreadRuntimeStateCodec{}
}

func defaultRunEventLogCodec(codec RunEventLogMarshaler) RunEventLogMarshaler {
	if codec != nil {
		return codec
	}
	return RunEventLogCodec{}
}
