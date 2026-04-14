package sandboxcmd

func (c Config) CLIArgs() []string {
	cfg := c.WithDefaults()
	return cfg.Runtime.CLIArgs("")
}
