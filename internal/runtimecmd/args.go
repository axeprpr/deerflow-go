package runtimecmd

import "fmt"

func (c NodeConfig) CLIArgs(prefix string) []string {
	args := []string{
		fmt.Sprintf("-%spreset=%s", prefix, c.Preset),
		fmt.Sprintf("-%sstate-provider=%s", prefix, c.StateProvider),
		fmt.Sprintf("-%srole=%s", prefix, c.Role),
		fmt.Sprintf("-%saddr=%s", prefix, c.Addr),
		fmt.Sprintf("-%sname=%s", prefix, c.Name),
		fmt.Sprintf("-%sroot=%s", prefix, c.Root),
		fmt.Sprintf("-%sdata-root=%s", prefix, c.DataRoot),
		fmt.Sprintf("-%sprovider=%s", prefix, c.Provider),
		fmt.Sprintf("-%smax-turns=%d", prefix, c.MaxTurns),
		fmt.Sprintf("-%stransport-backend=%s", prefix, c.TransportBackend),
		fmt.Sprintf("-%ssandbox-backend=%s", prefix, c.SandboxBackend),
		fmt.Sprintf("-%smemory-store=%s", prefix, c.MemoryStoreURL),
		fmt.Sprintf("-%sstate-root=%s", prefix, c.StateRoot),
		fmt.Sprintf("-%sstate-backend=%s", prefix, c.StateBackend),
		fmt.Sprintf("-%sstate-store=%s", prefix, c.StateStoreURL),
		fmt.Sprintf("-%ssnapshot-backend=%s", prefix, c.SnapshotBackend),
		fmt.Sprintf("-%sevent-backend=%s", prefix, c.EventBackend),
		fmt.Sprintf("-%sthread-backend=%s", prefix, c.ThreadBackend),
		fmt.Sprintf("-%ssnapshot-store=%s", prefix, c.SnapshotStoreURL),
		fmt.Sprintf("-%sevent-store=%s", prefix, c.EventStoreURL),
		fmt.Sprintf("-%sthread-store=%s", prefix, c.ThreadStoreURL),
	}
	if c.Endpoint != "" {
		args = append(args, fmt.Sprintf("-%sendpoint=%s", prefix, c.Endpoint))
	}
	if c.SandboxEndpoint != "" {
		args = append(args, fmt.Sprintf("-%ssandbox-endpoint=%s", prefix, c.SandboxEndpoint))
	}
	if c.SandboxImage != "" {
		args = append(args, fmt.Sprintf("-%ssandbox-image=%s", prefix, c.SandboxImage))
	}
	if c.SandboxMaxActiveLeases > 0 {
		args = append(args, fmt.Sprintf("-%ssandbox-max-active-leases=%d", prefix, c.SandboxMaxActiveLeases))
	}
	return compactArgs(args)
}

func compactArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if len(arg) == 0 || hasEmptyFlagValue(arg) {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func hasEmptyFlagValue(arg string) bool {
	for i := 0; i < len(arg); i++ {
		if arg[i] == '=' {
			return i == len(arg)-1
		}
	}
	return false
}
