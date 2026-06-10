package services

import (
	"strconv"
	"testing"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeKafkaProducer struct {
	messages []*kafka.Message
	topics   []string
	err      error
}

func (f *fakeKafkaProducer) Produce(msg *kafka.Message, deliveryChan chan kafka.Event) error {
	if f.err != nil {
		return f.err
	}
	f.messages = append(f.messages, msg)
	f.topics = append(f.topics, *msg.TopicPartition.Topic)
	delivered := &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     msg.TopicPartition.Topic,
			Partition: msg.TopicPartition.Partition,
			Offset:    kafka.Offset(1),
		},
	}
	deliveryChan <- delivered
	return nil
}

type fakeKafkaCommitter struct {
	committed bool
	err       error
}

func (f *fakeKafkaCommitter) CommitMessage(m *kafka.Message) ([]kafka.TopicPartition, error) {
	f.committed = true
	return nil, f.err
}

func testTopicMessage(headers []kafka.Header) *kafka.Message {
	topic := "hccm.ros.events"
	return &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: 2},
		Key:            []byte("key-1"),
		Value:          []byte(`{"request_id":"abc"}`),
		Headers:        headers,
	}
}

func headerValue(headers []kafka.Header, key string) (string, bool) {
	for _, h := range headers {
		if h.Key == key {
			return string(h.Value), true
		}
	}
	return "", false
}

func TestGetRetryCount_NoHeaderReturnsZero(t *testing.T) {
	t.Parallel()
	msg := testTopicMessage(nil)
	assert.Equal(t, 0, getRetryCount(msg))
}

func TestGetRetryCount_ValidHeaderReturnsCount(t *testing.T) {
	t.Parallel()
	msg := testTopicMessage([]kafka.Header{{Key: retryCountHeader, Value: []byte("3")}})
	assert.Equal(t, 3, getRetryCount(msg))
}

func TestGetRetryCount_MalformedHeaderReturnsZero(t *testing.T) {
	t.Parallel()
	msg := testTopicMessage([]kafka.Header{{Key: retryCountHeader, Value: []byte("not-a-number")}})
	assert.Equal(t, 0, getRetryCount(msg))
}

func TestProduceToDLQ_ProducesMessageWithCorrectHeaders(t *testing.T) {
	t.Parallel()
	producer := &fakeKafkaProducer{}
	msg := testTopicMessage([]kafka.Header{{Key: "X-Custom", Value: []byte("keep")}})

	err := produceToDLQImpl(producer, msg, "db timeout")
	require.NoError(t, err)
	require.Len(t, producer.messages, 1)

	out := producer.messages[0]
	assert.Equal(t, dlqTopicName(), *out.TopicPartition.Topic)
	assert.Equal(t, msg.Key, out.Key)
	assert.Equal(t, msg.Value, out.Value)

	originalTopic, ok := headerValue(out.Headers, headerOriginalTopic)
	require.True(t, ok)
	assert.Equal(t, "hccm.ros.events", originalTopic)

	partition, ok := headerValue(out.Headers, headerOriginalPartition)
	require.True(t, ok)
	assert.Equal(t, "2", partition)

	reason, ok := headerValue(out.Headers, headerFailureReason)
	require.True(t, ok)
	assert.Equal(t, "db timeout", reason)

	_, ok = headerValue(out.Headers, headerFailedAt)
	require.True(t, ok)

	custom, ok := headerValue(out.Headers, "X-Custom")
	require.True(t, ok)
	assert.Equal(t, "keep", custom)
}

func TestProduceRetry_ProducesMessageWithIncrementedCount(t *testing.T) {
	t.Parallel()
	producer := &fakeKafkaProducer{}
	msg := testTopicMessage([]kafka.Header{
		{Key: retryCountHeader, Value: []byte("2")},
		{Key: "X-Trace", Value: []byte("trace-1")},
	})

	err := produceRetryImpl(producer, msg, 2)
	require.NoError(t, err)
	require.Len(t, producer.messages, 1)

	out := producer.messages[0]
	assert.Equal(t, "hccm.ros.events", *out.TopicPartition.Topic)

	retryVal, ok := headerValue(out.Headers, retryCountHeader)
	require.True(t, ok)
	assert.Equal(t, "3", retryVal)

	trace, ok := headerValue(out.Headers, "X-Trace")
	require.True(t, ok)
	assert.Equal(t, "trace-1", trace)

	count := 0
	for _, h := range out.Headers {
		if h.Key == retryCountHeader {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func counterValue(t *testing.T, name string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		if len(mf.GetMetric()) == 0 {
			return 0
		}
		return mf.GetMetric()[0].GetCounter().GetValue()
	}
	return 0
}

func TestHandleKafkaTransientError_RetryPathWhenBelowMax(t *testing.T) {
	before := counterValue(t, "rosocp_kafka_retries_total")
	producer := &fakeKafkaProducer{}
	consumer := &fakeKafkaCommitter{}
	msg := testTopicMessage(nil)

	handleKafkaTransientError(consumer, producer, msg, assert.AnError)

	assert.True(t, consumer.committed)
	require.Len(t, producer.messages, 1)
	retryVal, ok := headerValue(producer.messages[0].Headers, retryCountHeader)
	require.True(t, ok)
	assert.Equal(t, "1", retryVal)
	after := counterValue(t, "rosocp_kafka_retries_total")
	assert.Equal(t, before+1, after)
}

func TestHandleKafkaTransientError_DLQPathWhenAtMax(t *testing.T) {
	beforeDLQ := counterValue(t, "rosocp_kafka_dlq_messages_total")
	producer := &fakeKafkaProducer{}
	consumer := &fakeKafkaCommitter{}
	maxRetries := maxTransientRetries()
	msg := testTopicMessage([]kafka.Header{{Key: retryCountHeader, Value: []byte(strconv.Itoa(maxRetries))}})

	handleKafkaTransientError(consumer, producer, msg, assert.AnError)

	assert.True(t, consumer.committed)
	require.Len(t, producer.messages, 1)
	assert.Equal(t, dlqTopicName(), *producer.messages[0].TopicPartition.Topic)
	reason, ok := headerValue(producer.messages[0].Headers, headerFailureReason)
	require.True(t, ok)
	assert.NotEmpty(t, reason)
	afterDLQ := counterValue(t, "rosocp_kafka_dlq_messages_total")
	assert.Equal(t, beforeDLQ+1, afterDLQ)
}

func TestHandleKafkaTransientError_ProduceFailureDoesNotCommit(t *testing.T) {
	t.Parallel()
	producer := &fakeKafkaProducer{err: assert.AnError}
	consumer := &fakeKafkaCommitter{}
	msg := testTopicMessage(nil)

	handleKafkaTransientError(consumer, producer, msg, assert.AnError)
	assert.False(t, consumer.committed)
}

func TestHandleKafkaTransientError_DLQProduceFailureDoesNotCommit(t *testing.T) {
	t.Parallel()
	producer := &fakeKafkaProducer{err: assert.AnError}
	consumer := &fakeKafkaCommitter{}
	maxRetries := maxTransientRetries()
	msg := testTopicMessage([]kafka.Header{{Key: retryCountHeader, Value: []byte(strconv.Itoa(maxRetries))}})

	handleKafkaTransientError(consumer, producer, msg, assert.AnError)
	assert.False(t, consumer.committed)
}
