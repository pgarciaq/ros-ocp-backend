package services

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	librobnetypes "github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

var (
	invalidCSV = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_invalid_csv_total",
		Help: "The total number of invalid container csv send by cost-mgmt",
	})
	recommendationRequest = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_recommendation_request_total",
		Help: "The total number of container recommendations requested from Kruize",
	})
	namespaceRecommendationRequest = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_namespace_recommendation_request_total",
		Help: "The total number of namespace recommendations requested from Kruize",
	})
	recommendationSuccess = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_recommendation_success_total",
		Help: "The total number of container recommendations saved by ROSOCP",
	})
	namespaceRecommendationSuccess = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_namespace_recommendation_success_total",
		Help: "The total number of namespace recommendations saved by ROSOCP",
	})
	invalidNamespaceCSV = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_invalid_namespace_csv_total",
		Help: "The total number of invalid namespace csvs sent by cost-mgmt",
	})
	csvFetchError = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_csv_fetch_error_total",
		Help: "The total number of errors encountered while fetching CSV from URL",
	})
	ingestionErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "rosocp_ingestion_errors_total",
		Help: "Ingestion pipeline failures by stage (csv_parse, digest, recommend, write)",
	}, []string{"stage"})
	IngestionFileFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ros_ingestion_file_failures_total",
		Help: "Permanent per-file ingestion failures tracked in report_file_status",
	}, []string{"report_type", "error_class"})
	IngestManifestIDSynthesized = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_ingest_manifest_id_synthesized_total",
		Help: "Kafka messages that omitted metadata.manifest_id and received a deterministic synthesized manifest ID",
	})
)

// PluginHookErrors counts non-fatal failures from plugin ingest hooks (processing continues).
var PluginHookErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "ros_ocp_plugin_hook_errors_total",
	Help: "Plugin ingest hook failures (non-fatal; CSV processing continued)",
}, []string{"plugin", "hook_type"})

// malformedJSONTotal counts malformed-JSON coercions on keep-going paths
// (snapshot CSV labels, VM notification merges). The site label is bounded to
// librobne/types Site* constants — never tenant data (ADR-0243).
var malformedJSONTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "rosocp_malformed_json_total",
	Help: "Malformed JSON coerced to empty on keep-going paths, by site",
}, []string{"site"})

// WireLibrobneMalformedJSONReporter connects librobne's malformed-JSON hook
// to malformedJSONTotal. Call once at process startup; the robne CLI leaves
// the default no-op reporter in place.
func WireLibrobneMalformedJSONReporter() {
	librobnetypes.SetMalformedJSONReporter(func(site string) {
		malformedJSONTotal.WithLabelValues(site).Inc()
	})
}
