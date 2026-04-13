package commandrun

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Lifecycle interface {
	Start() error
	Close(context.Context) error
}

func Run(logger *log.Logger, lifecycle Lifecycle, shutdownTimeout time.Duration, ignored ...error) error {
	if lifecycle == nil {
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- lifecycle.Start()
	}()

	select {
	case err := <-errCh:
		if shouldIgnore(err, ignored...) {
			return nil
		}
		return err
	case <-ctx.Done():
		if logger != nil {
			logger.Println("Shutting down...")
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return lifecycle.Close(shutdownCtx)
	}
}

func shouldIgnore(err error, ignored ...error) bool {
	if err == nil {
		return true
	}
	for _, target := range ignored {
		if target != nil && errors.Is(err, target) {
			return true
		}
	}
	return false
}
