package harnessruntime

import "path/filepath"

type RunSnapshotStoreFactory interface {
	Build(RuntimeNodeConfig) RunSnapshotStore
}

type RunSnapshotStoreFactoryFunc func(RuntimeNodeConfig) RunSnapshotStore

func (f RunSnapshotStoreFactoryFunc) Build(config RuntimeNodeConfig) RunSnapshotStore {
	return f(config)
}

type RunEventStoreFactory interface {
	Build(RuntimeNodeConfig) RunEventStore
}

type RunEventStoreFactoryFunc func(RuntimeNodeConfig) RunEventStore

func (f RunEventStoreFactoryFunc) Build(config RuntimeNodeConfig) RunEventStore {
	return f(config)
}

type ThreadStateStoreFactory interface {
	Build(RuntimeNodeConfig) ThreadStateStore
}

type ThreadStateStoreFactoryFunc func(RuntimeNodeConfig) ThreadStateStore

func (f ThreadStateStoreFactoryFunc) Build(config RuntimeNodeConfig) ThreadStateStore {
	return f(config)
}

type RuntimeStatePlaneProviders struct {
	Snapshots RunSnapshotStoreFactory
	Events    RunEventStoreFactory
	Threads   ThreadStateStoreFactory
}

func DefaultRuntimeStatePlaneProviders() RuntimeStatePlaneProviders {
	return RuntimeStatePlaneProviders{
		Snapshots: RunSnapshotStoreFactoryFunc(func(config RuntimeNodeConfig) RunSnapshotStore {
			switch config.normalizedSnapshotBackend() {
			case RuntimeStateStoreBackendFile:
				return NewJSONFileRunStore(filepath.Join(config.State.Root, "runs"))
			default:
				return NewInMemoryRunStore()
			}
		}),
		Events: RunEventStoreFactoryFunc(func(config RuntimeNodeConfig) RunEventStore {
			switch config.normalizedEventBackend() {
			case RuntimeStateStoreBackendFile:
				return NewJSONFileRunEventStore(filepath.Join(config.State.Root, "events"))
			default:
				return NewInMemoryRunEventStore()
			}
		}),
		Threads: ThreadStateStoreFactoryFunc(func(config RuntimeNodeConfig) ThreadStateStore {
			switch config.normalizedThreadBackend() {
			case RuntimeStateStoreBackendFile:
				return NewJSONFileThreadStateStore(filepath.Join(config.State.Root, "threads"))
			default:
				return NewInMemoryThreadStateStore()
			}
		}),
	}
}

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
