package kafka

import (
	"context"
	"runtime/debug"
	"strconv"
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

// partitionKey builds a "topic:partition" string without fmt.Sprintf overhead.
// Exported for use by lag.go; call sites in consumer.go use it via partitionLockKey alias.
func partitionKey(topic string, partition int32) string {
	return topic + ":" + strconv.Itoa(int(partition))
}

func partitionLockKey(tp kafka.TopicPartition) string {
	return partitionKey(*tp.Topic, tp.Partition)
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

// consumeMessagesParallelUntilCancelled routes each partition's messages to an
// ordered queue served by a dedicated goroutine (#756, ADR-0154): handler
// execution and commits within a partition follow offset order by
// construction — the old shared-jobs-channel design only guaranteed mutual
// exclusion per partition, and mutex barging let a higher offset run and
// commit before a lower one. Cross-partition parallelism is bounded by
// maxConcurrent (the DB-connection budget: one handler holds one connection,
// matching the ManifestDownloadWorkers × KafkaWorkers ≤ DBMaxConns − 2
// constraint in internal/config).
func consumeMessagesParallelUntilCancelled(
	ctx context.Context,
	reader kafkaReader,
	consumer *kafka.Consumer,
	handler func(msg *kafka.Message, consumer_object *kafka.Consumer),
	log *logrus.Entry,
	maxConcurrent int,
) {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}

	var wg sync.WaitGroup // partition goroutines
	sem := make(chan struct{}, maxConcurrent)
	var partitions sync.Map // partitionLockKey -> chan *kafka.Message
	var msgCount uint64
	batchStart := time.Now()

	// The dispatcher reads messages and routes each to its partition's queue,
	// spawning that partition's goroutine on first sight. On exit (ctx
	// cancelled) it stops; the queues are closed below so the partition
	// goroutines drain what is buffered before returning — the same
	// drain-on-shutdown contract the shared jobs channel had.
	dispatchDone := make(chan struct{})
	go func() {
		defer close(dispatchDone)
		for {
			select {
			case <-ctx.Done():
				log.Infof("Kafka consumer shutting down: %v", ctx.Err())
				return
			default:
			}

			msg, err := reader.ReadMessage(time.Second)
			if err == nil {
				lockKey := partitionLockKey(msg.TopicPartition)
				qIface, loaded := partitions.LoadOrStore(lockKey, make(chan *kafka.Message, partitionQueueSize))
				q := qIface.(chan *kafka.Message)
				if !loaded {
					wg.Add(1)
					go servePartitionQueue(q, consumer, handler, log, sem, &msgCount, &batchStart, &wg)
				}
				select {
				case q <- msg:
				case <-ctx.Done():
					return
				}
				continue
			}
			if kerr, ok := err.(kafka.Error); ok && !kerr.IsTimeout() {
				log.Errorf("Consumer error: %v", err)
			} else if !ok {
				log.Errorf("Consumer unexpected error type: %T: %v", err, err)
			}
		}
	}()

	<-dispatchDone
	partitions.Range(func(_, qIface any) bool {
		close(qIface.(chan *kafka.Message))
		return true
	})
	wg.Wait()
}

// partitionQueueSize bounds one partition's buffered messages. A hot partition
// fills its queue and blocks the dispatcher's send to it (backpressure), so
// memory stays bounded — the same head-of-line property the old shared jobs
// channel had. Other partitions' QUEUED messages keep processing while the
// dispatcher waits; they just stop receiving new ones until the hot partition
// drains.
const partitionQueueSize = 16

// servePartitionQueue drains one partition's queue in offset order: execution
// within the partition is serial by construction (#756), with the semaphore
// bounding how many partitions run handlers at once (the DB-connection
// budget). A panicked message is "done" for ordering purposes: recover,
// commit (skip), and continue with the next offset — the same policy as
// wrapHandlerWithInFlight (see the rationale there).
func servePartitionQueue(
	q chan *kafka.Message,
	consumer *kafka.Consumer,
	handler func(msg *kafka.Message, consumer_object *kafka.Consumer),
	log *logrus.Entry,
	sem chan struct{},
	msgCount *uint64,
	batchStart *time.Time,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	for msg := range q {
		func() {
			defer func() {
				if r := recover(); r != nil {
					handlerPanics.Inc()
					log.Errorf("panic in Kafka partition worker (partition=%s): %v\n%s",
						msg.TopicPartition, r, debug.Stack())
					if consumer != nil {
						if err := CommitMessage(consumer, msg); err != nil {
							log.Errorf("unable to commit after panic recovery: %v", err)
						}
					}
				}
			}()
			sem <- struct{}{}
			defer func() { <-sem }()
			logKafkaMessageReceived(log, msgCount, batchStart, msg)
			handler(msg, consumer)
		}()
	}
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

	configMap["session.timeout.ms"] = cfg.KafkaSessionTimeoutMS
	configMap["heartbeat.interval.ms"] = cfg.KafkaHeartbeatIntervalMS

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
