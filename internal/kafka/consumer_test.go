package kafka

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"
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

func TestPartitionKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		topic     string
		partition int32
		want      string
	}{
		{"my-topic", 0, "my-topic:0"},
		{"my-topic", 1, "my-topic:1"},
		{"events", 42, "events:42"},
		{"t", 2147483647, "t:2147483647"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got := partitionKey(tc.topic, tc.partition)
			assert.Equal(t, tc.want, got)
			// Verify equivalence with fmt.Sprintf (the old implementation).
			assert.Equal(t, fmt.Sprintf("%s:%d", tc.topic, tc.partition), got)
		})
	}
}

func TestPartitionLockKey(t *testing.T) {
	t.Parallel()
	topic := "test-topic"
	tp := kafka.TopicPartition{Topic: &topic, Partition: 3}
	got := partitionLockKey(tp)
	assert.Equal(t, "test-topic:3", got)
}

func BenchmarkPartitionKey(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = partitionKey("platform.upload.announce", 0)
	}
}

func BenchmarkPartitionKey_FmtSprintf(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("%s:%d", "platform.upload.announce", 0)
	}
}

// TestLogKafkaMessageReceived_NoRaceAcrossPartitionGoroutines is #804's
// RED: the sampling block (every 100th message) is entered by exactly
// one goroutine per multiple, but consecutive multiples' blocks can
// overlap across the partition goroutines that share the counters —
// the raw *batchStart read/write was a data race per the Go memory
// model. The detector flags unsynchronized cross-goroutine access
// regardless of wall-clock overlap, so 8 goroutines × 300 messages
// (24 sampling events spread across them) trips it on the old code.
func TestLogKafkaMessageReceived_NoRaceAcrossPartitionGoroutines(t *testing.T) {
	if !raceEnabled {
		t.Skip("requires the race detector: go test -race ./internal/kafka/")
	}
	// #804 review P3-1: scheduling luck is not detection — with the
	// default GOMAXPROCS the pre-fix race tripped only ~2/30 runs. On a
	// single P the cooperative preemption points inside the sampling
	// block (the time.Since / log.Infof calls) make the conflicting
	// read/write interleaving deterministic.
	prev := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })

	var msgCount uint64
	batchStart := newBatchStart()
	var logBuf bytes.Buffer
	log := logrus.NewEntry(logrus.New())
	log.Logger.SetOutput(&logBuf)
	log.Logger.SetLevel(logrus.InfoLevel)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 300; j++ {
				logKafkaMessageReceived(log, &msgCount, batchStart, &kafka.Message{})
			}
		}()
	}
	wg.Wait()
	if msgCount != 2400 {
		t.Fatalf("expected 2400 counted messages, got %d", msgCount)
	}
	// #804 review P3-3: the sampling behavior itself — exactly one INFO
	// line per 100 messages (24 events), never zero (a disabled sampling
	// block must fail this test, not just the race check).
	sampled := strings.Count(logBuf.String(), "Processed 100 kafka messages in")
	if sampled != 24 {
		t.Fatalf("expected 24 sampled log events, got %d", sampled)
	}
}
