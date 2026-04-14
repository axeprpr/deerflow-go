package sandboxcmd

import "fmt"

func (c Config) CLIArgs() []string {
	cfg := c.WithDefaults()
	args := []string{
		fmt.Sprintf("-addr=%s", cfg.Runtime.Addr),
		fmt.Sprintf("-name=%s", cfg.Runtime.Name),
		fmt.Sprintf("-root=%s", cfg.Runtime.Root),
		fmt.Sprintf("-data-root=%s", cfg.Runtime.DataRoot),
		fmt.Sprintf("-preset=%s", cfg.Runtime.Preset),
		fmt.Sprintf("-backend=%s", cfg.Runtime.SandboxBackend),
		fmt.Sprintf("-endpoint=%s", cfg.Runtime.SandboxEndpoint),
		fmt.Sprintf("-image=%s", cfg.Runtime.SandboxImage),
	}
	if cfg.Runtime.SandboxMaxActiveLeases > 0 {
		args = append(args, fmt.Sprintf("-max-active-leases=%d", cfg.Runtime.SandboxMaxActiveLeases))
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
