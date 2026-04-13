package main

import (
	"testing"
)

func TestDefaultAddrUsesPortFallback(t *testing.T) {
	t.Setenv("ADDR", "")
	t.Setenv("PORT", "9090")
	if got := defaultAddr(); got != ":9090" {
		t.Fatalf("defaultAddr() = %q, want %q", got, ":9090")
	}
}
