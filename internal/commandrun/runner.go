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
	return RunWithReady(logger, lifecycle, nil, 0, shutdownTimeout, ignored...)
}

func RunWithReady(logger *log.Logger, lifecycle Lifecycle, ready ReadyFunc, readyTimeout time.Duration, shutdownTimeout time.Duration, ignored ...error) error {
	if lifecycle == nil {
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- lifecycle.Start()
	}()

	if ready != nil {
		if readyTimeout <= 0 {
			readyTimeout = 15 * time.Second
		}
		readyCtx, cancel := context.WithTimeout(ctx, readyTimeout)
		readyCh := make(chan error, 1)
		go func() {
			readyCh <- ready(readyCtx)
		}()
		select {
		case err := <-errCh:
			cancel()
			if shouldIgnore(err, ignored...) {
				return nil
			}
			return err
		case err := <-readyCh:
			cancel()
			if err != nil {
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
				defer shutdownCancel()
				_ = lifecycle.Close(shutdownCtx)
				return err
			}
		case <-ctx.Done():
			cancel()
			if logger != nil {
				logger.Println("Shutting down...")
			}
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer shutdownCancel()
			return lifecycle.Close(shutdownCtx)
		}
	}

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
