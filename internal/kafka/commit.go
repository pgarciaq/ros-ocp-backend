package kafka

import (
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// MessageCommitter commits Kafka offsets for a consumed message.
type MessageCommitter interface {
	CommitMessage(m *kafka.Message) ([]kafka.TopicPartition, error)
}

// CommitMessage commits a Kafka offset.
//
// #756/ADR-0154: with partition-ordered queues, commits within a partition
// are serial by construction — the handler tail (report_processor /
// kafka_retry) and the panic-recovery commit both run in the partition's own
// goroutine. Cross-partition commits are safe without serialization:
// librdkafka's API is completely thread-safe ("the calling application may
// call any of the API functions from any of its own threads at any time" —
// librdkafka INTRODUCTION.md, Threads and callbacks). The global mutex this
// function used to take serialized commit calls without delivering offset
// ordering (the V15-06 finding) and is therefore removed.
func CommitMessage(consumer MessageCommitter, msg *kafka.Message) error {
	if consumer == nil || msg == nil {
		return nil
	}
	_, err := consumer.CommitMessage(msg)
	return err
}
