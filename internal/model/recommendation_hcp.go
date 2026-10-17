package model

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	kruizeplugin "github.com/redhatinsights/ros-ocp-backend/internal/plugins/kruize"
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

// HCP surface scope selectors (#694). Default ("") = HCP evidence scope;
// "management" = management-overhead scope. Handler-side validation is the
// gate (400 on unknown values); the model treats any other value as default
// so an unvalidated value can never silently widen scope.
const (
	HCPScopeDefault    = ""
	HCPScopeManagement = "management"
)

// managementScopeFragment scopes to management-cluster namespaces that are
// not hosted control planes (#694): the clusters row's W0 topology class is
// 'management' AND the namespace is absent from its hcp_namespaces list.
// COALESCE keeps NULL hcp_namespaces reading as empty (precedent: engine
// hcp_namespaces_test NULL rule). EXISTS keeps the #600 no-JOIN rule.
func managementScopeFragment() string {
	return `(EXISTS (SELECT 1 FROM clusters c
		WHERE c.org_id = recommendation_sets.org_id
		  AND c.cluster_uuid = recommendation_sets.cluster_uuid
		  AND c.cluster_topology = 'management'
		  AND recommendation_sets.namespace != ALL(COALESCE(c.hcp_namespaces, '{}'))))`
}

// hcpScopeFilter returns the scope fragment plus its bind args for the
// requested scope selector; any non-management value falls back to the
// default HCP evidence scope.
func hcpScopeFilter(scope string) (string, []interface{}) {
	if scope == HCPScopeManagement {
		return managementScopeFragment(), nil
	}
	fragment, windowStart := hcpScopeFragment()
	return fragment, []interface{}{windowStart}
}

// HCPRowScoped reports whether one org/cluster/namespace triple is HCP
// evidenced (row list or fresh snapshot). Used by the detail gate: off-scope
// IDs 404 instead of leaking app workloads through the HCP surface.
// scope selects the surface (HCPScopeDefault / HCPScopeManagement).
func HCPRowScoped(orgID, clusterUUID, namespace, scope string) (bool, error) {
	fragment, args := hcpScopeFilter(scope)
	var n int64
	err := database.GetDB().Table("recommendation_sets").
		Where("recommendation_sets.org_id = ? AND recommendation_sets.cluster_uuid = ? AND recommendation_sets.namespace = ?",
			orgID, clusterUUID, namespace).
		Where(fragment, args...).
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
	HostedClusterID  string             `json:"hosted_cluster_id"`
	Count            int64              `json:"count"`
	SavingsCents     int64              `gorm:"column:savings_cents" json:"-"`
	EstimatedSavings *money.MoneyAmount `gorm:"-" json:"estimated_savings,omitempty"`
}

// HCPNamespaceGroupedRow is one HCP namespace in group_by responses.
// Unlike the per-HC rollup, unassociated rows appear here under their own
// namespace — namespace is never empty, so nothing merges or drops.
type HCPNamespaceGroupedRow struct {
	Namespace        string             `json:"namespace"`
	Count            int64              `json:"count"`
	SavingsCents     int64              `gorm:"column:savings_cents" json:"-"`
	EstimatedSavings *money.MoneyAmount `gorm:"-" json:"estimated_savings,omitempty"`
}

// applyHCPBase builds the scoped, RBAC-filtered container query shared by
// the HCP list and grouped views. RBAC applies before any hosted filtering:
// the hosted tag never grants management access. scope swaps the surface
// fragment (HCPScopeDefault / HCPScopeManagement).
func applyHCPBase(orgID, scope string, queryParams map[string]interface{}, userPerms map[string][]string) (*gorm.DB, error) {
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
	fragment, args := hcpScopeFilter(scope)
	query = query.Where(fragment, args...)
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
func GetHCPRecommendationSets(orgID, scope string, opts listoptions.ListOptions, queryParams map[string]interface{}, userPerms map[string][]string) ([]RecommendationSetResult, int, error) {
	var out []RecommendationSetResult
	query, err := applyHCPBase(orgID, scope, queryParams, userPerms)
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
	if len(keys) == 0 {
		return keys, int(pinnedCount), nil
	}
	// #651: sibling fan-out mirrors the classic container list — every key
	// carries all six term/engine variants so term/engine projection is
	// display-side (DB-level filters are tolerated, not applied).
	ids := make([]string, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.ID)
	}
	siblings, err := (&RecommendationSet{}).GetRecommendationSiblingRowsByContainers(orgID, ids, userPerms)
	if err != nil {
		return keys, 0, err
	}
	byContainer := make(map[string][]kruizeplugin.SynthDBRow, len(keys))
	for i := range siblings {
		s := &siblings[i]
		byContainer[s.ID] = append(byContainer[s.ID], s.SynthDBRow)
	}
	for i := range keys {
		blob := kruizeplugin.SynthesizeKruizeJSON(kruizeplugin.SynthInputsFromRows(byContainer[keys[i].ID]))
		if len(blob) == 0 {
			continue
		}
		raw, merr := json.Marshal(blob)
		if merr != nil {
			continue
		}
		keys[i].Recommendations = datatypes.JSON(raw)
		// #616: absolute deltas — clear stored pcts so UpdateRecommendationJSON
		// recomputes instead of injecting stale values.
		keys[i].StoredVariationPcts = StoredVariationPcts{}
	}
	return keys, int(pinnedCount), nil
}

// GetHCPGroupedRecommendations aggregates associated hosted clusters:
// per-HC container counts plus summed savings cents over pinned rows.
// Unassociated rows never appear here.
func GetHCPGroupedRecommendations(orgID, scope string, opts listoptions.ListOptions, queryParams map[string]interface{}, userPerms map[string][]string) ([]HCPGroupedRow, int, error) {
	var out []HCPGroupedRow
	query, err := applyHCPBase(orgID, scope, queryParams, userPerms)
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

// GetHCPNamespaceGroupedRecommendations aggregates per-namespace container
// counts plus summed savings cents over pinned rows. Every in-scope row has
// a namespace, so associated and unassociated rows alike appear exactly once.
func GetHCPNamespaceGroupedRecommendations(orgID, scope string, opts listoptions.ListOptions, queryParams map[string]interface{}, userPerms map[string][]string) ([]HCPNamespaceGroupedRow, int, error) {
	var out []HCPNamespaceGroupedRow
	query, err := applyHCPBase(orgID, scope, queryParams, userPerms)
	if err != nil {
		return out, 0, err
	}
	pinned := hcpPinned(query.Session(&gorm.Session{}))
	var count int64
	if err := pinned.Session(&gorm.Session{}).Distinct("recommendation_sets.namespace").Count(&count).Error; err != nil {
		return out, 0, err
	}
	if err := pinned.Session(&gorm.Session{}).
		Select("recommendation_sets.namespace, COUNT(DISTINCT recommendation_sets.container_id) AS count, COALESCE(SUM(recommendation_sets.estimated_savings_cents), 0) AS savings_cents").
		Group("recommendation_sets.namespace").
		Order("recommendation_sets.namespace ASC").
		Offset(opts.Offset).
		Limit(opts.Limit).
		Scan(&out).Error; err != nil {
		return out, 0, err
	}
	return out, int(count), nil
}
