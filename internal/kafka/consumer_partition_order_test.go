package kafka

// #756 regression suite: parallel consumption must preserve offset order
// within a partition (ADR-0154's stated contract) while keeping cross-partition
// parallelism, and a committed offset must never acknowledge an unprocessed
// message.
//
// Two tiers (see docs/plans/756-kafka-partition-ordered-queues.md):
//   - Tier 1 (baseline RED): the stress trace test reproduces the dequeue→lock
//     inversion of the shared-jobs-channel design. It is probabilistic on the
//     old code by nature (scheduler/mutex barging) and deterministic-by-
//     construction green on the partition-ordered design.
//   - Tier 2 (guards): deterministic tests of the ordering, semaphore, drain
//     and panic contracts that hold on the fixed design.

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func discardLogEntry() *logrus.Entry {
	log := logrus.New()
	log.SetOutput(io.Discard)
	return logrus.NewEntry(log)
}

func msgFor(topic string, partition int32, offset int64) *kafka.Message {
	return &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: partition, Offset: kafka.Offset(offset)},
		Value:          []byte("payload"),
	}
}

// scriptedReader yields a fixed sequence of messages, then returns benign
// timeout errors — the same shape a real consumer's ReadMessage returns on an
// idle topic, so the consume loop keeps polling until ctx is cancelled.
// exhausted is closed on the first call after the last message was served:
// because the dispatcher completes each routing send before its next
// ReadMessage call, exhaustion proves every message is already routed.
type scriptedReader struct {
	mu        sync.Mutex
	msgs      []*kafka.Message
	next      int
	exhausted chan struct{}
	once      sync.Once
}

func newScriptedReader(msgs []*kafka.Message) *scriptedReader {
	return &scriptedReader{msgs: msgs, exhausted: make(chan struct{})}
}

func (r *scriptedReader) ReadMessage(_ time.Duration) (*kafka.Message, error) {
	r.mu.Lock()
	if r.next < len(r.msgs) {
		m := r.msgs[r.next]
		r.next++
		r.mu.Unlock()
		return m, nil
	}
	r.mu.Unlock()
	r.once.Do(func() { close(r.exhausted) })
	time.Sleep(2 * time.Millisecond)
	return nil, kafka.NewError(kafka.ErrTimedOut, "scripted reader exhausted", false)
}

// startConsumption runs the parallel consume path in a goroutine and returns a
// channel closed when it has fully returned (dispatch stopped, queues drained).
func startConsumption(ctx context.Context, reader kafkaReader, workers int, handler func(*kafka.Message, *kafka.Consumer)) chan struct{} {
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		consumeMessagesParallelUntilCancelled(ctx, reader, nil, handler, discardLogEntry(), workers)
	}()
	return returned
}

func waitForReturn(t *testing.T, returned chan struct{}) {
	t.Helper()
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("consumer did not shut down after cancel")
	}
}

// Tier 1 (#756 RED on the reviewed baseline): within one partition, handler
// execution must follow offset order. The shared-jobs-channel design lets a
// worker dequeue a higher offset and win the per-partition mutex while the
// lower offset's worker is still in the dequeue→lock window — Go mutexes make
// no FIFO wake-order guarantee. Sub-millisecond handler jitter keeps the
// baseline's mutex in normal (barging) mode so the inversion reproduces under
// contention; on the partition-ordered design reordering is impossible by
// construction (one goroutine per partition).
func TestParallelConsumption_PreservesOffsetOrderWithinPartition(t *testing.T) {
	t.Parallel()

	const (
		total   = 500
		workers = 8
	)
	msgs := make([]*kafka.Message, total)
	for i := range msgs {
		msgs[i] = msgFor("stress-topic", 0, int64(i))
	}

	var mu sync.Mutex
	trace := make([]kafka.Offset, 0, total)
	allDone := make(chan struct{})
	var once sync.Once

	handler := func(msg *kafka.Message, _ *kafka.Consumer) {
		// Deterministic per-offset jitter (0-149µs): keeps the baseline's
		// partition mutex in normal (barging) mode and varies contention
		// without a random source.
		time.Sleep(time.Duration(int64(msg.TopicPartition.Offset)%150) * time.Microsecond)
		mu.Lock()
		trace = append(trace, msg.TopicPartition.Offset)
		n := len(trace)
		mu.Unlock()
		if n == total {
			once.Do(func() { close(allDone) })
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	returned := startConsumption(ctx, newScriptedReader(msgs), workers, handler)

	select {
	case <-allDone:
	case <-time.After(30 * time.Second):
		t.Fatal("stress messages not all processed within 30s")
	}
	cancel()
	waitForReturn(t, returned)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, trace, total)
	for i := 1; i < len(trace); i++ {
		require.Greater(t, trace[i], trace[i-1],
			"partition 0 execution reordered: offset %d ran after offset %d — a worker dequeued a higher offset and won the partition lock before the lower offset (#756)",
			trace[i-1], trace[i])
	}
}

// Tier 2 (guard): a blocked lower offset must hold back every higher offset
// in the same partition — none may execute past it.
func TestParallelConsumption_BlockedLowerOffsetHoldsBackHigher(t *testing.T) {
	t.Parallel()

	const total = 10
	msgs := make([]*kafka.Message, total)
	for i := range msgs {
		msgs[i] = msgFor("gate-topic", 0, int64(i))
	}

	entered := make(chan kafka.Offset, total)
	release := make(chan struct{})
	var mu sync.Mutex
	trace := make([]kafka.Offset, 0, total)

	handler := func(msg *kafka.Message, _ *kafka.Consumer) {
		off := msg.TopicPartition.Offset
		if off == 3 {
			<-release // the blocked lower offset, holding the partition's turn
		}
		mu.Lock()
		trace = append(trace, off)
		mu.Unlock()
		entered <- off
	}

	ctx, cancel := context.WithCancel(context.Background())
	returned := startConsumption(ctx, newScriptedReader(msgs), 4, handler)

	// Offsets 0-2 complete; offset 3 is blocked holding its turn.
	for i := 0; i < 3; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("offsets before the gate did not process")
		}
	}

	// While offset 3 is blocked, no higher offset may execute.
	select {
	case off := <-entered:
		t.Fatalf("offset %d executed while lower offset 3 was blocked — partition order violated (#756)", off)
	case <-time.After(300 * time.Millisecond):
	}

	close(release)
	for i := 0; i < 7; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("partition stalled after the gate released")
		}
	}
	cancel()
	waitForReturn(t, returned)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, trace, total)
	for i := 1; i < len(trace); i++ {
		require.Greater(t, trace[i], trace[i-1], "trace reordered after gate release: %v", trace)
	}
}

// Control (must pass on both designs): handlers for different partitions run
// concurrently — ADR-0154's parallelism-across-partitions half.
func TestParallelConsumption_CrossPartitionsRunConcurrently(t *testing.T) {
	t.Parallel()

	msgs := []*kafka.Message{
		msgFor("multi-topic", 0, 0),
		msgFor("multi-topic", 1, 0),
	}

	entered := make(chan int32, 2)
	release := make(chan struct{})

	handler := func(msg *kafka.Message, _ *kafka.Consumer) {
		entered <- msg.TopicPartition.Partition
		<-release
	}

	ctx, cancel := context.WithCancel(context.Background())
	returned := startConsumption(ctx, newScriptedReader(msgs), 2, handler)

	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("cross-partition handlers did not run concurrently — parallelism across partitions lost (ADR-0154)")
		}
	}
	close(release)
	cancel()
	waitForReturn(t, returned)
}

// Tier 2 (guard): the concurrency cap bounds in-flight handlers across all
// partitions (the DB-connection budget), while every partition stays ordered.
func TestParallelConsumption_ConcurrencyCapBoundsInFlightHandlers(t *testing.T) {
	t.Parallel()

	const (
		partitions = 5
		maxAllowed = 2
	)
	msgs := make([]*kafka.Message, 0, partitions)
	for p := int32(0); p < partitions; p++ {
		msgs = append(msgs, msgFor("cap-topic", p, 0))
	}

	var inFlight, maxSeen int64
	processed := make(chan struct{}, partitions)

	handler := func(*kafka.Message, *kafka.Consumer) {
		cur := atomic.AddInt64(&inFlight, 1)
		for {
			m := atomic.LoadInt64(&maxSeen)
			if cur <= m || atomic.CompareAndSwapInt64(&maxSeen, m, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt64(&inFlight, -1)
		processed <- struct{}{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	returned := startConsumption(ctx, newScriptedReader(msgs), maxAllowed, handler)

	for i := 0; i < partitions; i++ {
		select {
		case <-processed:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d partitions processed — deadlock under the concurrency cap", i, partitions)
		}
	}
	cancel()
	waitForReturn(t, returned)

	require.LessOrEqual(t, atomic.LoadInt64(&maxSeen), int64(maxAllowed),
		"%d handlers in flight at once exceeds the cap %d — the DB-connection budget is not respected (#756)",
		atomic.LoadInt64(&maxSeen), maxAllowed)
}

// Tier 2 (guard): the shutdown contract (StartConsumer: "drains in-flight
// handlers") — messages already queued when ctx is cancelled are still
// processed, in partition order.
func TestParallelConsumption_DrainsQueuedMessagesOnCancel(t *testing.T) {
	t.Parallel()

	const total = 5
	msgs := make([]*kafka.Message, total)
	for i := range msgs {
		msgs[i] = msgFor("drain-topic", 0, int64(i))
	}

	entered := make(chan kafka.Offset, total)
	processed := make(chan kafka.Offset, total)

	handler := func(msg *kafka.Message, _ *kafka.Consumer) {
		off := msg.TopicPartition.Offset
		entered <- off
		if off == 0 {
			time.Sleep(300 * time.Millisecond) // still in flight when cancel fires
		}
		processed <- off
	}

	ctx, cancel := context.WithCancel(context.Background())
	reader := newScriptedReader(msgs)
	returned := startConsumption(ctx, reader, 3, handler)

	// A handler is in flight; the rest of the partition is queued behind it.
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first message never entered its handler")
	}
	// Deterministic precondition (review finding #3): the reader's exhaustion
	// proves every message was read AND routed (the dispatcher completes each
	// send before its next ReadMessage call), so cancel cannot drop an
	// unrouted message — what follows exercises purely the drain contract.
	select {
	case <-reader.exhausted:
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not serve all messages before cancel")
	}
	cancel()

	// Queued messages must not be dropped by the cancellation.
	var got []kafka.Offset
	for i := 0; i < total; i++ {
		select {
		case off := <-processed:
			got = append(got, off)
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d queued messages drained after cancel", i, total)
		}
	}
	waitForReturn(t, returned)

	require.Len(t, got, total)
	for i := 1; i < len(got); i++ {
		require.Greater(t, got[i], got[i-1], "drained messages reordered: %v", got)
	}
}

// Tier 2 (guard): a panicking handler is skipped via commit-on-skip and the
// partition continues with the next offset in order.
func TestParallelConsumption_PanicSkipsMessageAndPartitionContinues(t *testing.T) {
	t.Parallel()

	const total = 10
	msgs := make([]*kafka.Message, total)
	for i := range msgs {
		msgs[i] = msgFor("panic-topic", 0, int64(i))
	}

	processed := make(chan kafka.Offset, total)

	handler := func(msg *kafka.Message, _ *kafka.Consumer) {
		off := msg.TopicPartition.Offset
		if off == 3 {
			panic("simulated handler crash")
		}
		processed <- off
	}

	ctx, cancel := context.WithCancel(context.Background())
	returned := startConsumption(ctx, newScriptedReader(msgs), 4, handler)

	var got []kafka.Offset
	for i := 0; i < total-1; i++ {
		select {
		case off := <-processed:
			got = append(got, off)
		case <-time.After(5 * time.Second):
			t.Fatalf("partition stalled after panic: %d of %d surviving messages processed", i, total-1)
		}
	}
	cancel()
	waitForReturn(t, returned)

	require.Len(t, got, total-1)
	for _, off := range got {
		require.NotEqual(t, kafka.Offset(3), off, "the panicking offset must be skipped, not retried")
	}
	for i := 1; i < len(got); i++ {
		require.Greater(t, got[i], got[i-1], "survivors reordered after panic: %v", got)
	}
}

// The audit's mock-broker proof, turned into a standing regression: with a
// real consumer group on librdkafka's in-process mock cluster, a committed
// offset must never acknowledge an unprocessed message. Offset 0 blocks in its
// handler (nothing after it may run or commit); offset 1 panics (the
// commit-on-skip path must advance the watermark, never stall the partition).
func TestParallelConsumption_CommitsNeverAckUnprocessedOffsets(t *testing.T) {
	if testing.Short() {
		t.Skip("mock-cluster integration test skipped in short mode")
	}

	mc, err := kafka.NewMockCluster(1)
	require.NoError(t, err)
	defer mc.Close()
	brokers := mc.BootstrapServers()
	topic := "ros756-watermark"
	require.NoError(t, mc.CreateTopic(topic, 1, 1))

	producer, err := kafka.NewProducer(&kafka.ConfigMap{"bootstrap.servers": brokers})
	require.NoError(t, err)
	// NB: Produce with a nil delivery channel never completes against the
	// mock cluster (librdkafka quirk) — a delivery channel is required.
	delivery := make(chan kafka.Event, 2)
	for i := 0; i < 2; i++ {
		require.NoError(t, producer.Produce(&kafka.Message{
			TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: 0},
			Value:          []byte("payload"),
		}, delivery))
	}
	for i := 0; i < 2; i++ {
		select {
		case ev := <-delivery:
			m, ok := ev.(*kafka.Message)
			require.True(t, ok)
			require.NoError(t, m.TopicPartition.Error)
		case <-time.After(10 * time.Second):
			t.Fatal("test message not produced")
		}
	}
	producer.Close()

	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":  brokers,
		"group.id":           "ros756-watermark-group",
		"enable.auto.commit": false,
		"auto.offset.reset":  "earliest",
	})
	require.NoError(t, err)
	// errcheck: Close's error is irrelevant to the assertions here.
	defer func() { _ = consumer.Close() }()
	require.NoError(t, consumer.Subscribe(topic, nil))

	entered := make(chan kafka.Offset, 2)
	release := make(chan struct{})
	trace := make(chan kafka.Offset, 2)

	handler := func(msg *kafka.Message, c *kafka.Consumer) {
		off := msg.TopicPartition.Offset
		entered <- off
		if off == 0 {
			<-release // hold the partition's turn: nothing after 0 may run or commit
		}
		if off == 1 {
			panic("simulated handler crash on offset 1")
		}
		// Success path commits at the handler tail, as report_processor does.
		if err := CommitMessage(c, msg); err != nil {
			t.Errorf("success-path commit failed: %v", err)
		}
		trace <- off
	}

	consumeCtx, consumeCancel := context.WithCancel(context.Background())
	defer consumeCancel()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		consumeMessagesParallelUntilCancelled(consumeCtx, consumer, consumer, handler, discardLogEntry(), 3)
	}()

	// Offset 0 enters and blocks.
	select {
	case off := <-entered:
		require.Equal(t, kafka.Offset(0), off)
	case <-time.After(20 * time.Second):
		t.Fatal("consumer did not receive offset 0 from the mock cluster")
	}

	// Give offset 1 every opportunity to be fetched and dispatched while 0 is
	// blocked (review finding #4): the dangerous window the original bug
	// exploited. A correct implementation leaves 1 waiting in the partition
	// queue; the barging baseline would enter and commit it.
	time.Sleep(500 * time.Millisecond)

	// While 0 is blocked (unprocessed), no offset may be committed.
	committed, err := consumer.Committed([]kafka.TopicPartition{{Topic: &topic, Partition: 0}}, 5_000)
	require.NoError(t, err)
	require.Len(t, committed, 1)
	require.True(t, committed[0].Offset == kafka.OffsetInvalid || committed[0].Offset <= 0,
		"committed offset %v acknowledges unprocessed messages while offset 0 is still in flight (#756)",
		committed[0].Offset)

	close(release)

	// Offset 0 completes and commits.
	select {
	case off := <-trace:
		require.Equal(t, kafka.Offset(0), off)
	case <-time.After(5 * time.Second):
		t.Fatal("offset 0 did not complete after release")
	}
	// Offset 1 enters, panics, and is commit-skipped.
	select {
	case off := <-entered:
		require.Equal(t, kafka.Offset(1), off)
	case <-time.After(5 * time.Second):
		t.Fatal("partition stalled after offset 0 — panic recovery must continue the partition")
	}

	consumeCancel()
	waitForReturn(t, returned)

	// Final watermark: the panic path committed (skip) offset 1, so the
	// committed next-offset is 2 — exactly the processed prefix.
	committed, err = consumer.Committed([]kafka.TopicPartition{{Topic: &topic, Partition: 0}}, 5_000)
	require.NoError(t, err)
	require.Equal(t, kafka.Offset(2), committed[0].Offset,
		"commit-on-skip after the panic must advance the watermark to exactly the processed prefix")
}
