package kafka

import (
	"context"

	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// ConsumerCloseGracePeriod is the maximum time to wait for consumer.Close() during shutdown.
const ConsumerCloseGracePeriod = 30 * time.Second

// kafkaReader matches *kafka.Consumer.ReadMessage for tests.
type kafkaReader interface {
	ReadMessage(timeout time.Duration) (*kafka.Message, error)
}

// consumeMessagesUntilCancelled polls until ctx is cancelled. reader is typically the same *kafka.Consumer
// passed as consumer for handler callbacks.
func consumeMessagesUntilCancelled(ctx context.Context, reader kafkaReader, consumer *kafka.Consumer, handler func(msg *kafka.Message, consumer_object *kafka.Consumer), log *logrus.Entry) {
	for {
		select {
		case <-ctx.Done():
			log.Infof("Kafka consumer shutting down: %v", ctx.Err())
			return
		default:
		}

		msg, err := reader.ReadMessage(time.Second)
		if err == nil {
			log.Infof("Message received from kafka %s (len=%d)", msg.TopicPartition, len(msg.Value))
			log.Debugf("Message payload (truncated): %.512s", string(msg.Value))
			handler(msg, consumer)
			continue
		}
		if kerr, ok := err.(kafka.Error); ok && !kerr.IsTimeout() {
			log.Errorf("Consumer error: %v (%v)", err, msg)
		} else if !ok {
			log.Errorf("Consumer unexpected error type: %T: %v", err, err)
		}
	}
}

// StartConsumer polls kafka_topic until ctx is cancelled, then closes the consumer with a deadline.
func StartConsumer(ctx context.Context, kafka_topic string, handler func(msg *kafka.Message, consumer_object *kafka.Consumer), auto_commit_option ...bool) {
	log := logging.GetLogger()
	cfg := config.GetConfig()

	auto_commit := cfg.KafkaAutoCommit
	if len(auto_commit_option) > 0 {
		auto_commit = auto_commit_option[0]
	}

	var configMap kafka.ConfigMap
	if cfg.KafkaSASLMechanism != "" {
		configMap = kafka.ConfigMap{
			"bootstrap.servers":        cfg.KafkaBootstrapServers,
			"group.id":                 cfg.KafkaConsumerGroupId,
			"security.protocol":        cfg.KafkaSecurityProtocol,
			"sasl.mechanism":           cfg.KafkaSASLMechanism,
			"sasl.username":            cfg.KafkaUsername,
			"sasl.password":            cfg.KafkaPassword,
			"enable.auto.commit":       auto_commit,
			"go.logs.channel.enable":   true,
			"allow.auto.create.topics": false,
		}

		if cfg.KafkaCA != "" {
			configMap["ssl.ca.location"] = cfg.KafkaCA
		}

	} else {
		configMap = kafka.ConfigMap{
			"bootstrap.servers":        cfg.KafkaBootstrapServers,
			"group.id":                 cfg.KafkaConsumerGroupId,
			"enable.auto.commit":       auto_commit,
			"go.logs.channel.enable":   true,
			"allow.auto.create.topics": false,
		}
	}

	configMap["session.timeout.ms"] = 120000
	configMap["heartbeat.interval.ms"] = 30000

	consumer, err := kafka.NewConsumer(&configMap)
	if err != nil {
		log.Fatalf("Failed to create consumer: %s", err)
	}

	if err := consumer.Subscribe(kafka_topic, nil); err != nil {
		_ = consumer.Close()
		log.Fatalf("Failed to subscribe to topic %s: %s", kafka_topic, err)
	}

	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), ConsumerCloseGracePeriod)
		defer cancel()
		closeDone := make(chan error, 1)
		go func() { closeDone <- consumer.Close() }()
		select {
		case err := <-closeDone:
			if err != nil {
				log.Errorf("consumer.Close: %v", err)
			}
		case <-closeCtx.Done():
			log.Warnf("consumer.Close exceeded %v grace period", ConsumerCloseGracePeriod)
		}
	}()

	consumeMessagesUntilCancelled(ctx, consumer, consumer, handler, log)
}
