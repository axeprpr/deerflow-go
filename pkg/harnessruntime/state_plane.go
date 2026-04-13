package harnessruntime

import (
	"path/filepath"
	"strings"
)

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
	Plane     RuntimeStatePlaneFactory
	Snapshots RunSnapshotStoreFactory
	Events    RunEventStoreFactory
	Threads   ThreadStateStoreFactory
}

func DefaultRuntimeStatePlaneProviders() RuntimeStatePlaneProviders {
	return RuntimeStatePlaneProviders{
		Plane: RuntimeStatePlaneFactoryFunc(func(config RuntimeNodeConfig) RuntimeStatePlane {
			if path, ok := sharedSQLiteStatePlanePath(config); ok {
				plane, err := newSQLiteRuntimeStatePlane(path)
				if err == nil {
					return plane
				}
			}
			return RuntimeStatePlane{
				Snapshots: defaultRuntimeSnapshotStoreFactory().Build(config),
				Events:    defaultRuntimeEventStoreFactory().Build(config),
				Threads:   defaultRuntimeThreadStateStoreFactory().Build(config),
			}
		}),
		Snapshots: defaultRuntimeSnapshotStoreFactory(),
		Events:    defaultRuntimeEventStoreFactory(),
		Threads:   defaultRuntimeThreadStateStoreFactory(),
	}
}

func defaultRuntimeSnapshotStoreFactory() RunSnapshotStoreFactory {
	return RunSnapshotStoreFactoryFunc(func(config RuntimeNodeConfig) RunSnapshotStore {
		switch config.normalizedSnapshotBackend() {
		case RuntimeStateStoreBackendFile:
			return NewJSONFileRunStore(resolveStateStorePath(config.State.SnapshotURL, filepath.Join(config.State.Root, "runs")))
		case RuntimeStateStoreBackendSQLite:
			store, err := NewSQLiteRunSnapshotStore(resolveStateStorePath(config.State.SnapshotURL, filepath.Join(config.State.Root, "snapshots.sqlite3")))
			if err == nil {
				return store
			}
			return NewInMemoryRunStore()
		default:
			return NewInMemoryRunStore()
		}
	})
}

func defaultRuntimeEventStoreFactory() RunEventStoreFactory {
	return RunEventStoreFactoryFunc(func(config RuntimeNodeConfig) RunEventStore {
		switch config.normalizedEventBackend() {
		case RuntimeStateStoreBackendFile:
			return NewJSONFileRunEventStore(resolveStateStorePath(config.State.EventURL, filepath.Join(config.State.Root, "events")))
		case RuntimeStateStoreBackendSQLite:
			store, err := NewSQLiteRunEventStore(resolveStateStorePath(config.State.EventURL, filepath.Join(config.State.Root, "events.sqlite3")))
			if err == nil {
				return store
			}
			return NewInMemoryRunEventStore()
		default:
			return NewInMemoryRunEventStore()
		}
	})
}

func defaultRuntimeThreadStateStoreFactory() ThreadStateStoreFactory {
	return ThreadStateStoreFactoryFunc(func(config RuntimeNodeConfig) ThreadStateStore {
		switch config.normalizedThreadBackend() {
		case RuntimeStateStoreBackendFile:
			return NewJSONFileThreadStateStore(resolveStateStorePath(config.State.ThreadURL, filepath.Join(config.State.Root, "threads")))
		case RuntimeStateStoreBackendSQLite:
			store, err := NewSQLiteThreadStateStore(resolveStateStorePath(config.State.ThreadURL, filepath.Join(config.State.Root, "threads.sqlite3")))
			if err == nil {
				return store
			}
			return NewInMemoryThreadStateStore()
		default:
			return NewInMemoryThreadStateStore()
		}
	})
}

func sharedSQLiteStatePlanePath(config RuntimeNodeConfig) (string, bool) {
	if config.normalizedSnapshotBackend() != RuntimeStateStoreBackendSQLite ||
		config.normalizedEventBackend() != RuntimeStateStoreBackendSQLite ||
		config.normalizedThreadBackend() != RuntimeStateStoreBackendSQLite {
		return "", false
	}
	if shared := resolveStateStorePath(config.State.URL, ""); shared != "" {
		return shared, true
	}
	snapshotPath := resolveStateStorePath(config.State.SnapshotURL, filepath.Join(config.State.Root, "snapshots.sqlite3"))
	eventPath := resolveStateStorePath(config.State.EventURL, filepath.Join(config.State.Root, "events.sqlite3"))
	threadPath := resolveStateStorePath(config.State.ThreadURL, filepath.Join(config.State.Root, "threads.sqlite3"))
	if snapshotPath == "" || eventPath == "" || threadPath == "" {
		return "", false
	}
	if snapshotPath != eventPath || snapshotPath != threadPath {
		return "", false
	}
	return snapshotPath, true
}

func resolveStateStorePath(raw string, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	raw = strings.TrimPrefix(raw, "sqlite://")
	raw = strings.TrimPrefix(raw, "file://")
	if raw == "" {
		return fallback
	}
	return raw
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
