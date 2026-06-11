package asyncjobs

import (
	"context"
	"sync"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

const defaultShutdownGrace = 30 * time.Second

var (
	shutdownCtx    context.Context
	cancelShutdown context.CancelFunc
	wg             sync.WaitGroup
	initOnce       sync.Once
)

// Init wires async job cancellation to the API server lifecycle. ADR-0162 pattern: graceful shutdown with drain grace.
// When parent is cancelled (SIGTERM), in-flight jobs receive cancellation on shutdownCtx. Init
// waits up to grace for jobs to finish, then returns.
func Init(parent context.Context, grace time.Duration) {
	if grace <= 0 {
		grace = defaultShutdownGrace
	}
	initOnce.Do(func() {
		shutdownCtx, cancelShutdown = context.WithCancel(parent)
	})
	go func() {
		<-parent.Done()
		log := logging.GetLogger()
		log.Info("API shutdown: cancelling in-flight async jobs")
		cancelShutdown()

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			log.Info("API shutdown: all async jobs completed")
		case <-time.After(grace):
			log.Warnf("API shutdown: async jobs did not complete within %s grace period", grace)
		}
	}()
}

// Context returns the cancellable context for background API work. Falls back to
// Background when Init has not been called (unit tests).
func Context() context.Context {
	if shutdownCtx != nil {
		return shutdownCtx
	}
	return context.Background()
}

// Go runs fn in a tracked goroutine that respects API shutdown cancellation.
func Go(fn func(ctx context.Context)) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		fn(Context())
	}()
}

// ResetForTest clears shutdown state between tests.
func ResetForTest() {
	if cancelShutdown != nil {
		cancelShutdown()
	}
	shutdownCtx = nil
	cancelShutdown = nil
	wg = sync.WaitGroup{}
	initOnce = sync.Once{}
}
