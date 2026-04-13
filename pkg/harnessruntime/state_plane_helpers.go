package harnessruntime

func (p RuntimeStatePlaneProviders) normalized() RuntimeStatePlaneProviders {
	defaults := DefaultRuntimeStatePlaneProviders()
	if p.Backends.Default == nil {
		p.Backends.Default = defaults.Backends.Default
	}
	if p.Backends.InMemory == nil {
		p.Backends.InMemory = defaults.Backends.InMemory
	}
	if p.Backends.File == nil {
		p.Backends.File = defaults.Backends.File
	}
	if p.Backends.SQLite == nil {
		p.Backends.SQLite = defaults.Backends.SQLite
	}
	return p
}

func (p RuntimeStatePlaneProviders) buildPlane(config RuntimeNodeConfig) RuntimeStatePlane {
	p = p.normalized()
	if p.Plane != nil {
		plane := p.Plane.Build(config)
		if plane.Snapshots != nil && plane.Events != nil && plane.Threads != nil {
			return plane
		}
	}
	if config.normalizedSnapshotBackend() == config.normalizedEventBackend() &&
		config.normalizedEventBackend() == config.normalizedThreadBackend() {
		if plane, ok := p.Backends.Resolve(config.normalizedSnapshotBackend()).BuildPlane(config); ok {
			return plane
		}
	}
	return RuntimeStatePlane{
		Snapshots: p.buildSnapshotStore(config),
		Events:    p.buildEventStore(config),
		Threads:   p.buildThreadStateStore(config),
	}
}

func (p RuntimeStatePlaneProviders) buildSnapshotStore(config RuntimeNodeConfig) RunSnapshotStore {
	p = p.normalized()
	if p.Snapshots != nil {
		return p.Snapshots.Build(config)
	}
	return p.Backends.Resolve(config.normalizedSnapshotBackend()).BuildSnapshotStore(config)
}

func (p RuntimeStatePlaneProviders) buildEventStore(config RuntimeNodeConfig) RunEventStore {
	p = p.normalized()
	if p.Events != nil {
		return p.Events.Build(config)
	}
	return p.Backends.Resolve(config.normalizedEventBackend()).BuildEventStore(config)
}

func (p RuntimeStatePlaneProviders) buildThreadStateStore(config RuntimeNodeConfig) ThreadStateStore {
	p = p.normalized()
	if p.Threads != nil {
		return p.Threads.Build(config)
	}
	return p.Backends.Resolve(config.normalizedThreadBackend()).BuildThreadStateStore(config)
}
