package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	kafka_internal "github.com/redhatinsights/ros-ocp-backend/internal/kafka"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

const (
	defaultMaxTransientRetries = 5
	defaultDLQTopic            = "hccm.ros.events.dlq"
	retryCountHeader           = "X-Retry-Count"
	dlqAttemptHeader           = "X-DLQ-Attempts"
	defaultMaxDLQAttempts      = 3
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
	return getHeaderCount(msg, retryCountHeader)
}

func getDLQAttemptCount(msg *kafka.Message) int {
	return getHeaderCount(msg, dlqAttemptHeader)
}

// dlqAttemptBackoff spaces DLQ re-attempts. Overridden to zero in tests.
var dlqAttemptBackoff = func(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	d := time.Second << (attempt - 1)
	if d > 8*time.Second {
		return 8 * time.Second
	}
	return d
}

func getHeaderCount(msg *kafka.Message, key string) int {
	if msg == nil {
		return 0
	}
	for _, h := range msg.Headers {
		if h.Key == key {
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

// produceDLQRedeliveryImpl re-queues a message whose DLQ produce failed so the
// DLQ attempt is retried on redelivery. Mirrors produceRetryImpl but carries
// the DLQ attempt count instead of the transient retry count.
func produceDLQRedeliveryImpl(producer kafkaMessageProducer, msg *kafka.Message, attempts int) error {
	topic := msg.TopicPartition.Topic
	if topic == nil || *topic == "" {
		return fmt.Errorf("source topic is missing from message")
	}

	headers := make([]kafka.Header, 0, len(msg.Headers)+1)
	for _, h := range msg.Headers {
		if h.Key == dlqAttemptHeader {
			continue
		}
		headers = append(headers, h)
	}
	headers = append(headers, kafka.Header{
		Key:   dlqAttemptHeader,
		Value: []byte(strconv.Itoa(attempts + 1)),
	})

	retryMsg := &kafka.Message{
		TopicPartition: msg.TopicPartition,
		Key:            msg.Key,
		Value:          msg.Value,
		Headers:        headers,
	}
	return produceToTopicImpl(producer, retryMsg, *topic, nil)
}

// markDLQFilesFailed moves a dropped message's files out of non-terminal
// report_file_status states and records the ingestion failure metric.
// Best-effort: unparsable payloads and missing DB pools are logged and skipped.
func markDLQFilesFailed(log *logrus.Entry, msg *kafka.Message, reason string) {
	if msg == nil || len(msg.Value) == 0 {
		return
	}
	var kafkaMsg types.KafkaMsg
	if err := json.Unmarshal(msg.Value, &kafkaMsg); err != nil {
		log.Errorf("kafka: unable to parse message for DLQ file marking: %v", err)
		return
	}
	pool := database.GetPool()
	if pool == nil {
		log.Warn("kafka: no DB pool; skipping DLQ file-status marking")
		return
	}
	ctx := context.Background()
	manifestID := manifestIDFromMsg(kafkaMsg)
	for i, file := range kafkaMsg.Files {
		filename := filenameForFileIndex(kafkaMsg, file, i)
		reportType := reportTypeForFilename(filename)
		if err := model.MarkReportFileFailed(ctx, pool, manifestID, filename, reason); err != nil {
			log.Errorf("kafka: failed to record DLQ file failure for %s: %v", filename, err)
			continue
		}
		recordFileFailure(log, kafkaMsg.Metadata.Org_id, kafkaMsg.Metadata.Cluster_uuid, reportType, "dlq")
	}
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
		attempts := getDLQAttemptCount(msg)
		if attempts >= defaultMaxDLQAttempts {
			log.Errorf("kafka: message exhausted %d DLQ attempts (partition=%s), dropping with alert: %v",
				defaultMaxDLQAttempts, msg.TopicPartition, kafkaTransientErr)
			metrics.KafkaDLQFailedTotal.Inc()
			markDLQFilesFailed(log, msg, kafkaTransientErr.Error())
			if consumer != nil {
				if err := kafka_internal.CommitMessage(consumer, msg); err != nil {
					log.Errorf("kafka: unable to commit after DLQ exhaustion: %v", err)
				}
			}
			return
		}
		if wait := dlqAttemptBackoff(attempts); wait > 0 {
			time.Sleep(wait)
		}
		if dlqErr := produceToDLQImpl(producer, msg, kafkaTransientErr.Error()); dlqErr != nil {
			log.Errorf("kafka: DLQ produce attempt %d failed: %v", attempts+1, dlqErr)
			if rerr := produceDLQRedeliveryImpl(producer, msg, attempts); rerr != nil {
				log.Errorf("kafka: failed to produce DLQ redelivery: %v (will redeliver naturally)", rerr)
				return
			}
			if consumer != nil {
				if err := kafka_internal.CommitMessage(consumer, msg); err != nil {
					log.Errorf("kafka: unable to commit after DLQ redelivery produce: %v", err)
				}
			}
			return
		}
		metrics.KafkaDLQMessagesTotal.Inc()
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
