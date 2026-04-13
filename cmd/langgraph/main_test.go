package main

import (
	"testing"

	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
)

func TestDefaultAddrUsesPortFallback(t *testing.T) {
	t.Setenv("ADDR", "")
	t.Setenv("PORT", "9090")
	if got := langgraphcmd.DefaultConfig().Addr; got != ":9090" {
		t.Fatalf("DefaultConfig().Addr = %q, want %q", got, ":9090")
	}
}
