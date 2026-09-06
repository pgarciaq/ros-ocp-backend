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
	EstimatedSavingsCents *int64        `gorm:"column:estimated_savings_cents" json:"-"`

	// Explanation columns (expl_*) — nullable, omitted from JSON when nil.
	ExplDataDays            *int     `gorm:"column:expl_data_days" json:"expl_data_days,omitempty"`
	ExplDecayHalfLifeHours  *float64 `gorm:"column:expl_decay_half_life_hours" json:"expl_decay_half_life_hours,omitempty"`
	ExplCPUCostPctMC        *int64   `gorm:"column:expl_cpu_cost_pct_mc" json:"expl_cpu_cost_pct_mc,omitempty"`
	ExplCPUPerfPctMC        *int64   `gorm:"column:expl_cpu_perf_pct_mc" json:"expl_cpu_perf_pct_mc,omitempty"`
	ExplCPUUsageP95MC       *int64   `gorm:"column:expl_cpu_usage_p95_mc" json:"expl_cpu_usage_p95_mc,omitempty"`
	ExplCPUUsageP50MC       *int64   `gorm:"column:expl_cpu_usage_p50_mc" json:"expl_cpu_usage_p50_mc,omitempty"`
	ExplCPUUsageMeanMC      *int64   `gorm:"column:expl_cpu_usage_mean_mc" json:"expl_cpu_usage_mean_mc,omitempty"`
	ExplCPUAdaptiveMarginBP *int32   `gorm:"column:expl_cpu_adaptive_margin_bp" json:"expl_cpu_adaptive_margin_bp,omitempty"`
	ExplCPUTrendSlope       *float64 `gorm:"column:expl_cpu_trend_slope" json:"expl_cpu_trend_slope,omitempty"`
	ExplMemCostPctKiB       *int64   `gorm:"column:expl_mem_cost_pct_kib" json:"expl_mem_cost_pct_kib,omitempty"`
	ExplMemPerfPctKiB       *int64   `gorm:"column:expl_mem_perf_pct_kib" json:"expl_mem_perf_pct_kib,omitempty"`
	ExplMemUsageP95KiB      *int64   `gorm:"column:expl_mem_usage_p95_kib" json:"expl_mem_usage_p95_kib,omitempty"`
	ExplMemUsageP50KiB      *int64   `gorm:"column:expl_mem_usage_p50_kib" json:"expl_mem_usage_p50_kib,omitempty"`
	ExplMemUsageMeanKiB     *int64   `gorm:"column:expl_mem_usage_mean_kib" json:"expl_mem_usage_mean_kib,omitempty"`
	ExplMemAdaptiveMarginBP *int32   `gorm:"column:expl_mem_adaptive_margin_bp" json:"expl_mem_adaptive_margin_bp,omitempty"`
	ExplMemTrendSlope       *float64 `gorm:"column:expl_mem_trend_slope" json:"expl_mem_trend_slope,omitempty"`
	ExplOOMCountSum         *int64   `gorm:"column:expl_oom_count_sum" json:"expl_oom_count_sum,omitempty"`
	ExplOOMBumpApplied      *bool    `gorm:"column:expl_oom_bump_applied" json:"expl_oom_bump_applied,omitempty"`
	ExplCPUFloorApplied     *bool    `gorm:"column:expl_cpu_floor_applied" json:"expl_cpu_floor_applied,omitempty"`
	ExplMemFloorApplied     *bool    `gorm:"column:expl_mem_floor_applied" json:"expl_mem_floor_applied,omitempty"`
	ExplIsIdle              *bool    `gorm:"column:expl_is_idle" json:"expl_is_idle,omitempty"`

	// Currency is set by the API handler before serialization; not a DB column.
	Currency string `gorm:"-" json:"-"`
}

// termToAPI converts DB term values (short, medium, long) to canonical API form
// (short_term, medium_term, long_term) per ADR-0069.
func termToAPI(term string) string {
	switch term {
	case "short":
		return "short_term"
	case "medium":
		return "medium_term"
	case "long":
		return "long_term"
	default:
		return term
	}
}

// MarshalJSON exposes savings as a structured object in API responses while storing cents internally.
// It also converts the term field to canonical API form (short_term, medium_term, long_term).
func (h HistoryRow) MarshalJSON() ([]byte, error) {
	type historyRowAlias HistoryRow
	copy := historyRowAlias(h)
	copy.Term = termToAPI(h.Term)
	aux := struct {
		historyRowAlias
		EstimatedMonthlySavings *money.MoneyAmount `json:"estimated_monthly_savings,omitempty"`
	}{
		historyRowAlias: copy,
	}
	if h.EstimatedSavingsCents != nil {
		cur := h.Currency
		if cur == "" {
			cur = money.DefaultCurrency
		}
		aux.EstimatedMonthlySavings = money.FormatCentsToAmountPtr(h.EstimatedSavingsCents, cur)
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
			h.estimated_savings_cents,
			h.expl_data_days, h.expl_decay_half_life_hours,
			h.expl_cpu_cost_pct_mc, h.expl_cpu_perf_pct_mc,
			h.expl_cpu_usage_p95_mc, h.expl_cpu_usage_p50_mc, h.expl_cpu_usage_mean_mc,
			h.expl_cpu_adaptive_margin_bp, h.expl_cpu_trend_slope,
			h.expl_mem_cost_pct_kib, h.expl_mem_perf_pct_kib,
			h.expl_mem_usage_p95_kib, h.expl_mem_usage_p50_kib, h.expl_mem_usage_mean_kib,
			h.expl_mem_adaptive_margin_bp, h.expl_mem_trend_slope,
			h.expl_oom_count_sum, h.expl_oom_bump_applied, h.expl_cpu_floor_applied, h.expl_mem_floor_applied, h.expl_is_idle`).
		Joins(`JOIN clusters c ON c.cluster_uuid = h.cluster_uuid AND c.org_id = ?`, orgID).
		Where("h.org_id = ?", orgID)

	baseQuery = ApplyNativeRBAC(baseQuery, userPerms, "h.namespace")
	baseQuery = ApplyQueryParams(baseQuery, queryParams)
	if tagFilters := TagFiltersFromParams(queryParams); len(tagFilters) > 0 {
		baseQuery = ApplyTagFiltersToClusterNamespace(baseQuery, orgID, tagFilters, "h.cluster_uuid", "h.namespace")
	}

	var totalCount int64
	countQuery := db.Table("recommendation_history h").
		Select("COUNT(*)").
		Joins(`JOIN clusters c ON c.cluster_uuid = h.cluster_uuid AND c.org_id = ?`, orgID).
		Where("h.org_id = ?", orgID)
	countQuery = ApplyNativeRBAC(countQuery, userPerms, "h.namespace")
	countQuery = ApplyQueryParams(countQuery, queryParams)
	if tagFilters := TagFiltersFromParams(queryParams); len(tagFilters) > 0 {
		countQuery = ApplyTagFiltersToClusterNamespace(countQuery, orgID, tagFilters, "h.cluster_uuid", "h.namespace")
	}

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
