package model

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
)

// PVCQualityRow maps to a row from pvc_recommendation_quality joined with clusters.
type PVCQualityRow struct {
	MeasuredAt           time.Time `gorm:"column:measured_at" json:"measured_at"`
	ClusterUUID          string    `gorm:"column:cluster_uuid" json:"cluster_uuid"`
	ClusterAlias         string    `gorm:"column:cluster_alias" json:"cluster_alias"`
	Namespace            string    `gorm:"column:namespace" json:"namespace"`
	PVCName              string    `gorm:"column:pvc_name" json:"pvc_name"`
	Engine               string    `gorm:"column:engine" json:"engine"`
	StabilityPct         *float32  `gorm:"column:stability_pct" json:"stability_pct"`
	AdoptionDetected     bool      `gorm:"column:adoption_detected" json:"adoption_detected"`
	DaysAboveThreshold   *int64    `gorm:"column:days_above_threshold" json:"days_above_threshold"`
	RecommendationAgeHrs *int64    `gorm:"column:recommendation_age_hours" json:"recommendation_age_hours"`
}

// GetPVCRecommendationQuality queries pvc_recommendation_quality with filtering,
// RBAC, and pagination. Returns rows, total count, and error.
func GetPVCRecommendationQuality(
	orgID string,
	opts listoptions.ListOptions,
	queryParams map[string]interface{},
	userPerms map[string][]string,
) ([]PVCQualityRow, int, error) {
	db := database.GetDB()

	baseQuery := db.Table("pvc_recommendation_quality q").
		Select(`q.measured_at, q.cluster_uuid, c.cluster_alias,
			q.namespace, q.pvc_name, q.engine,
			q.stability_pct, q.adoption_detected,
			q.days_above_threshold, q.recommendation_age_hours`).
		Joins(`JOIN clusters c ON c.cluster_uuid = q.cluster_uuid AND c.org_id = ?`, orgID).
		Where("q.org_id = ?", orgID)

	baseQuery = ApplyNativeRBAC(baseQuery, userPerms, "q.namespace")
	baseQuery = ApplyQueryParams(baseQuery, queryParams)

	var totalCount int64
	countQuery := db.Table("pvc_recommendation_quality q").
		Select("COUNT(*)").
		Joins(`JOIN clusters c ON c.cluster_uuid = q.cluster_uuid AND c.org_id = ?`, orgID).
		Where("q.org_id = ?", orgID)
	countQuery = ApplyNativeRBAC(countQuery, userPerms, "q.namespace")
	countQuery = ApplyQueryParams(countQuery, queryParams)

	if err := countQuery.Scan(&totalCount).Error; err != nil {
		return nil, 0, err
	}

	orderClause := listoptions.SQLOrderByFragment(opts.OrderBy, opts.OrderHow)

	sqlRows, err := baseQuery.
		Order(orderClause).
		Offset(opts.Offset).
		Limit(opts.Limit).
		Rows()
	if err != nil {
		return nil, 0, err
	}
	defer sqlRows.Close()

	rows, err := scanPVCQualityRows(sqlRows, opts.Limit)
	if err != nil {
		return nil, 0, err
	}

	return rows, int(totalCount), nil
}
