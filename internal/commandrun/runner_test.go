package commandrun

import (
	"context"
	"errors"
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
