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

	PipelinePhaseDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rosocp_pipeline_phase_duration_seconds",
			Help:    "Duration of individual pipeline phases (digest, recommend, write, quality, history, gpu_enrichment)",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		},
		[]string{"phase"},
	)

	RecommendationsWritten = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rosocp_recommendations_written_total",
			Help: "Total recommendations written, labeled by type (container, namespace, node, pvc, snapshot)",
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

// ObservePipelinePhase records elapsed time for a pipeline phase (digest, recommend, write, etc.).
func ObservePipelinePhase(phase string, start time.Time) {
	PipelinePhaseDuration.WithLabelValues(phase).Observe(time.Since(start).Seconds())
}

// IncRecommendationsWritten increments the written counter for a recommendation type.
func IncRecommendationsWritten(typ string, count int) {
	RecommendationsWritten.WithLabelValues(typ).Add(float64(count))
}
