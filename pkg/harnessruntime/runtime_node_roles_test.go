package harnessruntime

import "testing"

func TestBuildGatewayRuntimeNodeUsesGatewayRole(t *testing.T) {
	node, err := BuildGatewayRuntimeNode("runtime-test", t.TempDir(), "http://worker:8081/dispatch", nil, nil)
	if err != nil {
		t.Fatalf("BuildGatewayRuntimeNode() error = %v", err)
	}
	if node.Config.Role != RuntimeNodeRoleGateway {
		t.Fatalf("role = %q, want %q", node.Config.Role, RuntimeNodeRoleGateway)
	}
	if node.RemoteWorker != nil {
		t.Fatalf("remote worker = %#v, want nil", node.RemoteWorker)
	}
	if node.Dispatcher == nil {
		t.Fatal("dispatcher = nil")
	}
}

func TestBuildWorkerRuntimeNodeUsesWorkerRole(t *testing.T) {
	node, err := BuildWorkerRuntimeNode("runtime-test", t.TempDir(), nil, nil)
	if err != nil {
		t.Fatalf("BuildWorkerRuntimeNode() error = %v", err)
	}
	if node.Config.Role != RuntimeNodeRoleWorker {
		t.Fatalf("role = %q, want %q", node.Config.Role, RuntimeNodeRoleWorker)
	}
	if node.RemoteWorker == nil || node.RemoteWorker.Server() == nil {
		t.Fatalf("remote worker = %#v", node.RemoteWorker)
	}
	if node.Dispatcher == nil {
		t.Fatal("dispatcher = nil")
	}
}

func TestBuildAllInOneRuntimeNodeUsesAllInOneRole(t *testing.T) {
	node, err := BuildAllInOneRuntimeNode("runtime-test", t.TempDir(), nil, nil)
	if err != nil {
		t.Fatalf("BuildAllInOneRuntimeNode() error = %v", err)
	}
	if node.Config.Role != RuntimeNodeRoleAllInOne {
		t.Fatalf("role = %q, want %q", node.Config.Role, RuntimeNodeRoleAllInOne)
	}
	if node.RemoteWorker == nil || node.RemoteWorker.Server() == nil {
		t.Fatalf("remote worker = %#v", node.RemoteWorker)
	}
	if node.Dispatcher == nil {
		t.Fatal("dispatcher = nil")
	}
}

func TestBuildGatewayRuntimeNodeLauncherDoesNotExposeHandler(t *testing.T) {
	launcher, err := BuildGatewayRuntimeNodeLauncher("runtime-test", t.TempDir(), "http://worker:8081/dispatch", nil, nil)
	if err != nil {
		t.Fatalf("BuildGatewayRuntimeNodeLauncher() error = %v", err)
	}
	if launcher.Spec().Role != RuntimeNodeRoleGateway {
		t.Fatalf("role = %q, want %q", launcher.Spec().Role, RuntimeNodeRoleGateway)
	}
	if launcher.Handler() != nil {
		t.Fatalf("Handler() = %#v, want nil", launcher.Handler())
	}
}
