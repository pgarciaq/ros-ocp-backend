package services

import (
	"fmt"
	"strconv"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	kafka_internal "github.com/redhatinsights/ros-ocp-backend/internal/kafka"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

const (
	defaultMaxTransientRetries = 5
	defaultDLQTopic            = "hccm.ros.events.dlq"
	retryCountHeader           = "X-Retry-Count"
)

const (
	headerOriginalTopic     = "X-Original-Topic"
	headerOriginalPartition = "X-Original-Partition"
	headerFailureReason     = "X-Failure-Reason"
	headerFailedAt          = "X-Failed-At"
)

type kafkaMessageProducer interface {
	Produce(msg *kafka.Message, deliveryChan chan kafka.Event) error
}

type kafkaCommitter interface {
	CommitMessage(m *kafka.Message) ([]kafka.TopicPartition, error)
}

func maxTransientRetries() int {
	cfg := config.GetConfig()
	if cfg.KafkaMaxTransientRetries > 0 {
		return cfg.KafkaMaxTransientRetries
	}
	return defaultMaxTransientRetries
}

func dlqTopicName() string {
	cfg := config.GetConfig()
	if cfg.KafkaDLQTopic != "" {
		return cfg.KafkaDLQTopic
	}
	return defaultDLQTopic
}

func getRetryCount(msg *kafka.Message) int {
	if msg == nil {
		return 0
	}
	for _, h := range msg.Headers {
		if h.Key == retryCountHeader {
			n, err := strconv.Atoi(string(h.Value))
			if err != nil {
				return 0
			}
			return n
		}
	}
	return 0
}

func produceToTopic(producer *kafka.Producer, msg *kafka.Message, topic string, extraHeaders []kafka.Header) error {
	if producer == nil {
		return fmt.Errorf("kafka producer is nil")
	}
	return produceToTopicImpl(producer, msg, topic, extraHeaders)
}

func produceToTopicImpl(producer kafkaMessageProducer, msg *kafka.Message, topic string, extraHeaders []kafka.Header) error {
	if msg == nil {
		return fmt.Errorf("kafka message is nil")
	}
	if topic == "" {
		return fmt.Errorf("kafka topic is empty")
	}

	headers := make([]kafka.Header, 0, len(msg.Headers)+len(extraHeaders))
	headers = append(headers, msg.Headers...)
	headers = append(headers, extraHeaders...)

	out := &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Key:            msg.Key,
		Value:          msg.Value,
		Headers:        headers,
	}

	deliveryChan := make(chan kafka.Event, 1)
	if err := producer.Produce(out, deliveryChan); err != nil {
		return fmt.Errorf("produce failed: %w", err)
	}

	e := <-deliveryChan
	m, ok := e.(*kafka.Message)
	if !ok {
		return fmt.Errorf("unexpected delivery event type: %T", e)
	}
	if m.TopicPartition.Error != nil {
		return fmt.Errorf("delivery failed: %w", m.TopicPartition.Error)
	}
	return nil
}

func produceToDLQ(producer *kafka.Producer, msg *kafka.Message, failureReason string) error {
	if producer == nil {
		return fmt.Errorf("kafka producer is nil")
	}
	return produceToDLQImpl(producer, msg, failureReason)
}

func produceToDLQImpl(producer kafkaMessageProducer, msg *kafka.Message, failureReason string) error {
	topic := msg.TopicPartition.Topic
	partition := msg.TopicPartition.Partition
	originalTopic := ""
	if topic != nil {
		originalTopic = *topic
	}

	extraHeaders := []kafka.Header{
		{Key: headerOriginalTopic, Value: []byte(originalTopic)},
		{Key: headerOriginalPartition, Value: []byte(strconv.Itoa(int(partition)))},
		{Key: headerFailureReason, Value: []byte(failureReason)},
		{Key: headerFailedAt, Value: []byte(time.Now().UTC().Format(time.RFC3339))},
	}
	return produceToTopicImpl(producer, msg, dlqTopicName(), extraHeaders)
}

func produceRetry(producer *kafka.Producer, msg *kafka.Message, currentRetryCount int) error {
	if producer == nil {
		return fmt.Errorf("kafka producer is nil")
	}
	return produceRetryImpl(producer, msg, currentRetryCount)
}

func produceRetryImpl(producer kafkaMessageProducer, msg *kafka.Message, currentRetryCount int) error {
	topic := msg.TopicPartition.Topic
	if topic == nil || *topic == "" {
		return fmt.Errorf("source topic is missing from message")
	}

	headers := make([]kafka.Header, 0, len(msg.Headers)+1)
	for _, h := range msg.Headers {
		if h.Key == retryCountHeader {
			continue
		}
		headers = append(headers, h)
	}
	headers = append(headers, kafka.Header{
		Key:   retryCountHeader,
		Value: []byte(strconv.Itoa(currentRetryCount + 1)),
	})

	retryMsg := &kafka.Message{
		TopicPartition: msg.TopicPartition,
		Key:            msg.Key,
		Value:          msg.Value,
		Headers:        headers,
	}
	return produceToTopicImpl(producer, retryMsg, *topic, nil)
}

func handleKafkaTransientError(consumer kafkaCommitter, producer kafkaMessageProducer, msg *kafka.Message, kafkaTransientErr error) {
	log := logging.GetLogger()
	if kafkaTransientErr == nil {
		return
	}

	retries := getRetryCount(msg)
	maxRetries := maxTransientRetries()

	if retries >= maxRetries {
		log.Errorf("kafka: message exhausted %d retries (partition=%s), routing to DLQ: %v",
			maxRetries, msg.TopicPartition, kafkaTransientErr)
		metrics.KafkaDLQMessagesTotal.Inc()
		if dlqErr := produceToDLQImpl(producer, msg, kafkaTransientErr.Error()); dlqErr != nil {
			log.Errorf("kafka: failed to produce to DLQ: %v (original error: %v)", dlqErr, kafkaTransientErr)
			return
		}
		if consumer != nil {
			if err := kafka_internal.CommitMessage(consumer, msg); err != nil {
				log.Errorf("kafka: unable to commit after DLQ: %v", err)
			}
		}
		return
	}

	log.Warnf("kafka: transient error (attempt %d/%d, partition=%s), requeueing: %v",
		retries+1, maxRetries, msg.TopicPartition, kafkaTransientErr)
	metrics.KafkaRetriesTotal.Inc()
	if retryErr := produceRetryImpl(producer, msg, retries); retryErr != nil {
		log.Errorf("kafka: failed to produce retry: %v (will redeliver naturally)", retryErr)
		return
	}
	if consumer != nil {
		if err := kafka_internal.CommitMessage(consumer, msg); err != nil {
			log.Errorf("kafka: unable to commit after retry produce: %v", err)
		}
	}
}

func kafkaProducerForRetry() kafkaMessageProducer {
	return kafka_internal.GetProducer()
}
