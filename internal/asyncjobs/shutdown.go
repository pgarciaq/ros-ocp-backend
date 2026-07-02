package asyncjobs

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

const defaultShutdownGrace = 30 * time.Second

var (
	lifecycleMu    sync.Mutex
	shutdownCtx    context.Context
	cancelShutdown context.CancelFunc
	wgPtr          atomic.Pointer[sync.WaitGroup]
	initOnce       sync.Once

	shutdownHooks []func()
	hooksMu       sync.Mutex
)

func init() {
	wgPtr.Store(&sync.WaitGroup{})
}

// RegisterShutdownHook runs fn when the API server lifecycle context is cancelled,
// before waiting for in-flight async jobs to finish.
func RegisterShutdownHook(fn func()) {
	if fn == nil {
		return
	}
	hooksMu.Lock()
	shutdownHooks = append(shutdownHooks, fn)
	hooksMu.Unlock()
}

// Init wires async job cancellation to the API server lifecycle. ADR-0162 pattern: graceful shutdown with drain grace.
// When parent is cancelled (SIGTERM), in-flight jobs receive cancellation on shutdownCtx. Init
// waits up to grace for jobs to finish, then returns.
func Init(parent context.Context, grace time.Duration) {
	if grace <= 0 {
		grace = defaultShutdownGrace
	}
	lifecycleMu.Lock()
	initOnce.Do(func() {
		shutdownCtx, cancelShutdown = context.WithCancel(parent)
	})
	cancel := cancelShutdown
	lifecycleMu.Unlock()

	currentWG := wgPtr.Load()
	go func() {
		<-parent.Done()
		log := logging.GetLogger()
		log.Info("API shutdown: cancelling in-flight async jobs")
		if cancel != nil {
			cancel()
		}

		hooksMu.Lock()
		hooks := append([]func(){}, shutdownHooks...)
		hooksMu.Unlock()
		for _, fn := range hooks {
			fn()
		}

		done := make(chan struct{})
		go func() {
			currentWG.Wait()
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
	lifecycleMu.Lock()
	ctx := shutdownCtx
	lifecycleMu.Unlock()
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

// Go runs fn in a tracked goroutine that respects API shutdown cancellation.
func Go(fn func(ctx context.Context)) {
	currentWG := wgPtr.Load()
	currentWG.Add(1)
	go func() {
		defer currentWG.Done()
		fn(Context())
	}()
}

const defaultTestDrainTimeout = 30 * time.Second

// WaitForTest blocks until all tracked async jobs finish or timeout expires.
// Integration tests call this before TRUNCATE so background work from a prior
// test cannot deadlock with table-level locks.
func WaitForTest(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultTestDrainTimeout
	}
	done := make(chan struct{})
	currentWG := wgPtr.Load()
	go func() {
		currentWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("async jobs did not complete within %s", timeout)
	}
}

// ResetForTest clears shutdown state between tests.
func ResetForTest() {
	lifecycleMu.Lock()
	cancel := cancelShutdown
	lifecycleMu.Unlock()

	if cancel != nil {
		cancel()
	}

	// Wait briefly for in-flight jobs on the current WaitGroup to finish.
	done := make(chan struct{})
	currentWG := wgPtr.Load()
	go func() {
		currentWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
	}

	lifecycleMu.Lock()
	shutdownCtx = nil
	cancelShutdown = nil
	initOnce = sync.Once{}
	lifecycleMu.Unlock()

	wgPtr.Store(&sync.WaitGroup{})

	hooksMu.Lock()
	shutdownHooks = nil
	hooksMu.Unlock()
}
