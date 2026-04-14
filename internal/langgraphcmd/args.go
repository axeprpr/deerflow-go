package langgraphcmd

import "fmt"

func (c Config) CLIArgs() []string {
	args := []string{
		fmt.Sprintf("-addr=%s", c.Addr),
		fmt.Sprintf("-provider=%s", c.Provider),
		fmt.Sprintf("-model=%s", c.Model),
	}
	if c.DatabaseURL != "" {
		args = append(args, fmt.Sprintf("-db=%s", c.DatabaseURL))
	}
	if c.AuthToken != "" {
		args = append(args, fmt.Sprintf("-auth-token=%s", c.AuthToken))
	}
	args = append(args, c.Runtime.CLIArgs("runtime-")...)
	return args
}
