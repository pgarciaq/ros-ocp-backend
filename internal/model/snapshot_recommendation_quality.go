package model

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
)

// SnapshotQualityRow maps to a row from snapshot_recommendation_quality joined with clusters.
type SnapshotQualityRow struct {
	MeasuredAt           time.Time `gorm:"column:measured_at" json:"measured_at"`
	ClusterUUID          string    `gorm:"column:cluster_uuid" json:"cluster_uuid"`
	ClusterAlias         string    `gorm:"column:cluster_alias" json:"cluster_alias"`
	SnapshotName         string    `gorm:"column:snapshot_name" json:"snapshot_name"`
	AdoptionDetected     bool      `gorm:"column:adoption_detected" json:"adoption_detected"`
	RecommendationAgeHrs *int64    `gorm:"column:recommendation_age_hours" json:"recommendation_age_hours"`
}

// GetSnapshotRecommendationQuality queries snapshot_recommendation_quality with filtering,
// RBAC, and pagination. Returns rows, total count, and error.
func GetSnapshotRecommendationQuality(
	orgID string,
	opts listoptions.ListOptions,
	queryParams map[string]interface{},
	userPerms map[string][]string,
) ([]SnapshotQualityRow, int, error) {
	db := database.GetDB()

	baseQuery := db.Table("snapshot_recommendation_quality q").
		Select(`q.measured_at, q.cluster_uuid, c.cluster_alias,
			q.snapshot_name,
			q.adoption_detected, q.recommendation_age_hours`).
		Joins(`JOIN clusters c ON c.cluster_uuid = q.cluster_uuid AND c.org_id = ?`, orgID).
		Where("q.org_id = ?", orgID)

	baseQuery = ApplyNativeRBAC(baseQuery, userPerms, "")
	baseQuery = ApplyQueryParams(baseQuery, queryParams)

	var totalCount int64
	countQuery := db.Table("snapshot_recommendation_quality q").
		Select("COUNT(*)").
		Joins(`JOIN clusters c ON c.cluster_uuid = q.cluster_uuid AND c.org_id = ?`, orgID).
		Where("q.org_id = ?", orgID)
	countQuery = ApplyNativeRBAC(countQuery, userPerms, "")
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

	rows, err := scanSnapshotQualityRows(sqlRows, opts.Limit)
	if err != nil {
		return nil, 0, err
	}

	return rows, int(totalCount), nil
}
