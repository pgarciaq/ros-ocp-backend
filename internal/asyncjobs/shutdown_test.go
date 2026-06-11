package asyncjobs

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit_CancelsInFlightJobs(t *testing.T) {
	ResetForTest()
	parent, cancel := context.WithCancel(context.Background())
	Init(parent, 2*time.Second)

	var started atomic.Bool
	var cancelled atomic.Bool
	done := make(chan struct{})

	Go(func(ctx context.Context) {
		started.Store(true)
		<-ctx.Done()
		cancelled.Store(true)
		close(done)
	})

	require.Eventually(t, func() bool { return started.Load() }, time.Second, 10*time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("async job did not observe cancellation")
	}
	assert.True(t, cancelled.Load())
}
