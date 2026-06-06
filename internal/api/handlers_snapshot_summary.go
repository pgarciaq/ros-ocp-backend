package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
)

const (
	defaultSnapshotSummaryLimit = 10
	snapshotSummaryGiBDivisor   = 1024 * 1024 * 1024

	snapshotSummaryGroupProject = "project"
	snapshotSummaryGroupCluster = "cluster"
)

// AgeDaysRange is the min/max snapshot age for a summary group.
type AgeDaysRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// SnapshotSummaryResponse aggregates snapshot recommendations by namespace or cluster.
type SnapshotSummaryResponse struct {
	Namespace                        string         `json:"namespace"`
	ClusterUUID                      string         `json:"cluster_uuid"`
	SnapshotCount                    int            `json:"snapshot_count"`
	ActionableSnapshotCount          int            `json:"actionable_snapshot_count"`
	CountsByType                     map[string]int `json:"counts_by_type"`
	TotalRestoreSizeBytes            int64          `json:"total_restore_size_bytes"`
	TotalRestoreSizeGiB              float64        `json:"total_restore_size_gib"`
	ReclaimableRestoreSizeBytes      int64          `json:"reclaimable_restore_size_bytes"`
	ReclaimableRestoreSizeGiB        float64        `json:"reclaimable_restore_size_gib"`
	TotalMonthlyHoldingCostUSD       float64        `json:"total_monthly_holding_cost_usd"`
	ReclaimableMonthlyHoldingCostUSD float64        `json:"reclaimable_monthly_holding_cost_usd"`
	AgeDays                          AgeDaysRange   `json:"age_days"`
}

// SnapshotSummaryListResponse wraps paginated namespace/cluster snapshot summaries.
type SnapshotSummaryListResponse struct {
	Meta struct {
		Count    int    `json:"count"`
		Limit    int    `json:"limit"`
		Offset   int    `json:"offset"`
		Currency string `json:"currency"`
	} `json:"meta"`
	Links Links                    `json:"links"`
	Data  []SnapshotSummaryResponse `json:"data"`
}

var snapshotSummaryOrderByAllowed = map[string]string{
	"reclaimable_monthly_holding_cost_usd": "reclaimable_monthly_holding_cost_usd",
	"reclaimable_restore_size_gib":         "reclaimable_restore_size_bytes",
	"actionable_snapshot_count":            "actionable_snapshot_count",
	"snapshot_count":                       "snapshot_count",
}

// GetSnapshotSummary handles GET /recommendations/openshift/snapshots/summary.
func GetSnapshotSummary(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	limit := defaultSnapshotSummaryLimit
	if v := strings.TrimSpace(c.QueryParam("limit")); v != "" {
		n, parseErr := strconv.Atoi(v)
		if parseErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "invalid limit"})
		}
		if n < 0 {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "limit cannot be negative"})
		}
		if n == 0 {
			limit = defaultSnapshotSummaryLimit
		} else if n > 100 {
			limit = 100
		} else {
			limit = n
		}
	}

	offset := 0
	if v := strings.TrimSpace(c.QueryParam("offset")); v != "" {
		o, parseErr := strconv.Atoi(v)
		if parseErr != nil || o < 0 {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "invalid offset"})
		}
		offset = o
	}

	groupBy, groupErr := resolveSnapshotSummaryGroupBy(c)
	if groupErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": groupErr.Error()})
	}

	orderCol, orderDir, orderErr := queryparams.ParseOrderBy(
		c, snapshotSummaryOrderByAllowed,
		"reclaimable_monthly_holding_cost_usd", "desc",
	)
	if orderErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": orderErr.Error()})
	}

	clusterFilter := strings.TrimSpace(queryparams.FirstFilter(c, "cluster"))
	namespaceFilter := strings.TrimSpace(queryparams.FirstFilter(c, "project"))
	typeFilter := queryparams.FirstFilter(c, "recommendation_type")
	userPerms := get_user_permissions(c)
	ctx := c.Request().Context()

	filterSQL := ""
	args := []interface{}{orgID}
	argIdx := 2

	rbacSQL, rbacArgs, rbacIdx, rbacDeny := snapshotRBACClusterFilter(userPerms, argIdx)
	if rbacDeny {
		return emptySnapshotSummaryResponse(c, orgID, clusterFilter, limit, offset)
	}
	filterSQL += rbacSQL
	args = append(args, rbacArgs...)
	argIdx = rbacIdx

	if clusterFilter != "" {
		filterSQL += ` AND cluster_uuid = $` + strconv.Itoa(argIdx)
		args = append(args, clusterFilter)
		argIdx++
	}

	nsClause, nsArg, nextIdx, nsErr := snapshotNamespaceFilterClause(namespaceFilter, argIdx)
	if nsErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": nsErr.Error()})
	}
	if nsClause != "" {
		filterSQL += nsClause
		if nsArg != nil {
			args = append(args, nsArg)
		}
		argIdx = nextIdx
	}
	if typeFilter != "" {
		filterSQL += ` AND recommendation_type = $` + strconv.Itoa(argIdx)
		args = append(args, typeFilter)
		argIdx++
	}

	groupBySQL, selectNamespace := snapshotSummaryGroupBySQL(groupBy)

	groupedSQL := `
		SELECT` + selectNamespace + `
			cluster_uuid::text,
			COUNT(*)::int AS snapshot_count,
			COUNT(*) FILTER (WHERE recommendation_type IN ('orphaned','stale','never_restored','redundant'))::int AS actionable_snapshot_count,
			COUNT(*) FILTER (WHERE recommendation_type = 'orphaned')::int AS count_orphaned,
			COUNT(*) FILTER (WHERE recommendation_type = 'stale')::int AS count_stale,
			COUNT(*) FILTER (WHERE recommendation_type = 'never_restored')::int AS count_never_restored,
			COUNT(*) FILTER (WHERE recommendation_type = 'redundant')::int AS count_redundant,
			COUNT(*) FILTER (WHERE recommendation_type = 'managed')::int AS count_managed,
			COUNT(*) FILTER (WHERE recommendation_type = 'active')::int AS count_active,
			COALESCE(SUM(restore_size_bytes), 0)::bigint AS total_restore_size_bytes,
			COALESCE(SUM(restore_size_bytes) FILTER (WHERE recommendation_type IN ('orphaned','stale','never_restored','redundant')), 0)::bigint AS reclaimable_restore_size_bytes,
			COALESCE(SUM(estimated_cost_cents), 0)::float / 100.0 AS total_monthly_holding_cost_usd,
			COALESCE(SUM(estimated_cost_cents) FILTER (WHERE recommendation_type IN ('orphaned','stale','never_restored','redundant')), 0)::float / 100.0 AS reclaimable_monthly_holding_cost_usd,
			COALESCE(MIN(age_days), 0)::int AS min_age_days,
			COALESCE(MAX(age_days), 0)::int AS max_age_days
		FROM snapshot_recommendation_sets
		WHERE org_id = $1` + filterSQL + `
		` + groupBySQL

	countSQL := `SELECT COUNT(*)::int FROM (` + groupedSQL + `) summary_groups`
	var total int
	if err := pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		hlog.Errorf("snapshot summary count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to count snapshot summaries",
		})
	}

	secondaryOrder := "namespace ASC"
	if groupBy == snapshotSummaryGroupCluster {
		secondaryOrder = "cluster_uuid ASC"
	}

	pageSQL := groupedSQL + `
		ORDER BY ` + orderCol + ` ` + strings.ToUpper(orderDir) + `, ` + secondaryOrder + `
		LIMIT $` + strconv.Itoa(argIdx) + ` OFFSET $` + strconv.Itoa(argIdx+1)
	pageArgs := append(append([]interface{}{}, args...), limit, offset)

	rows, err := pool.Query(ctx, pageSQL, pageArgs...)
	if err != nil {
		hlog.Errorf("snapshot summary query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch snapshot summaries",
		})
	}
	defer rows.Close()

	data := make([]SnapshotSummaryResponse, 0)
	for rows.Next() {
		var row SnapshotSummaryResponse
		var countOrphaned, countStale, countNeverRestored, countRedundant, countManaged, countActive int
		var minAge, maxAge int
		if scanErr := rows.Scan(
			&row.Namespace,
			&row.ClusterUUID,
			&row.SnapshotCount,
			&row.ActionableSnapshotCount,
			&countOrphaned,
			&countStale,
			&countNeverRestored,
			&countRedundant,
			&countManaged,
			&countActive,
			&row.TotalRestoreSizeBytes,
			&row.ReclaimableRestoreSizeBytes,
			&row.TotalMonthlyHoldingCostUSD,
			&row.ReclaimableMonthlyHoldingCostUSD,
			&minAge,
			&maxAge,
		); scanErr != nil {
			hlog.Errorf("scanning snapshot summary row: %v", scanErr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read snapshot summary rows",
			})
		}
		row.CountsByType = map[string]int{
			"orphaned":        countOrphaned,
			"stale":           countStale,
			"never_restored":  countNeverRestored,
			"redundant":       countRedundant,
			"managed":         countManaged,
			"active":          countActive,
		}
		row.TotalRestoreSizeGiB = bytesToGiB(row.TotalRestoreSizeBytes)
		row.ReclaimableRestoreSizeGiB = bytesToGiB(row.ReclaimableRestoreSizeBytes)
		row.AgeDays = AgeDaysRange{Min: minAge, Max: maxAge}
		data = append(data, row)
	}
	if err := rows.Err(); err != nil {
		hlog.Errorf("snapshot summary row iteration failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch snapshot summaries",
		})
	}
	if data == nil {
		data = []SnapshotSummaryResponse{}
	}

	resp := SnapshotSummaryListResponse{}
	resp.Meta.Count = total
	resp.Meta.Limit = limit
	resp.Meta.Offset = offset
	resp.Meta.Currency = fetchClusterCurrency(ctx, orgID, clusterFilter)
	resp.Links = buildLinks(c.Request(), total, limit, offset)
	resp.Data = data

	return c.JSON(http.StatusOK, resp)
}

func emptySnapshotSummaryResponse(c echo.Context, orgID, clusterFilter string, limit, offset int) error {
	resp := SnapshotSummaryListResponse{}
	resp.Meta.Count = 0
	resp.Meta.Limit = limit
	resp.Meta.Offset = offset
	resp.Meta.Currency = fetchClusterCurrency(c.Request().Context(), orgID, clusterFilter)
	resp.Links = buildLinks(c.Request(), 0, limit, offset)
	resp.Data = []SnapshotSummaryResponse{}
	return c.JSON(http.StatusOK, resp)
}

func resolveSnapshotSummaryGroupBy(c echo.Context) (string, error) {
	if queryparams.GroupByField(c, "cluster") {
		return snapshotSummaryGroupCluster, nil
	}
	// project is canonical; namespace is a backward-compatible alias (Koku uses "project" for OpenShift namespaces).
	if queryparams.GroupByField(c, "project") || queryparams.GroupByField(c, "namespace") {
		return snapshotSummaryGroupProject, nil
	}
	for _, raw := range c.QueryParams()["group_by"] {
		for _, part := range queryparams.SplitCommaValues([]string{raw}) {
			switch strings.TrimSpace(part) {
			case "cluster":
				return snapshotSummaryGroupCluster, nil
			case "project", "namespace":
				return snapshotSummaryGroupProject, nil
			case "":
			default:
				return "", fmt.Errorf("invalid group_by; must be project, namespace, or cluster")
			}
		}
	}
	for key := range c.QueryParams() {
		if strings.HasPrefix(key, queryparams.GroupByPrefix) && strings.HasSuffix(key, "]") {
			field := strings.TrimSpace(key[len(queryparams.GroupByPrefix) : len(key)-1])
			if field != "project" && field != "namespace" && field != "cluster" {
				return "", fmt.Errorf("invalid group_by; must be project, namespace, or cluster")
			}
		}
	}
	return snapshotSummaryGroupProject, nil
}

func snapshotSummaryGroupBySQL(groupBy string) (groupByClause, selectNamespace string) {
	if groupBy == snapshotSummaryGroupCluster {
		return `GROUP BY cluster_uuid`, `
			'' AS namespace,`
	}
	return `GROUP BY namespace, cluster_uuid`, `
			namespace,`
}

// snapshotNamespaceFilterClause builds an exact or ILIKE filter for namespace (project).
// Wildcard: * in the value is translated to SQL % (Koku-style substring match).
func snapshotNamespaceFilterClause(filter string, argIdx int) (clause string, arg interface{}, nextIdx int, err error) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return "", nil, argIdx, nil
	}
	if strings.Contains(filter, "*") {
		pattern := strings.ReplaceAll(filter, "*", "%")
		return " AND namespace ILIKE $" + strconv.Itoa(argIdx), pattern, argIdx + 1, nil
	}
	return " AND namespace = $" + strconv.Itoa(argIdx), filter, argIdx + 1, nil
}

func bytesToGiB(bytes int64) float64 {
	if bytes == 0 {
		return 0
	}
	return float64(bytes) / snapshotSummaryGiBDivisor
}
