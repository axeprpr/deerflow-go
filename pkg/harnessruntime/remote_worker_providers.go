package harnessruntime

import (
	"net/http"
	"time"
)

type RemoteWorkerClientFactory interface {
	Build(RuntimeNodeConfig) RemoteWorkerClient
}

type RemoteWorkerClientFactoryFunc func(RuntimeNodeConfig) RemoteWorkerClient

func (f RemoteWorkerClientFactoryFunc) Build(config RuntimeNodeConfig) RemoteWorkerClient {
	return f(config)
}

type RemoteWorkerProtocolFactory interface {
	Build(RuntimeNodeConfig, DispatchResultMarshaler) RemoteWorkerProtocol
}

type RemoteWorkerProtocolFactoryFunc func(RuntimeNodeConfig, DispatchResultMarshaler) RemoteWorkerProtocol

func (f RemoteWorkerProtocolFactoryFunc) Build(config RuntimeNodeConfig, results DispatchResultMarshaler) RemoteWorkerProtocol {
	return f(config, results)
}

type RemoteWorkerHTTPServerFactory interface {
	Build(RemoteWorkerServerConfig, WorkerTransport, RemoteWorkerProtocol) *http.Server
}

type RemoteWorkerHTTPServerFactoryFunc func(RemoteWorkerServerConfig, WorkerTransport, RemoteWorkerProtocol) *http.Server

func (f RemoteWorkerHTTPServerFactoryFunc) Build(config RemoteWorkerServerConfig, transport WorkerTransport, protocol RemoteWorkerProtocol) *http.Server {
	return f(config, transport, protocol)
}

type RemoteWorkerProviders struct {
	Client   RemoteWorkerClientFactory
	Protocol RemoteWorkerProtocolFactory
	Server   RemoteWorkerHTTPServerFactory
}

func DefaultRemoteWorkerProviders() RemoteWorkerProviders {
	return RemoteWorkerProviders{
		Client: RemoteWorkerClientFactoryFunc(func(RuntimeNodeConfig) RemoteWorkerClient {
			return NewHTTPRemoteWorkerClient(nil)
		}),
		Protocol: RemoteWorkerProtocolFactoryFunc(func(_ RuntimeNodeConfig, results DispatchResultMarshaler) RemoteWorkerProtocol {
			return JSONRemoteWorkerProtocol{Results: defaultDispatchResultCodec(results)}
		}),
		Server: RemoteWorkerHTTPServerFactoryFunc(func(config RemoteWorkerServerConfig, transport WorkerTransport, protocol RemoteWorkerProtocol) *http.Server {
			addr := config.Addr
			if addr == "" {
				addr = ":8081"
			}
			timeout := config.ReadHeaderTimeout
			if timeout <= 0 {
				timeout = 10 * time.Second
			}
			return &http.Server{
				Addr:              addr,
				Handler:           NewHTTPRemoteWorkerServer(transport, protocol).Handler(),
				ReadHeaderTimeout: timeout,
			}
		}),
	}
}
