package runtimecmd

func (c NodeConfig) StartupLines() []string {
	return c.Manifest().StartupLines()
}
