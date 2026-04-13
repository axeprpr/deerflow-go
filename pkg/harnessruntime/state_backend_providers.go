package harnessruntime

import "path/filepath"

type RuntimeStateBackendProvider interface {
	BuildPlane(RuntimeNodeConfig) (RuntimeStatePlane, bool)
	BuildSnapshotStore(RuntimeNodeConfig) RunSnapshotStore
	BuildEventStore(RuntimeNodeConfig) RunEventStore
	BuildThreadStateStore(RuntimeNodeConfig) ThreadStateStore
}

type RuntimeStateBackendProviders struct {
	Default  RuntimeStateBackendProvider
	InMemory RuntimeStateBackendProvider
	File     RuntimeStateBackendProvider
	SQLite   RuntimeStateBackendProvider
	Remote   RuntimeStateBackendProvider
}

func DefaultRuntimeStateBackendProviders() RuntimeStateBackendProviders {
	return RuntimeStateBackendProviders{
		InMemory: runtimeInMemoryStateBackendProvider{},
		File:     runtimeFileStateBackendProvider{},
		SQLite:   runtimeSQLiteStateBackendProvider{},
		Remote:   runtimeRemoteStateBackendProvider{providers: DefaultRemoteStateProviders()},
	}
}

func (p RuntimeStateBackendProviders) Resolve(backend RuntimeStateStoreBackend) RuntimeStateBackendProvider {
	switch backend {
	case RuntimeStateStoreBackendFile:
		if p.File != nil {
			return p.File
		}
	case RuntimeStateStoreBackendSQLite:
		if p.SQLite != nil {
			return p.SQLite
		}
	case RuntimeStateStoreBackendRemote:
		if p.Remote != nil {
			return p.Remote
		}
	default:
		if p.InMemory != nil {
			return p.InMemory
		}
	}
	if p.Default != nil {
		return p.Default
	}
	return runtimeInMemoryStateBackendProvider{}
}

type runtimeInMemoryStateBackendProvider struct{}

func (runtimeInMemoryStateBackendProvider) BuildPlane(RuntimeNodeConfig) (RuntimeStatePlane, bool) {
	return RuntimeStatePlane{}, false
}

func (runtimeInMemoryStateBackendProvider) BuildSnapshotStore(RuntimeNodeConfig) RunSnapshotStore {
	return NewInMemoryRunStore()
}

func (runtimeInMemoryStateBackendProvider) BuildEventStore(RuntimeNodeConfig) RunEventStore {
	return NewInMemoryRunEventStore()
}

func (runtimeInMemoryStateBackendProvider) BuildThreadStateStore(RuntimeNodeConfig) ThreadStateStore {
	return NewInMemoryThreadStateStore()
}

type runtimeFileStateBackendProvider struct{}

func (runtimeFileStateBackendProvider) BuildPlane(RuntimeNodeConfig) (RuntimeStatePlane, bool) {
	return RuntimeStatePlane{}, false
}

func (runtimeFileStateBackendProvider) BuildSnapshotStore(config RuntimeNodeConfig) RunSnapshotStore {
	return NewJSONFileRunStore(resolveStateStorePath(config.State.SnapshotURL, filepath.Join(config.State.Root, "runs")))
}

func (runtimeFileStateBackendProvider) BuildEventStore(config RuntimeNodeConfig) RunEventStore {
	return NewJSONFileRunEventStore(resolveStateStorePath(config.State.EventURL, filepath.Join(config.State.Root, "events")))
}

func (runtimeFileStateBackendProvider) BuildThreadStateStore(config RuntimeNodeConfig) ThreadStateStore {
	return NewJSONFileThreadStateStore(resolveStateStorePath(config.State.ThreadURL, filepath.Join(config.State.Root, "threads")))
}

type runtimeSQLiteStateBackendProvider struct{}

func (runtimeSQLiteStateBackendProvider) BuildPlane(config RuntimeNodeConfig) (RuntimeStatePlane, bool) {
	if path, ok := sharedSQLiteStatePlanePath(config); ok {
		plane, err := newSQLiteRuntimeStatePlane(path)
		if err == nil {
			return plane, true
		}
	}
	return RuntimeStatePlane{}, false
}

func (runtimeSQLiteStateBackendProvider) BuildSnapshotStore(config RuntimeNodeConfig) RunSnapshotStore {
	store, err := NewSQLiteRunSnapshotStore(resolveStateStorePath(config.State.SnapshotURL, filepath.Join(config.State.Root, "snapshots.sqlite3")))
	if err == nil {
		return store
	}
	return NewInMemoryRunStore()
}

func (runtimeSQLiteStateBackendProvider) BuildEventStore(config RuntimeNodeConfig) RunEventStore {
	store, err := NewSQLiteRunEventStore(resolveStateStorePath(config.State.EventURL, filepath.Join(config.State.Root, "events.sqlite3")))
	if err == nil {
		return store
	}
	return NewInMemoryRunEventStore()
}

func (runtimeSQLiteStateBackendProvider) BuildThreadStateStore(config RuntimeNodeConfig) ThreadStateStore {
	store, err := NewSQLiteThreadStateStore(resolveStateStorePath(config.State.ThreadURL, filepath.Join(config.State.Root, "threads.sqlite3")))
	if err == nil {
		return store
	}
	return NewInMemoryThreadStateStore()
}

type runtimeRemoteStateBackendProvider struct {
	providers RemoteStateProviders
}

func (p runtimeRemoteStateBackendProvider) BuildPlane(config RuntimeNodeConfig) (RuntimeStatePlane, bool) {
	if config.normalizedSnapshotBackend() != RuntimeStateStoreBackendRemote ||
		config.normalizedEventBackend() != RuntimeStateStoreBackendRemote ||
		config.normalizedThreadBackend() != RuntimeStateStoreBackendRemote {
		return RuntimeStatePlane{}, false
	}
	endpoint := resolveStateStoreURL(config.State.URL, "")
	if endpoint == "" {
		return RuntimeStatePlane{}, false
	}
	client := p.providers.Client.Build(config)
	protocol := p.providers.Protocol.Build(config)
	return RuntimeStatePlane{
		Snapshots: NewRemoteRunSnapshotStore(endpoint, client, protocol),
		Events:    NewRemoteRunEventStore(endpoint, client, protocol),
		Threads:   NewRemoteThreadStateStore(endpoint, client, protocol),
	}, true
}

func (p runtimeRemoteStateBackendProvider) BuildSnapshotStore(config RuntimeNodeConfig) RunSnapshotStore {
	return NewRemoteRunSnapshotStore(resolveStateStoreURL(config.State.SnapshotURL, resolveStateStoreURL(config.State.URL, "")), p.providers.Client.Build(config), p.providers.Protocol.Build(config))
}

func (p runtimeRemoteStateBackendProvider) BuildEventStore(config RuntimeNodeConfig) RunEventStore {
	return NewRemoteRunEventStore(resolveStateStoreURL(config.State.EventURL, resolveStateStoreURL(config.State.URL, "")), p.providers.Client.Build(config), p.providers.Protocol.Build(config))
}

func (p runtimeRemoteStateBackendProvider) BuildThreadStateStore(config RuntimeNodeConfig) ThreadStateStore {
	return NewRemoteThreadStateStore(resolveStateStoreURL(config.State.ThreadURL, resolveStateStoreURL(config.State.URL, "")), p.providers.Client.Build(config), p.providers.Protocol.Build(config))
}
