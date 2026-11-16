package kafka

import (
	"context"
	"testing"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/stretchr/testify/require"
)

// TestParallelConsumption_CommitFallbackComposesWithNextCommit is #803
// finding 1's composition proof: the produce-failure fallback's own commit
// (the loud-loss disposition — alert, mark, commit) and the next message's
// success-tail commit compose correctly through the real partition-queue
// machinery against a mock broker — commits succeed, messages process in
// offset order, the machinery does not deadlock or double-process.
//
// Scope, honestly (the #803 review's finding): the final broker watermark
// CANNOT distinguish the fallback's own commit from the next message's
// cumulative commit (Kafka commits are cumulative — the pre-fix behavior
// reaches the same final watermark). The unit-level RED in
// internal/services pins that the fallback commits and fires the alert;
// this test pins the composition. The committed offset follows Kafka's
// next-to-read convention: committing offsets 0 and 1 reports 2.
func TestParallelConsumption_CommitFallbackComposesWithNextCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("mock-cluster integration test skipped in short mode")
	}

	mc, err := kafka.NewMockCluster(1)
	require.NoError(t, err)
	defer mc.Close()
	brokers := mc.BootstrapServers()
	topic := "ros803-fallback-compose"
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
		"group.id":           "ros803-fallback-compose-group",
		"enable.auto.commit": false,
		"auto.offset.reset":  "earliest",
	})
	require.NoError(t, err)
	defer func() { _ = consumer.Close() }()
	require.NoError(t, consumer.Subscribe(topic, nil))

	done := make(chan kafka.Offset, 2)
	handler := func(msg *kafka.Message, c *kafka.Consumer) {
		off := msg.TopicPartition.Offset
		if off == 0 {
			// The #803 fallback disposition: the message is dropped with
			// alert, and the fallback COMMITS the offset itself (loud
			// loss — never an uncommitted return that the next message's
			// cumulative commit would silently ack).
			if err := CommitMessage(c, msg); err != nil {
				t.Errorf("fallback commit failed: %v", err)
			}
			done <- off
			return
		}
		// The success path commits at the handler tail, as report_processor does.
		if err := CommitMessage(c, msg); err != nil {
			t.Errorf("success-path commit failed: %v", err)
		}
		done <- off
	}

	consumeCtx, consumeCancel := context.WithCancel(context.Background())
	defer consumeCancel()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		consumeMessagesParallelUntilCancelled(consumeCtx, consumer, consumer, handler, discardLogEntry(), 3)
	}()

	for i := 0; i < 2; i++ {
		select {
		case off := <-done:
			require.Equal(t, kafka.Offset(i), off, "messages must process in offset order")
		case <-time.After(20 * time.Second):
			t.Fatal("consumer did not process both messages")
		}
	}

	// The broker's committed watermark covers both offsets: the fallback's
	// own commit for 0 and the success-tail commit for 1 — composed.
	committed, err := consumer.Committed([]kafka.TopicPartition{{Topic: &topic, Partition: 0}}, 5_000)
	require.NoError(t, err)
	require.Len(t, committed, 1)
	t.Logf("committed offset: %v", committed[0].Offset)
	require.Equal(t, kafka.Offset(2), committed[0].Offset,
		"the fallback's commit and the next message's commit must compose to the full watermark (#803)")

	consumeCancel()
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("consumer did not shut down")
	}
}
