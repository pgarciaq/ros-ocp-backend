package reship

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	ReshipInProgress = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ros_reship_in_progress",
			Help: "1 while a masu reship_ros call is in flight for a cluster, 0 otherwise",
		},
		[]string{"org_id", "cluster_uuid"},
	)

	ReshipFilesProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ros_reship_files_processed",
			Help: "Total ROS files published to Kafka by masu reship_ros",
		},
		[]string{"org_id"},
	)

	ReshipDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ros_reship_duration_seconds",
			Help:    "Duration of masu reship_ros HTTP calls",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		},
		[]string{"org_id"},
	)

	ReshipFailuresTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ros_reship_failures_total",
			Help: "Reship attempts that exhausted consecutive retry budget",
		},
		[]string{"org_id"},
	)

	ReshipProviderResolutionFailuresTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ros_reship_provider_resolution_failures_total",
			Help: "Failures resolving cluster_uuid to provider_uuid via masu effective_rates",
		},
		[]string{"org_id", "reason"},
	)
)

func observeReshipStart(orgID, clusterUUID string) {
	ReshipInProgress.WithLabelValues(orgID, clusterUUID).Set(1)
}

func observeReshipEnd(orgID, clusterUUID string, start time.Time, filesProcessed int) {
	ReshipInProgress.WithLabelValues(orgID, clusterUUID).Set(0)
	ReshipDurationSeconds.WithLabelValues(orgID).Observe(time.Since(start).Seconds())
	if filesProcessed > 0 {
		ReshipFilesProcessed.WithLabelValues(orgID).Add(float64(filesProcessed))
	}
}

func incReshipFailures(orgID string) {
	ReshipFailuresTotal.WithLabelValues(orgID).Inc()
}

func recordProviderResolutionFailure(orgID, clusterUUID string, attempt int, err error) {
	reason, statusCode := resolutionFailureDetails(err)
	ReshipProviderResolutionFailuresTotal.WithLabelValues(orgID, reason).Inc()
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
