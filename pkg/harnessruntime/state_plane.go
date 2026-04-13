package harnessruntime

type RuntimeStatePlane struct {
	Snapshots RunSnapshotStore
	Events    RunEventStore
	Threads   ThreadStateStore
}

func (p RuntimeStatePlane) SnapshotStore() RunSnapshotStore {
	return p.Snapshots
}

func (p RuntimeStatePlane) EventStore() RunEventStore {
	return p.Events
}

func (p RuntimeStatePlane) ThreadStateStore() ThreadStateStore {
	return p.Threads
}
