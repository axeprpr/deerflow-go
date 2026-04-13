package harnessruntime

type RemoteSandboxProtocolFactory interface {
	Build(RuntimeNodeConfig) RemoteSandboxProtocol
}

type RemoteSandboxProtocolFactoryFunc func(RuntimeNodeConfig) RemoteSandboxProtocol

func (f RemoteSandboxProtocolFactoryFunc) Build(config RuntimeNodeConfig) RemoteSandboxProtocol {
	return f(config)
}

type RemoteSandboxHTTPServerFactory interface {
	Build(SandboxManagerConfig, RemoteSandboxProtocol) *HTTPRemoteSandboxServer
}

type RemoteSandboxHTTPServerFactoryFunc func(SandboxManagerConfig, RemoteSandboxProtocol) *HTTPRemoteSandboxServer

func (f RemoteSandboxHTTPServerFactoryFunc) Build(config SandboxManagerConfig, protocol RemoteSandboxProtocol) *HTTPRemoteSandboxServer {
	return f(config, protocol)
}

type RemoteSandboxProviders struct {
	Protocol RemoteSandboxProtocolFactory
	Server   RemoteSandboxHTTPServerFactory
}

func DefaultRemoteSandboxProviders() RemoteSandboxProviders {
	return RemoteSandboxProviders{
		Protocol: RemoteSandboxProtocolFactoryFunc(func(RuntimeNodeConfig) RemoteSandboxProtocol {
			return JSONRemoteSandboxProtocol{}
		}),
		Server: RemoteSandboxHTTPServerFactoryFunc(func(config SandboxManagerConfig, protocol RemoteSandboxProtocol) *HTTPRemoteSandboxServer {
			return NewHTTPRemoteSandboxServer(config, protocol)
		}),
	}
}
