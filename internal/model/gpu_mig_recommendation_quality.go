package model

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
)

// GPUMIGQualityRow maps to a row from gpu_mig_recommendation_quality joined with clusters.
type GPUMIGQualityRow struct {
	MeasuredAt           time.Time `gorm:"column:measured_at" json:"measured_at"`
	ClusterUUID          string    `gorm:"column:cluster_uuid" json:"cluster_uuid"`
	ClusterAlias         string    `gorm:"column:cluster_alias" json:"cluster_alias"`
	Namespace            string    `gorm:"column:namespace" json:"namespace"`
	Workload             string    `gorm:"column:workload" json:"workload"`
	ContainerName        string    `gorm:"column:container_name" json:"container_name"`
	Engine               string    `gorm:"column:engine" json:"engine"`
	StabilityPct         *float32  `gorm:"column:stability_pct" json:"stability_pct"`
	AdoptionDetected     bool      `gorm:"column:adoption_detected" json:"adoption_detected"`
	ContentionDays       *int64    `gorm:"column:contention_days" json:"contention_days"`
	RecommendationAgeHrs *int64    `gorm:"column:recommendation_age_hours" json:"recommendation_age_hours"`
}

// GetGPUMIGRecommendationQuality queries gpu_mig_recommendation_quality with filtering,
// RBAC, and pagination. Returns rows, total count, and error.
func GetGPUMIGRecommendationQuality(
	orgID string,
	opts listoptions.ListOptions,
	queryParams map[string]interface{},
	userPerms map[string][]string,
) ([]GPUMIGQualityRow, int, error) {
	db := database.GetDB()

	baseQuery := db.Table("gpu_mig_recommendation_quality q").
		Select(`q.measured_at, q.cluster_uuid, c.cluster_alias,
			q.namespace, q.workload, q.container_name, q.engine,
			q.stability_pct, q.adoption_detected,
			q.contention_days, q.recommendation_age_hours`).
		Joins(`JOIN clusters c ON c.cluster_uuid = q.cluster_uuid`).
		Where("q.org_id = ?", orgID)

	baseQuery = ApplyNativeRBAC(baseQuery, userPerms, "q.namespace")
	baseQuery = ApplyQueryParams(baseQuery, queryParams)

	var totalCount int64
	countQuery := db.Table("gpu_mig_recommendation_quality q").
		Select("COUNT(*)").
		Joins(`JOIN clusters c ON c.cluster_uuid = q.cluster_uuid`).
		Where("q.org_id = ?", orgID)
	countQuery = ApplyNativeRBAC(countQuery, userPerms, "q.namespace")
	countQuery = ApplyQueryParams(countQuery, queryParams)

	if err := countQuery.Scan(&totalCount).Error; err != nil {
		return nil, 0, err
	}

	orderClause := listoptions.SQLOrderByFragment(opts.OrderBy, opts.OrderHow)

	var rows []GPUMIGQualityRow
	err := baseQuery.
		Order(orderClause).
		Offset(opts.Offset).
		Limit(opts.Limit).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	return rows, int(totalCount), nil
}
