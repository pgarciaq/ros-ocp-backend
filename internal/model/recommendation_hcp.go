package model

import (
	"time"

	"gorm.io/gorm"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/rbac"
)

// HCP scope for the dedicated surface (#638). Rows qualify when their
// namespace appears in EITHER the clusters-row HCP list or a complete
// snapshot observation: routing protects on either source knowing a
// namespace (mirrors engine.mergeHCPNamespaces). EXISTS subqueries keep the
// #600 no-JOIN rule (no row multiplication). Both cluster_uuid columns are
// UUID-typed (migration 000041), so cross-table equality is type-safe.
func hcpScopeFragment() (string, time.Time) {
	maxLookbackDays := config.GetConfig().MaxLookbackDays
	if maxLookbackDays <= 0 {
		maxLookbackDays = 14
	}
	windowStart := time.Now().UTC().AddDate(0, 0, -maxLookbackDays)
	fragment := `(EXISTS (SELECT 1 FROM clusters c
		WHERE c.org_id = recommendation_sets.org_id
		  AND c.cluster_uuid = recommendation_sets.cluster_uuid
		  AND recommendation_sets.namespace = ANY(c.hcp_namespaces))
		OR EXISTS (SELECT 1 FROM manifest_hcp_snapshots s
		WHERE s.org_id = recommendation_sets.org_id
		  AND s.cluster_uuid = recommendation_sets.cluster_uuid
		  AND s.hcp_namespace = recommendation_sets.namespace
		  AND s.complete = TRUE AND s.observed_at >= ?))`
	return fragment, windowStart
}

// HCPRowScoped reports whether one org/cluster/namespace triple is HCP
// evidenced (row list or fresh snapshot). Used by the detail gate: off-scope
// IDs 404 instead of leaking app workloads through the HCP surface.
func HCPRowScoped(orgID, clusterUUID, namespace string) (bool, error) {
	fragment, windowStart := hcpScopeFragment()
	var n int64
	err := database.GetDB().Table("recommendation_sets").
		Where("recommendation_sets.org_id = ? AND recommendation_sets.cluster_uuid = ? AND recommendation_sets.namespace = ?",
			orgID, clusterUUID, namespace).
		Where(fragment, windowStart).
		Limit(1).
		Count(&n).Error
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// HCPGroupedRow is one associated hosted cluster in group_by responses.
// Savings are integer cents summed over pinned rows; the handler renders
// them into EstimatedSavings in display currency (fleet-summary pattern).
type HCPGroupedRow struct {
	HostedClusterID        string             `json:"hosted_cluster_id"`
	Count                  int64              `json:"count"`
	SavingsCents           int64              `gorm:"column:savings_cents" json:"-"`
	EstimatedSavings       *money.MoneyAmount `gorm:"-" json:"estimated_savings,omitempty"`
}

// applyHCPBase builds the scoped, RBAC-filtered container query shared by
// the HCP list and grouped views. RBAC applies before any hosted filtering:
// the hosted tag never grants management access.
func applyHCPBase(orgID string, queryParams map[string]interface{}, userPerms map[string][]string) (*gorm.DB, error) {
	query := getRecommendationQuery(orgID)
	if err := rbac.AddRBACFilter(query, userPerms, rbac.ResourceContainer); err != nil {
		return nil, err
	}
	for key, values := range queryParams {
		switch v := values.(type) {
		case []string:
			args := make([]interface{}, len(v))
			for i, s := range v {
				args[i] = s
			}
			query = query.Where(key, args...)
		default:
			query = query.Where(key, v)
		}
	}
	fragment, windowStart := hcpScopeFragment()
	query = query.Where(fragment, windowStart)
	return query, nil
}

// hcpPinned scopes a query to short/cost representative rows (the same
// collapse the list path uses) so grouped counts name entities, not rows.
func hcpPinned(query *gorm.DB) *gorm.DB {
	return query.Where(
		"recommendation_sets.term IN ? AND recommendation_sets.engine = ?",
		[]string{"short", "short_term"}, "cost",
	)
}

// GetHCPRecommendationSets serves the dedicated surface list: HCP-scoped
// container rows (associated + incomplete), same collapse/pagination as the
// container list. Incomplete derives read-side (empty ID on scoped rows).
func GetHCPRecommendationSets(orgID string, opts listoptions.ListOptions, queryParams map[string]interface{}, userPerms map[string][]string) ([]RecommendationSetResult, int, error) {
	var out []RecommendationSetResult
	query, err := applyHCPBase(orgID, queryParams, userPerms)
	if err != nil {
		return out, 0, err
	}
	var count int64
	if err := query.Session(&gorm.Session{}).Distinct("recommendation_sets.container_id").Count(&count).Error; err != nil {
		return out, 0, err
	}
	pinned := hcpPinned(query.Session(&gorm.Session{}))
	var pinnedCount int64
	if err := pinned.Session(&gorm.Session{}).Distinct("recommendation_sets.container_id").Count(&pinnedCount).Error; err != nil {
		return out, 0, err
	}
	limit := opts.Limit
	if opts.Format == "csv" {
		limit = config.GetConfig().RecordLimitCSV
	}
	var keys []RecommendationSetResult
	if err := pinned.Session(&gorm.Session{}).
		Order(listoptions.SQLOrderByFragment(opts.OrderBy, opts.OrderHow)).
		Order("recommendation_sets.container_id ASC").
		Offset(opts.Offset).
		Limit(limit).
		Scan(&keys).Error; err != nil {
		return out, 0, err
	}
	for i := range keys {
		if keys[i].HostedClusterID == "" {
			keys[i].Incomplete = true
		}
	}
	return keys, int(pinnedCount), nil
}

// GetHCPGroupedRecommendations aggregates associated hosted clusters:
// per-HC container counts plus summed savings cents over pinned rows.
// Unassociated rows never appear here.
func GetHCPGroupedRecommendations(orgID string, opts listoptions.ListOptions, queryParams map[string]interface{}, userPerms map[string][]string) ([]HCPGroupedRow, int, error) {
	var out []HCPGroupedRow
	query, err := applyHCPBase(orgID, queryParams, userPerms)
	if err != nil {
		return out, 0, err
	}
	pinned := hcpPinned(query.Session(&gorm.Session{})).
		Where("recommendation_sets.hosted_cluster_id IS NOT NULL AND recommendation_sets.hosted_cluster_id != ''")
	var count int64
	if err := pinned.Session(&gorm.Session{}).Distinct("recommendation_sets.hosted_cluster_id").Count(&count).Error; err != nil {
		return out, 0, err
	}
	if err := pinned.Session(&gorm.Session{}).
		Select("recommendation_sets.hosted_cluster_id, COUNT(DISTINCT recommendation_sets.container_id) AS count, COALESCE(SUM(recommendation_sets.estimated_savings_cents), 0) AS savings_cents").
		Group("recommendation_sets.hosted_cluster_id").
		Order("recommendation_sets.hosted_cluster_id ASC").
		Offset(opts.Offset).
		Limit(opts.Limit).
		Scan(&out).Error; err != nil {
		return out, 0, err
	}
	return out, int(count), nil
}
