package statecmd

import "fmt"

func (c Config) CLIArgs() []string {
	cfg := c.WithDefaults()
	args := []string{
		fmt.Sprintf("-addr=%s", cfg.Runtime.Addr),
		fmt.Sprintf("-name=%s", cfg.Runtime.Name),
		fmt.Sprintf("-root=%s", cfg.Runtime.Root),
		fmt.Sprintf("-data-root=%s", cfg.Runtime.DataRoot),
		fmt.Sprintf("-preset=%s", cfg.Runtime.Preset),
		fmt.Sprintf("-state-provider=%s", cfg.Runtime.StateProvider),
		fmt.Sprintf("-state-root=%s", cfg.Runtime.StateRoot),
		fmt.Sprintf("-state-backend=%s", cfg.Runtime.StateBackend),
		fmt.Sprintf("-state-store=%s", cfg.Runtime.StateStoreURL),
		fmt.Sprintf("-snapshot-backend=%s", cfg.Runtime.SnapshotBackend),
		fmt.Sprintf("-event-backend=%s", cfg.Runtime.EventBackend),
		fmt.Sprintf("-thread-backend=%s", cfg.Runtime.ThreadBackend),
		fmt.Sprintf("-snapshot-store=%s", cfg.Runtime.SnapshotStoreURL),
		fmt.Sprintf("-event-store=%s", cfg.Runtime.EventStoreURL),
		fmt.Sprintf("-thread-store=%s", cfg.Runtime.ThreadStoreURL),
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
