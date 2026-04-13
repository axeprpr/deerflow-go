package harnessruntime

import "net/http"

type RemoteStateProtocolFactory interface {
	Build(RuntimeNodeConfig) RemoteStateProtocol
}

type RemoteStateProtocolFactoryFunc func(RuntimeNodeConfig) RemoteStateProtocol

func (f RemoteStateProtocolFactoryFunc) Build(config RuntimeNodeConfig) RemoteStateProtocol {
	return f(config)
}

type RemoteStateClientFactory interface {
	Build(RuntimeNodeConfig) *HTTPRemoteStateClient
}

type RemoteStateClientFactoryFunc func(RuntimeNodeConfig) *HTTPRemoteStateClient

func (f RemoteStateClientFactoryFunc) Build(config RuntimeNodeConfig) *HTTPRemoteStateClient {
	return f(config)
}

type RemoteStateHTTPServerFactory interface {
	Build(RuntimeStatePlane, RemoteStateProtocol) *HTTPRemoteStateServer
}

type RemoteStateHTTPServerFactoryFunc func(RuntimeStatePlane, RemoteStateProtocol) *HTTPRemoteStateServer

func (f RemoteStateHTTPServerFactoryFunc) Build(state RuntimeStatePlane, protocol RemoteStateProtocol) *HTTPRemoteStateServer {
	return f(state, protocol)
}

type RemoteStateProviders struct {
	Protocol RemoteStateProtocolFactory
	Client   RemoteStateClientFactory
	Server   RemoteStateHTTPServerFactory
}

func DefaultRemoteStateProviders() RemoteStateProviders {
	return RemoteStateProviders{
		Protocol: RemoteStateProtocolFactoryFunc(func(RuntimeNodeConfig) RemoteStateProtocol {
			return JSONRemoteStateProtocol{}
		}),
		Client: RemoteStateClientFactoryFunc(func(RuntimeNodeConfig) *HTTPRemoteStateClient {
			return NewHTTPRemoteStateClient(&http.Client{})
		}),
		Server: RemoteStateHTTPServerFactoryFunc(func(state RuntimeStatePlane, protocol RemoteStateProtocol) *HTTPRemoteStateServer {
			return NewHTTPRemoteStateServer(state, protocol)
		}),
	}
}
