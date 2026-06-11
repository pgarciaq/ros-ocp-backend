package kafka

import (
	"sync"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// MessageCommitter commits Kafka offsets for a consumed message.
type MessageCommitter interface {
	CommitMessage(m *kafka.Message) ([]kafka.TopicPartition, error)
}

var parallelCommitMu sync.Mutex

// CommitMessage commits a Kafka offset. ADR-0154: Serialize commits when parallel workers enabled.
// When ROS_KAFKA_PARALLEL is enabled with multiple workers, commits are serialized because
func CommitMessage(consumer MessageCommitter, msg *kafka.Message) error {
	if consumer == nil || msg == nil {
		return nil
	}
	cfg := config.GetConfig()
	if cfg != nil && cfg.KafkaParallel && cfg.KafkaWorkers > 1 {
		parallelCommitMu.Lock()
		defer parallelCommitMu.Unlock()
	}
	_, err := consumer.CommitMessage(msg)
	return err
}
