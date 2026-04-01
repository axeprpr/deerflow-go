package main

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestRegisterBuiltinsIncludesWebTools(t *testing.T) {
	registry := tools.NewRegistry()

	registerBuiltins(registry)

	for _, name := range []string{"bash", "read_file", "write_file", "glob", "web_search", "web_fetch"} {
		if tool := registry.Get(name); tool == nil {
			t.Fatalf("tool %q was not registered", name)
		}
	}
}
