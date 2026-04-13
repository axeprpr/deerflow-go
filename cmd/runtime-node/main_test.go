package main

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestNormalizeRole(t *testing.T) {
	tests := []struct {
		in   string
		want harnessruntime.RuntimeNodeRole
	}{
		{in: "worker", want: harnessruntime.RuntimeNodeRoleWorker},
		{in: "all-in-one", want: harnessruntime.RuntimeNodeRoleAllInOne},
		{in: "gateway", want: harnessruntime.RuntimeNodeRoleGateway},
		{in: "", want: harnessruntime.RuntimeNodeRoleWorker},
		{in: "unknown", want: harnessruntime.RuntimeNodeRoleWorker},
	}

	for _, tc := range tests {
		if got := normalizeRole(tc.in); got != tc.want {
			t.Fatalf("normalizeRole(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeAddr(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ":8081"},
		{in: ":9000", want: ":9000"},
		{in: "9000", want: ":9000"},
	}

	for _, tc := range tests {
		if got := normalizeAddr(tc.in); got != tc.want {
			t.Fatalf("normalizeAddr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
