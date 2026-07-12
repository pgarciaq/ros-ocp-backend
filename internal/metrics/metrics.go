// Package metrics registers Prometheus collectors via promauto, which is safe if tests import this
// package multiple times (no MustRegister/double-registration panic in normal Go test runs).
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Bounded pipeline phase label values (ADR-0243: no tenant-specific labels).
const (
	PhaseDownload             = "download"
	PhaseParseDigest          = "parse_digest"
	PhaseWriteDigests         = "write_digests"
	PhaseRecommend            = "recommend"
	PhaseWriteRecommendations = "write_recommendations"
	PhasePostProcess          = "post_process"
	PhaseMetadataRefresh      = "metadata_refresh"
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
			Help:    "Duration of each pipeline phase in seconds (download, parse_digest, write_digests, recommend, write_recommendations, post_process, metadata_refresh)",
			Buckets: prometheus.ExponentialBuckets(0.01, 3, 12),
		},
		[]string{"phase"},
	)

	PipelineTotalDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rosocp_pipeline_total_duration_seconds",
			Help:    "End-to-end Kafka manifest processing duration in seconds",
			Buckets: prometheus.ExponentialBuckets(0.01, 3, 12),
		},
		[]string{"status"},
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

	KafkaDLQMessagesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "rosocp_kafka_dlq_messages_total",
			Help: "Kafka messages routed to the dead-letter topic after exhausting transient retries",
		},
	)

	KafkaRetriesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "rosocp_kafka_retries_total",
			Help: "Kafka messages requeued with an incremented retry count after transient processing errors",
		},
	)

	IngestGroupsInMemory = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "rosocp_ingest_groups_in_memory",
			Help: "Current number of container-day digest groups held in memory during streaming ingest",
		},
	)

	IngestFlushTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "rosocp_ingest_flush_total",
			Help: "Total incremental digest-group flush operations during streaming ingest",
		},
	)

	IngestFlushDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "rosocp_ingest_flush_duration_seconds",
			Help:    "Duration of incremental digest-group flush operations during streaming ingest",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		},
	)

	AnalyticsIncompleteTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rosocp_analytics_incomplete_total",
			Help: "Container ingestion batches where history or quality analytics writes failed (degraded mode). Per-org/cluster context is logged structurally.",
		},
		[]string{"error_type"},
	)

	CSVRowsSkipped = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rosocp_csv_rows_skipped_total",
			Help: "CSV rows skipped during parse/validation, labeled by report type",
		},
		[]string{"report_type"},
	)

	DigestRowsCapExceeded = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "rosocp_digest_rows_cap_exceeded_total",
			Help: "Number of times a cluster's digest row count exceeded ROS_MAX_DIGEST_ROWS_PER_CLUSTER, causing recommendation to be skipped",
		},
	)

	DigestRowsCapWarning = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "rosocp_digest_rows_cap_warning_total",
			Help: "Number of times a cluster reached 80% of ROS_MAX_DIGEST_ROWS_PER_CLUSTER",
		},
	)

	KafkaConsumerLag = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "rosocp_kafka_consumer_lag",
			Help: "Per-partition Kafka consumer lag (high watermark minus committed offset) for assigned partitions",
		},
		[]string{"topic", "partition"},
	)

	KafkaConsumerLagTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "rosocp_kafka_consumer_lag_total",
			Help: "Aggregate Kafka consumer lag across all assigned partitions, labeled by topic",
		},
		[]string{"topic"},
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

// ObservePipelinePhase records elapsed time for a pipeline phase.
func ObservePipelinePhase(phase string, start time.Time) {
	PipelinePhaseDuration.WithLabelValues(phase).Observe(time.Since(start).Seconds())
}

// ObservePhase runs fn and records its wall-clock duration under phase.
func ObservePhase(phase string, fn func() error) error {
	start := time.Now()
	err := fn()
	PipelinePhaseDuration.WithLabelValues(phase).Observe(time.Since(start).Seconds())
	return err
}

// ObservePipelineTotal records end-to-end manifest processing duration.
// status must be "success" or "error".
func ObservePipelineTotal(status string, start time.Time) {
	PipelineTotalDuration.WithLabelValues(status).Observe(time.Since(start).Seconds())
}

// IncRecommendationsWritten increments the written counter for a recommendation type.
func IncRecommendationsWritten(typ string, count int) {
	RecommendationsWritten.WithLabelValues(typ).Add(float64(count))
}

// SetIngestGroupsInMemory updates the in-memory digest group gauge during streaming ingest.
func SetIngestGroupsInMemory(count int) {
	IngestGroupsInMemory.Set(float64(count))
}

// ObserveIngestFlush records duration for an incremental digest-group flush.
func ObserveIngestFlush(start time.Time) {
	IngestFlushDuration.Observe(time.Since(start).Seconds())
}

// IncIngestFlushTotal increments the incremental flush counter.
func IncIngestFlushTotal() {
	IngestFlushTotal.Inc()
}

// IncAnalyticsIncomplete increments the analytics-incomplete counter for a cluster batch failure.
// errorType must be "history" or "quality". orgID and clusterUUID are accepted for call-site
// compatibility; tenant context must be logged at the call site.
func IncAnalyticsIncomplete(orgID, clusterUUID, errorType string) {
	_ = orgID
	_ = clusterUUID
	AnalyticsIncompleteTotal.WithLabelValues(errorType).Inc()
}

// IncCSVRowsSkipped increments the skipped-row counter for a report type (e.g. container, vm).
func IncCSVRowsSkipped(reportType string, count int) {
	if count <= 0 {
		return
	}
	CSVRowsSkipped.WithLabelValues(reportType).Add(float64(count))
}
