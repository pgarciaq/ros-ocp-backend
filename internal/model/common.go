package model

import (
	"gorm.io/gorm"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	kruizeplugin "github.com/redhatinsights/ros-ocp-backend/internal/plugins/kruize"
)

const (
	NamespaceMaxLen = 63
	ClusterMaxLen   = 253
)

type StoredVariationPcts = kruizeplugin.StoredVariationPcts
type SynthDBRow = kruizeplugin.SynthDBRow
type StoredVariationSpec = kruizeplugin.StoredVariationSpec
type RecommendationColumnValues = kruizeplugin.RecommendationColumnValues

var StoredVariationSpecs = kruizeplugin.StoredVariationSpecs

var ExtractRecommendationColumnValues = kruizeplugin.ExtractRecommendationColumnValues

// getRecommendationQuery reads denormalized recommendation_sets columns only
// (#600). The workloads/clusters JOINs are gone: workloads has zero rows
// and zero writers, so every COALESCE left branch was dead and display
// output is byte-identical without them. Do not re-add linkage here.
func getRecommendationQuery(orgID string) *gorm.DB {
	db := database.GetDB()
	query := db.Table("recommendation_sets").
		Select(
			"recommendation_sets.container_id AS id, "+
				"recommendation_sets.container_name AS container, "+
				"recommendation_sets.namespace AS project, "+
				"recommendation_sets.workload AS workload, "+
				"recommendation_sets.workload_type AS workload_type, "+
				"'' AS source_id, "+
				"recommendation_sets.cluster_uuid AS cluster_uuid, "+
				"recommendation_sets.cluster_uuid::text AS cluster_alias, "+
				"recommendation_sets.updated_at AS last_reported, "+
				"false AS analytics_incomplete, "+
				"NULL AS analytics_incomplete_at, "+
				"recommendation_sets.term, "+
				"recommendation_sets.engine, "+
				"recommendation_sets.rec_cpu_request_millicores, "+
				"recommendation_sets.rec_cpu_limit_millicores, "+
				"recommendation_sets.rec_memory_request_kib, "+
				"recommendation_sets.rec_memory_limit_kib, "+
				"recommendation_sets.current_cpu_request_millicores, "+
				"recommendation_sets.current_cpu_limit_millicores, "+
				"recommendation_sets.current_memory_request_kib, "+
				"recommendation_sets.current_memory_limit_kib, "+
				"recommendation_sets.monitoring_start_time, "+
				"recommendation_sets.monitoring_end_time, "+
				"recommendation_sets.notification_codes::text AS notification_codes, "+
				"recommendation_sets.recommendations, "+
				"recommendation_sets.cpu_variation_short_cost_pct, "+
				"recommendation_sets.cpu_variation_short_performance_pct, "+
				"recommendation_sets.cpu_variation_medium_cost_pct, "+
				"recommendation_sets.cpu_variation_medium_performance_pct, "+
				"recommendation_sets.cpu_variation_long_cost_pct, "+
				"recommendation_sets.cpu_variation_long_performance_pct, "+
				"recommendation_sets.memory_variation_short_cost_pct, "+
				"recommendation_sets.memory_variation_short_performance_pct, "+
				"recommendation_sets.memory_variation_medium_cost_pct, "+
				"recommendation_sets.memory_variation_medium_performance_pct, "+
				"recommendation_sets.memory_variation_long_cost_pct, "+
				"recommendation_sets.memory_variation_long_performance_pct").
		Model(&RecommendationSetResult{}).
		Where("recommendation_sets.org_id = ?", orgID)
	return query
}

func getNamespaceRecommendationQuery(orgID string) *gorm.DB {
	db := database.GetDB()
	query := db.Table("namespace_recommendation_sets").
		Select("COALESCE(namespace_recommendation_sets.namespace_id, namespace_recommendation_sets.id::text) AS id, "+
			"namespace_recommendation_sets.namespace_name AS project, "+
			"clusters.source_id, "+
			"clusters.cluster_uuid, "+
			"clusters.cluster_alias, "+
			"clusters.last_reported_at AS last_reported, "+
			"namespace_recommendation_sets.recommendations, "+
			"namespace_recommendation_sets.cpu_variation_short_cost_pct, "+
			"namespace_recommendation_sets.cpu_variation_short_performance_pct, "+
			"namespace_recommendation_sets.cpu_variation_medium_cost_pct, "+
			"namespace_recommendation_sets.cpu_variation_medium_performance_pct, "+
			"namespace_recommendation_sets.cpu_variation_long_cost_pct, "+
			"namespace_recommendation_sets.cpu_variation_long_performance_pct, "+
			"namespace_recommendation_sets.memory_variation_short_cost_pct, "+
			"namespace_recommendation_sets.memory_variation_short_performance_pct, "+
			"namespace_recommendation_sets.memory_variation_medium_cost_pct, "+
			"namespace_recommendation_sets.memory_variation_medium_performance_pct, "+
			"namespace_recommendation_sets.memory_variation_long_cost_pct, "+
			"namespace_recommendation_sets.memory_variation_long_performance_pct, "+
			"namespace_recommendation_sets.term, "+
			"namespace_recommendation_sets.engine, "+
			"namespace_recommendation_sets.rec_cpu_request_millicores, "+
			"namespace_recommendation_sets.rec_cpu_limit_millicores, "+
			"namespace_recommendation_sets.rec_memory_request_kib, "+
			"namespace_recommendation_sets.rec_memory_limit_kib, "+
			"namespace_recommendation_sets.current_cpu_request_millicores, "+
			"namespace_recommendation_sets.current_cpu_limit_millicores, "+
			"namespace_recommendation_sets.current_memory_request_kib, "+
			"namespace_recommendation_sets.current_memory_limit_kib, "+
			"namespace_recommendation_sets.monitoring_start_time, "+
			"namespace_recommendation_sets.monitoring_end_time, "+
			"namespace_recommendation_sets.notification_codes::text AS notification_codes, "+
			"namespace_recommendation_sets.confidence_level").
		// #599 Phase 5: native namespace rows carry no workload_id, so the
		// legacy workloads JOIN surfaced none of them (Kruize-mode compat
		// served 0 rows). Join clusters directly on cluster_uuid instead;
		// cluster columns (source_id/alias/last_reported, RBAC cluster and
		// tag filters) resolve identically. LEFT keeps blob-only legacy rows
		// servable when no clusters row matches.
		Joins(`
			LEFT JOIN clusters ON clusters.cluster_uuid = namespace_recommendation_sets.cluster_uuid
				AND clusters.org_id = namespace_recommendation_sets.org_id
		`).Model(&NamespaceRecommendationSetResult{}).
		Where("namespace_recommendation_sets.org_id = ?", orgID)
	return query
}
