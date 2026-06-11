package kafka

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

const kafkaMessageSampleInterval = 100

// ConsumerCloseGracePeriod is the maximum time to wait for consumer.Close() during shutdown.
const ConsumerCloseGracePeriod = 30 * time.Second

// kafkaReader matches *kafka.Consumer.ReadMessage for tests.
type kafkaReader interface {
	ReadMessage(timeout time.Duration) (*kafka.Message, error)
}

func partitionLockKey(tp kafka.TopicPartition) string {
	return fmt.Sprintf("%s:%d", *tp.Topic, tp.Partition)
}

// consumeMessagesUntilCancelled polls until ctx is cancelled. reader is typically the same *kafka.Consumer
// passed as consumer for handler callbacks.
func consumeMessagesUntilCancelled(ctx context.Context, reader kafkaReader, consumer *kafka.Consumer, handler func(msg *kafka.Message, consumer_object *kafka.Consumer), log *logrus.Entry) {
	cfg := config.GetConfig()
	if cfg.KafkaParallel && cfg.KafkaWorkers > 1 {
		consumeMessagesParallelUntilCancelled(ctx, reader, consumer, handler, log, cfg.KafkaWorkers)
		return
	}
	consumeMessagesSequentialUntilCancelled(ctx, reader, consumer, handler, log)
}

func consumeMessagesSequentialUntilCancelled(ctx context.Context, reader kafkaReader, consumer *kafka.Consumer, handler func(msg *kafka.Message, consumer_object *kafka.Consumer), log *logrus.Entry) {
	var msgCount uint64
	batchStart := time.Now()
	for {
		select {
		case <-ctx.Done():
			log.Infof("Kafka consumer shutting down: %v", ctx.Err())
			return
		default:
		}

		msg, err := reader.ReadMessage(time.Second)
		if err == nil {
			logKafkaMessageReceived(log, &msgCount, &batchStart, msg)
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

// consumeMessagesParallelUntilCancelled runs a worker pool with per-partition mutexes
// for handler ordering. ADR-0154: Partition-scoped parallelism; offset commits must use
// kafka.CommitMessage (serialized mutex) because librdkafka consumers are not thread-safe
// for concurrent CommitMessage calls.
func consumeMessagesParallelUntilCancelled(
	ctx context.Context,
	reader kafkaReader,
	consumer *kafka.Consumer,
	handler func(msg *kafka.Message, consumer_object *kafka.Consumer),
	log *logrus.Entry,
	workers int,
) {
	jobs := make(chan *kafka.Message, workers*2)
	var wg sync.WaitGroup
	var partitionLocks sync.Map
	var msgCount uint64
	batchStart := time.Now()

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for msg := range jobs {
				lockKey := partitionLockKey(msg.TopicPartition)
				muIface, _ := partitionLocks.LoadOrStore(lockKey, &sync.Mutex{})
				mu := muIface.(*sync.Mutex)
				mu.Lock()
				logKafkaMessageReceived(log, &msgCount, &batchStart, msg)
				handler(msg, consumer)
				mu.Unlock()
			}
		}()
	}

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer close(jobs)
		for {
			select {
			case <-ctx.Done():
				log.Infof("Kafka consumer shutting down: %v", ctx.Err())
				return
			default:
			}

			msg, err := reader.ReadMessage(time.Second)
			if err == nil {
				select {
				case jobs <- msg:
				case <-ctx.Done():
					return
				}
				continue
			}
			if kerr, ok := err.(kafka.Error); ok && !kerr.IsTimeout() {
				log.Errorf("Consumer error: %v (%v)", err, msg)
			} else if !ok {
				log.Errorf("Consumer unexpected error type: %T: %v", err, err)
			}
		}
	}()

	<-readDone
	wg.Wait()
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

func logKafkaMessageReceived(log *logrus.Entry, msgCount *uint64, batchStart *time.Time, msg *kafka.Message) {
	log.Debugf("Message received from kafka %s (len=%d)", msg.TopicPartition, len(msg.Value))
	n := atomic.AddUint64(msgCount, 1)
	if n%kafkaMessageSampleInterval == 0 {
		elapsed := time.Since(*batchStart)
		log.Infof("Processed %d kafka messages in %s", kafkaMessageSampleInterval, elapsed.Round(time.Millisecond))
		*batchStart = time.Now()
	}
}
