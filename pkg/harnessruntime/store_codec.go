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

func (ThreadRuntimeStateCodec) Encode(state ThreadRuntimeState) ([]byte, error) {
	return json.MarshalIndent(state, "", "  ")
}

func (ThreadRuntimeStateCodec) Decode(data []byte) (ThreadRuntimeState, error) {
	var state ThreadRuntimeState
	err := json.Unmarshal(data, &state)
	return state, err
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
