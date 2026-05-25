package model

import (
	"encoding/json"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

// HistoryRow maps to a row from recommendation_history joined with clusters.
type HistoryRow struct {
	RecordedAt            time.Time     `gorm:"column:recorded_at" json:"recorded_at"`
	ClusterUUID           string        `gorm:"column:cluster_uuid" json:"cluster_uuid"`
	ClusterAlias          string        `gorm:"column:cluster_alias" json:"cluster_alias"`
	Namespace             string        `gorm:"column:namespace" json:"namespace"`
	Workload              string        `gorm:"column:workload" json:"workload"`
	ContainerName         string        `gorm:"column:container_name" json:"container_name"`
	Term                  string        `gorm:"column:term" json:"term"`
	Engine                string        `gorm:"column:engine" json:"engine"`
	RecCPURequestMC       *int64        `gorm:"column:rec_cpu_request_millicores" json:"rec_cpu_request_millicores"`
	RecCPULimitMC         *int64        `gorm:"column:rec_cpu_limit_millicores" json:"rec_cpu_limit_millicores"`
	RecMemRequestKiB      *int64        `gorm:"column:rec_memory_request_kib" json:"rec_memory_request_kib"`
	RecMemLimitKiB        *int64        `gorm:"column:rec_memory_limit_kib" json:"rec_memory_limit_kib"`
	NotificationCodes     SmallintArray `gorm:"column:notification_codes;type:smallint[]" json:"notification_codes"`
	ConfidenceLevel       *float32      `gorm:"column:confidence_level" json:"confidence_level"`
	EstimatedSavingsCents *int64        `gorm:"column:estimated_monthly_savings_usd" json:"-"`
}

// MarshalJSON exposes savings as USD float in API responses while storing cents internally.
func (h HistoryRow) MarshalJSON() ([]byte, error) {
	type historyRowAlias HistoryRow
	aux := struct {
		historyRowAlias
		EstimatedMonthlySavingsUSD *float64 `json:"estimated_monthly_savings_usd,omitempty"`
	}{
		historyRowAlias: historyRowAlias(h),
	}
	if h.EstimatedSavingsCents != nil {
		usd := money.CentsToUSD(*h.EstimatedSavingsCents)
		aux.EstimatedMonthlySavingsUSD = &usd
	}
	return json.Marshal(aux)
}

// GetRecommendationHistory queries recommendation_history with filtering,
// RBAC, and pagination. Returns rows, total count, and error.
func GetRecommendationHistory(
	orgID string,
	opts listoptions.ListOptions,
	queryParams map[string]interface{},
	userPerms map[string][]string,
) ([]HistoryRow, int, error) {
	db := database.GetDB()

	baseQuery := db.Table("recommendation_history h").
		Select(`h.recorded_at, h.cluster_uuid, c.cluster_alias,
			h.namespace, h.workload, h.container_name,
			h.term, h.engine,
			h.rec_cpu_request_millicores, h.rec_cpu_limit_millicores,
			h.rec_memory_request_kib, h.rec_memory_limit_kib,
			h.notification_codes, h.confidence_level,
			h.estimated_monthly_savings_usd`).
		Joins(`JOIN clusters c ON c.cluster_uuid = h.cluster_uuid`).
		Where("h.org_id = ?", orgID)

	baseQuery = ApplyNativeRBAC(baseQuery, userPerms, "h.namespace")
	baseQuery = ApplyQueryParams(baseQuery, queryParams)

	var totalCount int64
	countQuery := db.Table("recommendation_history h").
		Select("COUNT(*)").
		Joins(`JOIN clusters c ON c.cluster_uuid = h.cluster_uuid`).
		Where("h.org_id = ?", orgID)
	countQuery = ApplyNativeRBAC(countQuery, userPerms, "h.namespace")
	countQuery = ApplyQueryParams(countQuery, queryParams)

	if err := countQuery.Scan(&totalCount).Error; err != nil {
		return nil, 0, err
	}

	orderClause := listoptions.SQLOrderByFragment(opts.OrderBy, opts.OrderHow)

	var rows []HistoryRow
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
