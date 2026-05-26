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
type StoredVariationSpec = kruizeplugin.StoredVariationSpec
type RecommendationColumnValues = kruizeplugin.RecommendationColumnValues

var StoredVariationSpecs = kruizeplugin.StoredVariationSpecs

var ExtractRecommendationColumnValues = kruizeplugin.ExtractRecommendationColumnValues

func getRecommendationQuery(orgID string) *gorm.DB {
	db := database.GetDB()
	query := db.Table("recommendation_sets").
		Select(
			"recommendation_sets.container_id AS id, "+
				"recommendation_sets.container_name AS container, "+
				"COALESCE(workloads.namespace, recommendation_sets.namespace) AS project, "+
				"COALESCE(workloads.workload_name, recommendation_sets.workload) AS workload, "+
				"COALESCE(workloads.workload_type::text, recommendation_sets.workload_type) AS workload_type, "+
				"COALESCE(clusters.source_id, '') AS source_id, "+
				"COALESCE(clusters.cluster_uuid, recommendation_sets.cluster_uuid) AS cluster_uuid, "+
				"COALESCE(clusters.cluster_alias, recommendation_sets.cluster_uuid) AS cluster_alias, "+
				"COALESCE(clusters.last_reported_at, recommendation_sets.updated_at) AS last_reported, "+
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
		Joins(`
			LEFT JOIN workloads ON recommendation_sets.workload_id = workloads.id
			LEFT JOIN clusters ON workloads.cluster_id = clusters.id
			LEFT JOIN rh_accounts ON clusters.tenant_id = rh_accounts.id
		`).Model(&RecommendationSetResult{}).
		Where("COALESCE(rh_accounts.org_id, recommendation_sets.org_id) = ?", orgID)
	return query
}

func getNamespaceRecommendationQuery(orgID string) *gorm.DB {
	db := database.GetDB()
	query := db.Table("namespace_recommendation_sets").
		Select("namespace_recommendation_sets.id, "+
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
			"namespace_recommendation_sets.memory_variation_long_performance_pct").
		Joins(`
			JOIN workloads ON namespace_recommendation_sets.workload_id = workloads.id
			JOIN clusters ON workloads.cluster_id = clusters.id
		`).Model(&NamespaceRecommendationSetResult{}).
		Where("namespace_recommendation_sets.org_id = ?", orgID)
	return query
}
