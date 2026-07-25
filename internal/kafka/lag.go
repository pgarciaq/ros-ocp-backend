package kafka

import (
	"context"
	"strconv"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

const watermarkTimeoutMs = 5000

// LagQuerier abstracts the Kafka consumer methods needed by LagMonitor, enabling unit tests
// without a real broker.
type LagQuerier interface {
	Assignment() ([]kafka.TopicPartition, error)
	QueryWatermarkOffsets(topic string, partition int32, timeoutMs int) (low, high int64, err error)
	Committed(partitions []kafka.TopicPartition, timeoutMs int) ([]kafka.TopicPartition, error)
}

// LagMonitor periodically polls Kafka for consumer lag on assigned partitions and updates
// Prometheus gauge metrics.
type LagMonitor struct {
	querier  LagQuerier
	interval time.Duration
	log      *logrus.Entry
}

// NewLagMonitor creates a LagMonitor that polls querier at the given interval.
func NewLagMonitor(querier LagQuerier, pollInterval time.Duration) *LagMonitor {
	return &LagMonitor{
		querier:  querier,
		interval: pollInterval,
		log:      logging.GetLogger(),
	}
}

// Start begins the polling goroutine. It blocks until ctx is cancelled.
func (lm *LagMonitor) Start(ctx context.Context) {
	ticker := time.NewTicker(lm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			lm.log.Info("Kafka lag monitor shutting down")
			metrics.KafkaConsumerLag.Reset()
			metrics.KafkaConsumerLagTotal.Reset()
			return
		case <-ticker.C:
			lm.poll()
		}
	}
}

// poll queries the broker for current lag across assigned partitions.
func (lm *LagMonitor) poll() {
	assigned, err := lm.querier.Assignment()
	if err != nil {
		lm.log.Warnf("Kafka lag monitor: Assignment() error: %v", err)
		return
	}
	if len(assigned) == 0 {
		return
	}

	// Reset before each poll to drop partitions lost after a rebalance.
	metrics.KafkaConsumerLag.Reset()

	committed, err := lm.querier.Committed(assigned, watermarkTimeoutMs)
	if err != nil {
		lm.log.Warnf("Kafka lag monitor: Committed() error: %v", err)
		return
	}

	commitIndex := make(map[string]kafka.Offset, len(committed))
	for _, tp := range committed {
		key := partitionKey(*tp.Topic, tp.Partition)
		commitIndex[key] = tp.Offset
	}

	topicLag := make(map[string]int64)
	for _, tp := range assigned {
		topic := *tp.Topic
		partition := tp.Partition

		_, high, err := lm.querier.QueryWatermarkOffsets(topic, partition, watermarkTimeoutMs)
		if err != nil {
			lm.log.Debugf("Kafka lag monitor: watermark error for %s[%d]: %v", topic, partition, err)
			continue
		}

		key := partitionKey(topic, partition)
		committedOffset, ok := commitIndex[key]
		if !ok || int64(committedOffset) < 0 {
			committedOffset = 0
		}

		lag := high - int64(committedOffset)
		if lag < 0 {
			lag = 0
		}

		partLabel := strconv.Itoa(int(partition))
		metrics.KafkaConsumerLag.WithLabelValues(topic, partLabel).Set(float64(lag))
		topicLag[topic] += lag
	}

	for topic, total := range topicLag {
		metrics.KafkaConsumerLagTotal.WithLabelValues(topic).Set(float64(total))
	}
}
