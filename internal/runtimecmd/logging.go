package runtimecmd

import "fmt"

func (c NodeConfig) StartupLines() []string {
	return []string{
		fmt.Sprintf("runtime node starting role=%s", c.Role),
		fmt.Sprintf("  transport=%s endpoint=%s", c.TransportBackend, firstNonEmpty(c.Endpoint, "(local)")),
		fmt.Sprintf("  worker_addr=%s", c.Addr),
		fmt.Sprintf("  sandbox=%s", c.SandboxBackend),
		fmt.Sprintf("  memory_store=%s", firstNonEmpty(c.MemoryStoreURL, "(file-store)")),
		fmt.Sprintf("  state=%s snapshot=%s event=%s thread=%s root=%s store=%s", firstNonEmpty(string(c.StateBackend), "(default)"), firstNonEmpty(string(c.SnapshotBackend), "(default)"), firstNonEmpty(string(c.EventBackend), "(default)"), firstNonEmpty(string(c.ThreadBackend), "(default)"), firstNonEmpty(c.StateRoot, "(memory)"), firstNonEmpty(c.StateStoreURL, "(derived)")),
		fmt.Sprintf("  snapshot_store=%s event_store=%s thread_store=%s", firstNonEmpty(c.SnapshotStoreURL, "(derived)"), firstNonEmpty(c.EventStoreURL, "(derived)"), firstNonEmpty(c.ThreadStoreURL, "(derived)")),
	}
}
