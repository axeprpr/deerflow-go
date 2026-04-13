package harnessruntime

import "github.com/axeprpr/deerflow-go/pkg/harness"

func BuildAllInOneRuntimeNode(name, root string, source func() *harness.Runtime, specs WorkerSpecRuntime) (*RuntimeNode, error) {
	return BuildAllInOneRuntimeNodeWithProviders(name, root, source, specs, DefaultRuntimeNodeProviders())
}

func BuildAllInOneRuntimeNodeWithProviders(name, root string, source func() *harness.Runtime, specs WorkerSpecRuntime, providers RuntimeNodeProviders) (*RuntimeNode, error) {
	config := DefaultRuntimeNodeConfig(name, root)
	return buildRoleRuntimeNode(config, source, specs, providers)
}

func BuildGatewayRuntimeNode(name, root, endpoint string, source func() *harness.Runtime, specs WorkerSpecRuntime) (*RuntimeNode, error) {
	return BuildGatewayRuntimeNodeWithProviders(name, root, endpoint, source, specs, DefaultRuntimeNodeProviders())
}

func BuildGatewayRuntimeNodeWithProviders(name, root, endpoint string, source func() *harness.Runtime, specs WorkerSpecRuntime, providers RuntimeNodeProviders) (*RuntimeNode, error) {
	config := DefaultGatewayRuntimeNodeConfig(name, root, endpoint)
	return buildRoleRuntimeNode(config, source, specs, providers)
}

func BuildWorkerRuntimeNode(name, root string, source func() *harness.Runtime, specs WorkerSpecRuntime) (*RuntimeNode, error) {
	return BuildWorkerRuntimeNodeWithProviders(name, root, source, specs, DefaultRuntimeNodeProviders())
}

func BuildWorkerRuntimeNodeWithProviders(name, root string, source func() *harness.Runtime, specs WorkerSpecRuntime, providers RuntimeNodeProviders) (*RuntimeNode, error) {
	config := DefaultWorkerRuntimeNodeConfig(name, root)
	return buildRoleRuntimeNode(config, source, specs, providers)
}

func buildRoleRuntimeNode(config RuntimeNodeConfig, source func() *harness.Runtime, specs WorkerSpecRuntime, providers RuntimeNodeProviders) (*RuntimeNode, error) {
	return config.BuildRuntimeNodeWithProviders(config.BuildDispatchRuntimeWithProviders(source, specs, providers.Remote), providers)
}
