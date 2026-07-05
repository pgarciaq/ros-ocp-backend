package kafka

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

const kafkaMessageSampleInterval = 100

// ConsumerCloseGracePeriod is the maximum time to wait for consumer.Close() during shutdown.
const ConsumerCloseGracePeriod = 30 * time.Second

var handlerPanics = promauto.NewCounter(prometheus.CounterOpts{
	Name: "rosocp_kafka_handler_panics_total",
	Help: "Number of panics recovered in Kafka message handler goroutines",
})

// MessageHandler processes a single Kafka message. ctx is cancelled on processor shutdown (SIGTERM).
type MessageHandler func(ctx context.Context, msg *kafka.Message, consumer *kafka.Consumer)

// kafkaReader matches *kafka.Consumer.ReadMessage for tests.
type kafkaReader interface {
	ReadMessage(timeout time.Duration) (*kafka.Message, error)
}

func partitionLockKey(tp kafka.TopicPartition) string {
	return fmt.Sprintf("%s:%d", *tp.Topic, tp.Partition)
}

func wrapHandlerWithInFlight(ctx context.Context, handler MessageHandler, inFlight *sync.WaitGroup) func(*kafka.Message, *kafka.Consumer) {
	log := logging.GetLogger()
	return func(msg *kafka.Message, consumer *kafka.Consumer) {
		inFlight.Add(1)
		defer inFlight.Done()
		defer func() {
			if r := recover(); r != nil {
				handlerPanics.Inc()
				log.Errorf("panic in Kafka message handler (partition=%s): %v\n%s",
					msg.TopicPartition, r, debug.Stack())
				// Commit (skip) the panicking message rather than retrying. Rationale:
				// panics here are caused by malformed message data (nil dereference,
				// OOB access, type assertion failure) — retrying would panic again
				// indefinitely, blocking the partition. Transient issues (DB unavailable,
				// OOM) manifest as errors not panics. Monitor rosocp_kafka_handler_panics_total
				// to detect data-quality issues that need manual investigation.
				if consumer != nil {
					if err := CommitMessage(consumer, msg); err != nil {
						log.Errorf("unable to commit after panic recovery: %v", err)
					}
				}
			}
		}()
		handler(ctx, msg, consumer)
	}
}

// drainInFlightHandlers waits for handlers to finish after the consumer loop stops accepting messages.
func drainInFlightHandlers(log *logrus.Entry, inFlight *sync.WaitGroup) {
	drainInFlightHandlersWithTimeout(log, inFlight, time.Duration(config.GetConfig().ShutdownTimeoutSecs)*time.Second)
}

func drainInFlightHandlersWithTimeout(log *logrus.Entry, inFlight *sync.WaitGroup, timeout time.Duration) {
	drainCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		inFlight.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info("Kafka consumer: all in-flight message handlers completed")
	case <-drainCtx.Done():
		log.Warnf("Kafka consumer: shutdown drain exceeded %v; exiting with handlers still running", timeout)
	}
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
				func() {
					defer mu.Unlock()
				defer func() {
					if r := recover(); r != nil {
						handlerPanics.Inc()
						log.Errorf("panic in Kafka parallel worker (partition=%s): %v\n%s",
							msg.TopicPartition, r, debug.Stack())
						// Commit (skip) — see rationale in wrapHandlerWithInFlight.
						if consumer != nil {
							if err := CommitMessage(consumer, msg); err != nil {
								log.Errorf("unable to commit after panic recovery: %v", err)
							}
						}
					}
				}()
					logKafkaMessageReceived(log, &msgCount, &batchStart, msg)
					handler(msg, consumer)
				}()
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

// StartConsumer polls kafka_topic until ctx is cancelled, drains in-flight handlers, then closes the consumer.
func StartConsumer(ctx context.Context, kafka_topic string, handler MessageHandler, auto_commit_option ...bool) {
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

	lagInterval := time.Duration(cfg.KafkaLagPollIntervalSecs) * time.Second
	lagMon := NewLagMonitor(consumer, lagInterval)
	go lagMon.Start(ctx)

	var inFlight sync.WaitGroup
	wrappedHandler := wrapHandlerWithInFlight(ctx, handler, &inFlight)

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

	consumeMessagesUntilCancelled(ctx, consumer, consumer, wrappedHandler, log)
	drainInFlightHandlers(log, &inFlight)
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
