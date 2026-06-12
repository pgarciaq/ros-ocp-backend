package reship

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	ReshipInProgress = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "ros_reship_in_progress",
			Help: "Number of masu reship_ros calls currently in flight across all orgs/clusters",
		},
	)

	ReshipFilesProcessed = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "ros_reship_files_processed",
			Help: "Total ROS files published to Kafka by masu reship_ros",
		},
	)

	ReshipDurationSeconds = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "ros_reship_duration_seconds",
			Help:    "Duration of masu reship_ros HTTP calls",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		},
	)

	ReshipFailuresTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "ros_reship_failures_total",
			Help: "Reship attempts that exhausted consecutive retry budget",
		},
	)

	ReshipProviderResolutionFailuresTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ros_reship_provider_resolution_failures_total",
			Help: "Failures resolving cluster_uuid to provider_uuid via masu effective_rates",
		},
		[]string{"reason"},
	)

	ReshipFallbackForwardOnlyTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "ros_reship_fallback_forward_only_total",
			Help: "Clusters transitioned to forward-only BH recommendations after reship retry exhaustion",
		},
	)
)

func observeReshipStart(orgID, clusterUUID string) {
	ReshipInProgress.Inc()
	reshipLog.WithFields(map[string]interface{}{
		"msg":          "reship started",
		"org_id":       orgID,
		"cluster_uuid": clusterUUID,
	}).Info("reship started")
}

func observeReshipEnd(orgID, clusterUUID string, start time.Time, filesProcessed int) {
	ReshipInProgress.Dec()
	ReshipDurationSeconds.Observe(time.Since(start).Seconds())
	if filesProcessed > 0 {
		ReshipFilesProcessed.Add(float64(filesProcessed))
	}
	reshipLog.WithFields(map[string]interface{}{
		"msg":             "reship completed",
		"org_id":          orgID,
		"cluster_uuid":    clusterUUID,
		"duration_sec":    time.Since(start).Seconds(),
		"files_processed": filesProcessed,
	}).Info("reship completed")
}

func incReshipFailures(orgID string) {
	ReshipFailuresTotal.Inc()
	reshipLog.WithFields(map[string]interface{}{
		"msg":    "reship retries exhausted",
		"org_id": orgID,
	}).Error("reship retries exhausted")
}

func incReshipFallbackForwardOnly(orgID string) {
	ReshipFallbackForwardOnlyTotal.Inc()
	reshipLog.WithFields(map[string]interface{}{
		"msg":    "reship fallback to forward-only recommendations",
		"org_id": orgID,
	}).Warn("reship fallback to forward-only recommendations")
}

func recordProviderResolutionFailure(orgID, clusterUUID string, attempt int, err error) {
	reason, statusCode := resolutionFailureDetails(err)
	ReshipProviderResolutionFailuresTotal.WithLabelValues(reason).Inc()
	fields := map[string]interface{}{
		"msg":           "provider_uuid resolution failed; reship deferred",
		"org_id":        orgID,
		"cluster_uuid":  clusterUUID,
		"reason":        reason,
		"retry_attempt": attempt,
	}
	if statusCode > 0 {
		fields["http_status"] = statusCode
	}
	reshipLog.WithFields(fields).Warn(err.Error())
}
