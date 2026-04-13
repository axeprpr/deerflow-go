package commandrun

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeLifecycle struct {
	startErr error
}

func (f fakeLifecycle) Start() error {
	return f.startErr
}

func (f fakeLifecycle) Close(context.Context) error {
	return nil
}

type blockingLifecycle struct {
	done   chan struct{}
	closed atomic.Bool
}

func (b *blockingLifecycle) Start() error {
	<-b.done
	return nil
}

func (b *blockingLifecycle) Close(context.Context) error {
	if b.closed.CompareAndSwap(false, true) {
		close(b.done)
	}
	return nil
}

func TestRunIgnoresConfiguredTerminalErrors(t *testing.T) {
	if err := Run(nil, fakeLifecycle{startErr: context.Canceled}, 10*time.Millisecond, context.Canceled); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunReturnsUnexpectedErrors(t *testing.T) {
	want := errors.New("boom")
	if err := Run(nil, fakeLifecycle{startErr: want}, 10*time.Millisecond, context.Canceled); !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
}

func TestRunWithReadyWaitsForReady(t *testing.T) {
	lifecycle := &blockingLifecycle{done: make(chan struct{})}
	var readyCalled atomic.Bool
	var logs bytes.Buffer
	logger := log.New(&logs, "", 0)

	done := make(chan error, 1)
	go func() {
		done <- PreparedCommand{
			Logger:       logger,
			Lifecycle:    lifecycle,
			StartupLines: []string{"starting"},
			ReadyLines:   []string{"ready"},
			ReadyTimeout: time.Second,
			Ready: func(context.Context) error {
				readyCalled.Store(true)
				close(lifecycle.done)
				return nil
			},
		}.Run()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("PreparedCommand.Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("PreparedCommand.Run() timed out")
	}

	if !readyCalled.Load() {
		t.Fatal("ready probe was not called")
	}
	if got := logs.String(); !strings.Contains(got, "starting") || !strings.Contains(got, "ready") {
		t.Fatalf("logs = %q", got)
	}
	if strings.Index(logs.String(), "starting") > strings.Index(logs.String(), "ready") {
		t.Fatalf("logs out of order = %q", logs.String())
	}
}

func TestRunWithReadyClosesLifecycleOnReadyFailure(t *testing.T) {
	lifecycle := &blockingLifecycle{done: make(chan struct{})}
	want := errors.New("not ready")
	err := PreparedCommand{
		Lifecycle:    lifecycle,
		ReadyTimeout: 100 * time.Millisecond,
		Ready: func(context.Context) error {
			return want
		},
	}.Run()
	if !errors.Is(err, want) {
		t.Fatalf("PreparedCommand.Run() error = %v, want %v", err, want)
	}
	if !lifecycle.closed.Load() {
		t.Fatal("lifecycle was not closed after readiness failure")
	}
}
