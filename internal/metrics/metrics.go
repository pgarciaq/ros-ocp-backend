// Package metrics registers Prometheus collectors via promauto, which is safe if tests import this
// package multiple times (no MustRegister/double-registration panic in normal Go test runs).
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	DBQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rosocp_db_query_duration_seconds",
			Help:    "Duration of database queries",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	RecommendationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rosocp_recommendation_duration_seconds",
			Help:    "Duration of recommendation computation",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"type"},
	)

	KafkaMessagesProcessed = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "rosocp_kafka_messages_processed_total",
			Help: "Kafka messages processed successfully by the consumer handler",
		},
	)
)

// ObserveDB records elapsed time for a labeled DB operation.
func ObserveDB(operation string, start time.Time) {
	DBQueryDuration.WithLabelValues(operation).Observe(time.Since(start).Seconds())
}

// ObserveRecommendation records elapsed time for a recommendation type (container, node, gpu, pvc, namespace).
func ObserveRecommendation(typ string, start time.Time) {
	RecommendationDuration.WithLabelValues(typ).Observe(time.Since(start).Seconds())
}
