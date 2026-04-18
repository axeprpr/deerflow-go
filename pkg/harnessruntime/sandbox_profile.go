package harnessruntime

import "strings"

type SandboxProfile struct {
	Backend       SandboxBackend              `json:"backend"`
	Isolation     string                      `json:"isolation"`
	Capabilities  []string                    `json:"capabilities"`
	Limits        SandboxProfileLimits        `json:"limits"`
	Observability SandboxProfileObservability `json:"observability"`
}

type SandboxProfileLimits struct {
	HeartbeatIntervalMilli int64  `json:"heartbeat_interval_ms,omitempty"`
	IdleTTLMilli           int64  `json:"idle_ttl_ms,omitempty"`
	SweepIntervalMilli     int64  `json:"sweep_interval_ms,omitempty"`
	MaxActiveLeases        int    `json:"max_active_leases,omitempty"`
	Endpoint               string `json:"endpoint,omitempty"`
	Image                  string `json:"image,omitempty"`
}

type SandboxProfileObservability struct {
	HealthPath string `json:"health_path,omitempty"`
	LeasePath  string `json:"lease_path,omitempty"`
}

func DescribeSandboxProfile(config SandboxManagerConfig) SandboxProfile {
	normalized := config.Normalized()
	backend := normalized.Backend
	if backend == "" {
		backend = SandboxBackendLocalLinux
	}

	profile := SandboxProfile{
		Backend:      backend,
		Capabilities: []string{"exec", "read_file", "write_file", "heartbeat", "release"},
		Limits: SandboxProfileLimits{
			HeartbeatIntervalMilli: normalized.HeartbeatInterval.Milliseconds(),
			IdleTTLMilli:           normalized.IdleTTL.Milliseconds(),
			SweepIntervalMilli:     normalized.SweepInterval.Milliseconds(),
			MaxActiveLeases:        normalized.MaxActiveLeases,
			Endpoint:               strings.TrimSpace(normalized.Endpoint),
			Image:                  strings.TrimSpace(normalized.Image),
		},
	}

	switch backend {
	case SandboxBackendContainer:
		profile.Isolation = "container"
		profile.Capabilities = append(profile.Capabilities, "image-sandbox")
	case SandboxBackendRemote:
		profile.Isolation = "remote-service"
		profile.Capabilities = append(profile.Capabilities, "http-lease")
		profile.Observability = SandboxProfileObservability{
			HealthPath: DefaultRemoteSandboxHealthPath,
			LeasePath:  DefaultRemoteSandboxLeasePath,
		}
	case SandboxBackendWSL2:
		profile.Isolation = "wsl2"
		profile.Capabilities = append(profile.Capabilities, "wsl-kernel-isolation")
	case SandboxBackendWindowsRestricted:
		profile.Isolation = "windows-restricted"
		profile.Capabilities = append(profile.Capabilities, "restricted-host")
	default:
		profile.Isolation = "host-local"
		profile.Capabilities = append(profile.Capabilities, "local-provider")
	}

	if profile.Limits.HeartbeatIntervalMilli <= 0 || profile.Limits.IdleTTLMilli <= 0 || profile.Limits.SweepIntervalMilli <= 0 {
		defaults := normalizeSandboxLeaseConfig(SandboxLeaseConfig{
			HeartbeatInterval: normalized.HeartbeatInterval,
			IdleTTL:           normalized.IdleTTL,
			SweepInterval:     normalized.SweepInterval,
		})
		profile.Limits.HeartbeatIntervalMilli = defaults.HeartbeatInterval.Milliseconds()
		profile.Limits.IdleTTLMilli = defaults.IdleTTL.Milliseconds()
		profile.Limits.SweepIntervalMilli = defaults.SweepInterval.Milliseconds()
	}

	return profile
}
