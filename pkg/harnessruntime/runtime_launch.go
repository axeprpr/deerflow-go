package harnessruntime

type RuntimeNodeLaunchSpec struct {
	Role               RuntimeNodeRole
	ServesRemoteWorker bool
	RemoteWorkerAddr   string
}

func (n *RuntimeNode) LaunchSpec() RuntimeNodeLaunchSpec {
	if n == nil {
		return RuntimeNodeLaunchSpec{}
	}
	spec := RuntimeNodeLaunchSpec{
		Role: n.Config.Role,
	}
	if n.RemoteWorker != nil {
		spec.ServesRemoteWorker = true
		spec.RemoteWorkerAddr = n.RemoteWorker.Addr()
	}
	return spec
}
