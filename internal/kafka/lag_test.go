package kafka

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

func strPtr(s string) *string { return &s }

// mockLagQuerier implements LagQuerier for testing.
type mockLagQuerier struct {
	mu                sync.Mutex
	assignmentResult  []kafka.TopicPartition
	assignmentErr     error
	watermarks        map[string][2]int64 // "topic:partition" → {low, high}
	watermarkErr      error
	committedResult   []kafka.TopicPartition
	committedErr      error
	assignmentCalls   int
	watermarkCalls    int
	committedCalls    int
}

func (m *mockLagQuerier) Assignment() ([]kafka.TopicPartition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assignmentCalls++
	return m.assignmentResult, m.assignmentErr
}

func (m *mockLagQuerier) QueryWatermarkOffsets(topic string, partition int32, _ int) (int64, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watermarkCalls++
	if m.watermarkErr != nil {
		return -1, -1, m.watermarkErr
	}
	key := fmt.Sprintf("%s:%d", topic, partition)
	wm, ok := m.watermarks[key]
	if !ok {
		return 0, 0, nil
	}
	return wm[0], wm[1], nil
}

func (m *mockLagQuerier) Committed(partitions []kafka.TopicPartition, _ int) ([]kafka.TopicPartition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.committedCalls++
	if m.committedErr != nil {
		return nil, m.committedErr
	}
	return m.committedResult, nil
}

func gatherGauge(t *testing.T, name string) map[string]float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	result := make(map[string]float64)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			key := ""
			for _, lp := range m.GetLabel() {
				if key != "" {
					key += ","
				}
				key += lp.GetName() + "=" + lp.GetValue()
			}
			if m.GetGauge() != nil {
				result[key] = m.GetGauge().GetValue()
			}
		}
	}
	return result
}

func TestLagMonitor_NormalLag(t *testing.T) {
	t.Cleanup(func() {
		metrics.KafkaConsumerLag.Reset()
		metrics.KafkaConsumerLagTotal.Reset()
	})

	topic := "test-topic"
	mock := &mockLagQuerier{
		assignmentResult: []kafka.TopicPartition{
			{Topic: strPtr(topic), Partition: 0},
			{Topic: strPtr(topic), Partition: 1},
		},
		watermarks: map[string][2]int64{
			"test-topic:0": {0, 100},
			"test-topic:1": {0, 250},
		},
		committedResult: []kafka.TopicPartition{
			{Topic: strPtr(topic), Partition: 0, Offset: kafka.Offset(80)},
			{Topic: strPtr(topic), Partition: 1, Offset: kafka.Offset(200)},
		},
	}

	lm := NewLagMonitor(mock, time.Hour)
	lm.poll()

	perPartition := gatherGauge(t, "rosocp_kafka_consumer_lag")
	assert.InDelta(t, 20.0, perPartition["partition=0,topic=test-topic"], 0.1)
	assert.InDelta(t, 50.0, perPartition["partition=1,topic=test-topic"], 0.1)

	total := gatherGauge(t, "rosocp_kafka_consumer_lag_total")
	assert.InDelta(t, 70.0, total["topic=test-topic"], 0.1)
}

func TestLagMonitor_ZeroLag(t *testing.T) {
	t.Cleanup(func() {
		metrics.KafkaConsumerLag.Reset()
		metrics.KafkaConsumerLagTotal.Reset()
	})

	topic := "caught-up"
	mock := &mockLagQuerier{
		assignmentResult: []kafka.TopicPartition{
			{Topic: strPtr(topic), Partition: 0},
		},
		watermarks: map[string][2]int64{
			"caught-up:0": {0, 500},
		},
		committedResult: []kafka.TopicPartition{
			{Topic: strPtr(topic), Partition: 0, Offset: kafka.Offset(500)},
		},
	}

	lm := NewLagMonitor(mock, time.Hour)
	lm.poll()

	perPartition := gatherGauge(t, "rosocp_kafka_consumer_lag")
	assert.InDelta(t, 0.0, perPartition["partition=0,topic=caught-up"], 0.1)

	total := gatherGauge(t, "rosocp_kafka_consumer_lag_total")
	assert.InDelta(t, 0.0, total["topic=caught-up"], 0.1)
}

func TestLagMonitor_RebalanceCleansStaleLabels(t *testing.T) {
	t.Cleanup(func() {
		metrics.KafkaConsumerLag.Reset()
		metrics.KafkaConsumerLagTotal.Reset()
	})

	topic := "rebalance-topic"
	mock := &mockLagQuerier{
		assignmentResult: []kafka.TopicPartition{
			{Topic: strPtr(topic), Partition: 0},
			{Topic: strPtr(topic), Partition: 1},
		},
		watermarks: map[string][2]int64{
			"rebalance-topic:0": {0, 100},
			"rebalance-topic:1": {0, 200},
		},
		committedResult: []kafka.TopicPartition{
			{Topic: strPtr(topic), Partition: 0, Offset: kafka.Offset(50)},
			{Topic: strPtr(topic), Partition: 1, Offset: kafka.Offset(100)},
		},
	}

	lm := NewLagMonitor(mock, time.Hour)
	lm.poll()

	perPartition := gatherGauge(t, "rosocp_kafka_consumer_lag")
	require.Contains(t, perPartition, "partition=1,topic=rebalance-topic")

	// Simulate rebalance: partition 1 is revoked.
	mock.mu.Lock()
	mock.assignmentResult = []kafka.TopicPartition{
		{Topic: strPtr(topic), Partition: 0},
	}
	mock.committedResult = []kafka.TopicPartition{
		{Topic: strPtr(topic), Partition: 0, Offset: kafka.Offset(60)},
	}
	mock.mu.Unlock()

	lm.poll()

	perPartition = gatherGauge(t, "rosocp_kafka_consumer_lag")
	assert.InDelta(t, 40.0, perPartition["partition=0,topic=rebalance-topic"], 0.1)
	_, hasP1 := perPartition["partition=1,topic=rebalance-topic"]
	assert.False(t, hasP1, "partition 1 label should be removed after rebalance")
}

func TestLagMonitor_GracefulShutdown(t *testing.T) {
	t.Cleanup(func() {
		metrics.KafkaConsumerLag.Reset()
		metrics.KafkaConsumerLagTotal.Reset()
	})

	mock := &mockLagQuerier{
		assignmentResult: []kafka.TopicPartition{
			{Topic: strPtr("shutdown-topic"), Partition: 0},
		},
		watermarks: map[string][2]int64{
			"shutdown-topic:0": {0, 10},
		},
		committedResult: []kafka.TopicPartition{
			{Topic: strPtr("shutdown-topic"), Partition: 0, Offset: kafka.Offset(5)},
		},
	}

	lm := NewLagMonitor(mock, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		lm.Start(ctx)
	}()

	// Let it poll at least once.
	time.Sleep(120 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("LagMonitor.Start did not exit after context cancellation")
	}

	// After shutdown, gauges should be reset.
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() == "rosocp_kafka_consumer_lag" || mf.GetName() == "rosocp_kafka_consumer_lag_total" {
			assert.Empty(t, mf.GetMetric(), "metric %s should have no samples after shutdown", mf.GetName())
		}
	}
}

func TestLagMonitor_AssignmentError(t *testing.T) {
	t.Cleanup(func() {
		metrics.KafkaConsumerLag.Reset()
		metrics.KafkaConsumerLagTotal.Reset()
	})

	mock := &mockLagQuerier{
		assignmentErr: fmt.Errorf("broker unavailable"),
	}

	lm := NewLagMonitor(mock, time.Hour)
	lm.poll()

	perPartition := gatherGauge(t, "rosocp_kafka_consumer_lag")
	assert.Empty(t, perPartition, "no lag metrics should be emitted on Assignment error")
}

func TestLagMonitor_CommittedError(t *testing.T) {
	t.Cleanup(func() {
		metrics.KafkaConsumerLag.Reset()
		metrics.KafkaConsumerLagTotal.Reset()
	})

	topic := "err-topic"
	mock := &mockLagQuerier{
		assignmentResult: []kafka.TopicPartition{
			{Topic: strPtr(topic), Partition: 0},
		},
		committedErr: fmt.Errorf("timeout"),
	}

	lm := NewLagMonitor(mock, time.Hour)
	lm.poll()

	perPartition := gatherGauge(t, "rosocp_kafka_consumer_lag")
	assert.Empty(t, perPartition, "no lag metrics should be emitted on Committed error")
}

func TestLagMonitor_WatermarkError(t *testing.T) {
	t.Cleanup(func() {
		metrics.KafkaConsumerLag.Reset()
		metrics.KafkaConsumerLagTotal.Reset()
	})

	topic := "wm-err"
	mock := &mockLagQuerier{
		assignmentResult: []kafka.TopicPartition{
			{Topic: strPtr(topic), Partition: 0},
		},
		watermarkErr: fmt.Errorf("connection lost"),
		committedResult: []kafka.TopicPartition{
			{Topic: strPtr(topic), Partition: 0, Offset: kafka.Offset(10)},
		},
	}

	lm := NewLagMonitor(mock, time.Hour)
	lm.poll()

	total := gatherGauge(t, "rosocp_kafka_consumer_lag_total")
	_, hasTopic := total["topic=wm-err"]
	assert.False(t, hasTopic, "no total should be emitted when watermark fails for all partitions")
}

func TestLagMonitor_UncommittedOffset(t *testing.T) {
	t.Cleanup(func() {
		metrics.KafkaConsumerLag.Reset()
		metrics.KafkaConsumerLagTotal.Reset()
	})

	topic := "fresh-topic"
	mock := &mockLagQuerier{
		assignmentResult: []kafka.TopicPartition{
			{Topic: strPtr(topic), Partition: 0},
		},
		watermarks: map[string][2]int64{
			"fresh-topic:0": {0, 100},
		},
		committedResult: []kafka.TopicPartition{
			{Topic: strPtr(topic), Partition: 0, Offset: kafka.Offset(-1001)},
		},
	}

	lm := NewLagMonitor(mock, time.Hour)
	lm.poll()

	perPartition := gatherGauge(t, "rosocp_kafka_consumer_lag")
	assert.InDelta(t, 100.0, perPartition["partition=0,topic=fresh-topic"], 0.1,
		"when committed offset is negative (unset), lag should equal high watermark")
}

func TestLagMonitor_EmptyAssignment(t *testing.T) {
	t.Cleanup(func() {
		metrics.KafkaConsumerLag.Reset()
		metrics.KafkaConsumerLagTotal.Reset()
	})

	mock := &mockLagQuerier{
		assignmentResult: nil,
	}

	lm := NewLagMonitor(mock, time.Hour)
	lm.poll()

	_ = gatherGauge(t, "rosocp_kafka_consumer_lag")
	// verify gatherGaugeVec returns collected metrics but none match our names
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() == "rosocp_kafka_consumer_lag" {
			for _, m := range mf.GetMetric() {
				g := m.GetGauge()
				if g != nil {
					assert.Fail(t, "unexpected lag metric sample on empty assignment")
				}
			}
		}
	}
}

// gatherGaugeVecSamples collects all metric samples for the given name.
func gatherGaugeVecSamples(t *testing.T, name string) []*dto.Metric {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf.GetMetric()
		}
	}
	return nil
}
