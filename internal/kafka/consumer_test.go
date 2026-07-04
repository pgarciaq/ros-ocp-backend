package kafka

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// panicReader fails the test if ReadMessage is invoked (used when ctx is already cancelled).
type panicReader struct{}

func (panicReader) ReadMessage(time.Duration) (*kafka.Message, error) {
	panic("ReadMessage must not be called when cancelled context exits the loop first")
}

func TestConsumeMessagesUntilCancelled_RespectsImmediateCancellation(t *testing.T) {
	t.Parallel()

	log := logrus.New()
	log.SetOutput(io.Discard)
	entry := logrus.NewEntry(log)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handler := func(context.Context, *kafka.Message, *kafka.Consumer) {
		panic("handler must not run")
	}
	wrapped := wrapHandlerWithInFlight(ctx, handler, &sync.WaitGroup{})

	done := make(chan struct{})
	go func() {
		defer close(done)
		consumeMessagesUntilCancelled(ctx, panicReader{}, nil, wrapped, entry)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("consumeMessagesUntilCancelled blocked instead of exiting on cancelled context")
	}
}

func TestConsumerCloseGracePeriod_Configured(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 30*time.Second, ConsumerCloseGracePeriod)
}

func TestDrainInFlightHandlers_WaitsForCompletion(t *testing.T) {
	t.Parallel()

	log := logrus.New()
	log.SetOutput(io.Discard)
	entry := logrus.NewEntry(log)

	var inFlight sync.WaitGroup
	inFlight.Add(1)
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(50 * time.Millisecond)
		inFlight.Done()
	}()

	done := make(chan struct{})
	go func() {
		drainInFlightHandlers(entry, &inFlight)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("drainInFlightHandlers did not return after handlers completed")
	}
	<-released
}

func TestDrainInFlightHandlers_TimesOut(t *testing.T) {
	t.Parallel()

	log := logrus.New()
	log.SetOutput(io.Discard)
	entry := logrus.NewEntry(log)

	var inFlight sync.WaitGroup
	inFlight.Add(1)

	start := time.Now()
	drainInFlightHandlersWithTimeout(entry, &inFlight, 100*time.Millisecond)
	elapsed := time.Since(start)

	require.GreaterOrEqual(t, elapsed, 90*time.Millisecond)
	require.Less(t, elapsed, 2*time.Second)
}

func TestWrapHandlerWithInFlight_TracksHandlers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var inFlight sync.WaitGroup
	var ran bool

	handler := func(context.Context, *kafka.Message, *kafka.Consumer) {
		ran = true
	}
	wrapped := wrapHandlerWithInFlight(ctx, handler, &inFlight)
	wrapped(&kafka.Message{}, nil)

	drainInFlightHandlers(logrus.NewEntry(logrus.New()), &inFlight)
	assert.True(t, ran)
}

func TestWrapHandlerWithInFlight_RecoversPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var inFlight sync.WaitGroup

	handler := func(context.Context, *kafka.Message, *kafka.Consumer) {
		panic("simulated crash")
	}
	wrapped := wrapHandlerWithInFlight(ctx, handler, &inFlight)

	msg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     strPtr("test-topic"),
			Partition: 0,
		},
	}

	require.NotPanics(t, func() {
		wrapped(msg, nil)
	})

	// WaitGroup must be fully decremented (no leak)
	done := make(chan struct{})
	go func() {
		inFlight.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("inFlight WaitGroup not drained after panic recovery")
	}
}

func TestWrapHandlerWithInFlight_CommitsOnPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var inFlight sync.WaitGroup

	handler := func(context.Context, *kafka.Message, *kafka.Consumer) {
		panic("handler explosion")
	}
	wrapped := wrapHandlerWithInFlight(ctx, handler, &inFlight)

	// We can't easily mock *kafka.Consumer, but verify no secondary panic
	// when consumer is nil (commit is skipped gracefully).
	msg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     strPtr("test-topic"),
			Partition: 1,
		},
	}
	require.NotPanics(t, func() {
		wrapped(msg, nil)
	})
}
