package stackcmd

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type fakeProcessRunner struct {
	startFn func() error
	closeFn func(context.Context) error
}

func (f fakeProcessRunner) Start() error {
	if f.startFn == nil {
		return nil
	}
	return f.startFn()
}

func (f fakeProcessRunner) Close(ctx context.Context) error {
	if f.closeFn == nil {
		return nil
	}
	return f.closeFn(ctx)
}

func TestRunProcessRunnersFailFastWithoutIsolation(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var closeCalls atomic.Int32
	wantErr := errors.New("runner failed")

	blocker := fakeProcessRunner{
		startFn: func() error {
			<-release
			return nil
		},
		closeFn: func(context.Context) error {
			if closeCalls.Add(1) == 1 {
				close(release)
			}
			return nil
		},
	}
	failing := fakeProcessRunner{
		startFn: func() error { return wantErr },
	}

	err := runProcessRunners([]processRunner{blocker, failing}, false)
	if !errors.Is(err, wantErr) {
		t.Fatalf("runProcessRunners() error = %v, want %v", err, wantErr)
	}
	if got := closeCalls.Load(); got == 0 {
		t.Fatalf("close calls = %d, want >= 1", got)
	}
}

func TestRunProcessRunnersIsolationKeepsOtherRunnersAlive(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	done := make(chan error, 1)
	var closeCalls atomic.Int32
	wantErr := errors.New("runner failed")

	blocker := fakeProcessRunner{
		startFn: func() error {
			<-release
			return nil
		},
		closeFn: func(context.Context) error {
			closeCalls.Add(1)
			close(release)
			return nil
		},
	}
	failing := fakeProcessRunner{
		startFn: func() error { return wantErr },
	}

	go func() {
		done <- runProcessRunners([]processRunner{blocker, failing}, true)
	}()

	select {
	case err := <-done:
		t.Fatalf("runProcessRunners() completed too early: %v", err)
	case <-time.After(80 * time.Millisecond):
	}
	if got := closeCalls.Load(); got != 0 {
		t.Fatalf("close calls = %d, want 0 while isolation is enabled", got)
	}

	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, wantErr) {
			t.Fatalf("runProcessRunners() error = %v, want %v", err, wantErr)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for isolated runProcessRunners completion")
	}
}
