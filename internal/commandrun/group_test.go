package commandrun

import (
	"context"
	"errors"
	"testing"
)

type groupLifecycle struct {
	started *int
	closed  *int
	err     error
}

func (g groupLifecycle) Start() error {
	if g.started != nil {
		*g.started++
	}
	return g.err
}

func (g groupLifecycle) Close(context.Context) error {
	if g.closed != nil {
		*g.closed++
	}
	return g.err
}

func TestLifecycleGroupStartRunsAllLifecycles(t *testing.T) {
	started := 0
	group := NewLifecycleGroup(
		groupLifecycle{started: &started},
		groupLifecycle{started: &started},
	)
	if err := group.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if started != 2 {
		t.Fatalf("started = %d, want 2", started)
	}
}

func TestLifecycleGroupStartReturnsFirstError(t *testing.T) {
	started := 0
	want := errors.New("boom")
	group := NewLifecycleGroup(
		groupLifecycle{started: &started, err: want},
		groupLifecycle{started: &started},
	)
	if err := group.Start(); !errors.Is(err, want) {
		t.Fatalf("Start() error = %v, want %v", err, want)
	}
}

func TestLifecycleGroupCloseClosesAllLifecycles(t *testing.T) {
	closed := 0
	group := NewLifecycleGroup(
		groupLifecycle{closed: &closed},
		groupLifecycle{closed: &closed},
	)
	if err := group.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if closed != 2 {
		t.Fatalf("closed = %d, want 2", closed)
	}
}
