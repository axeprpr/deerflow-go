package harnessruntime

import "testing"

func TestDescribeSandboxProfileRemote(t *testing.T) {
	profile := DescribeSandboxProfile(SandboxManagerConfig{
		Backend:         SandboxBackendRemote,
		Endpoint:        "https://sandbox.internal",
		MaxActiveLeases: 11,
	})
	if profile.Backend != SandboxBackendRemote {
		t.Fatalf("backend = %q", profile.Backend)
	}
	if profile.Isolation != "remote-service" {
		t.Fatalf("isolation = %q", profile.Isolation)
	}
	if profile.Observability.HealthPath != DefaultRemoteSandboxHealthPath {
		t.Fatalf("health path = %q", profile.Observability.HealthPath)
	}
	if profile.Limits.Endpoint != "https://sandbox.internal" {
		t.Fatalf("endpoint = %q", profile.Limits.Endpoint)
	}
	if profile.Limits.MaxActiveLeases != 11 {
		t.Fatalf("max active leases = %d", profile.Limits.MaxActiveLeases)
	}
}

func TestDescribeSandboxProfileContainer(t *testing.T) {
	profile := DescribeSandboxProfile(SandboxManagerConfig{
		Backend: SandboxBackendContainer,
		Image:   "ghcr.io/example/sandbox:latest",
	})
	if profile.Backend != SandboxBackendContainer {
		t.Fatalf("backend = %q", profile.Backend)
	}
	if profile.Isolation != "container" {
		t.Fatalf("isolation = %q", profile.Isolation)
	}
	if profile.Limits.Image != "ghcr.io/example/sandbox:latest" {
		t.Fatalf("image = %q", profile.Limits.Image)
	}
}

func TestDescribeSandboxProfileWindowsRestricted(t *testing.T) {
	profile := DescribeSandboxProfile(SandboxManagerConfig{
		Backend: SandboxBackendWindowsRestricted,
	})
	if profile.Backend != SandboxBackendWindowsRestricted {
		t.Fatalf("backend = %q", profile.Backend)
	}
	if profile.Isolation != "windows-restricted" {
		t.Fatalf("isolation = %q", profile.Isolation)
	}
	if profile.Limits.HeartbeatIntervalMilli <= 0 {
		t.Fatalf("heartbeat = %d", profile.Limits.HeartbeatIntervalMilli)
	}
}
