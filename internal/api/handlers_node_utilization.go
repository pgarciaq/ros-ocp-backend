package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

const defaultNodeUtilLimit = 10

const nodeUtilizationDeprecationMsg = `This path is deprecated. Use GET /api/cost-management/v1/recommendations/openshift/nodes for node CPU/memory utilization recommendations.`

var nodeUtilAllowedOrderBy = map[string]string{
	"node":                        "f.node",
	"estimated_monthly_savings":   "sort_savings",
	"estimated_monthly_savings_usd": "sort_savings", // deprecated alias
}

const (
	nodeUtilDefaultOrderBy  = "estimated_monthly_savings"
	nodeUtilDefaultOrderHow = listoptions.OrderDesc
	nodeUtilPrimaryTerm     = "medium"
	nodeUtilPrimaryEngine   = "cost"
)

type nodeUtilRow struct {
	Node                    string
	ClusterUUID             string
	Term                    string
	Engine                  string
	CPUUtilP50              float32
	CPUUtilP95              float32
	MemUtilP50              float32
	MemUtilP95              float32
	CPUOvercommitRatio      float32
	IsUnderutilized         bool
	IsOvercommitted         bool
	StrandedResource        *string
	PodCount                int64
	TrendSlope              float32
	RecommendedCPUCores     sql.NullFloat64
	RecommendedMemoryGiB    sql.NullFloat64
	NodeCountReduction      int
	EstimatedMonthlySavings sql.NullInt64
	NotificationCodes       []int16
	UpdatedAt               time.Time
}

type nodeUtilKey struct {
	ClusterUUID string
	Node        string
}

// GetNodeUtilizationRecs handles GET /recommendations/openshift/nodes (node CPU/memory utilization).
func GetNodeUtilizationRecs(c echo.Context) error {
	return respondNodeUtilizationRecs(c, false)
}

// GetNodeUtilizationRecsLegacyPath handles GET /recommendations/openshift/nodes/utilization (deprecated alias).
func GetNodeUtilizationRecsLegacyPath(c echo.Context) error {
	return respondNodeUtilizationRecs(c, true)
}

func respondNodeUtilizationRecs(c echo.Context, deprecated bool) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	if deprecated {
		c.Response().Header().Set("Deprecation", "true")
		c.Response().Header().Set("Link", `</api/cost-management/v1/recommendations/openshift/nodes>; rel="alternate"`)
	}

	limit := defaultNodeUtilLimit
	if v := strings.TrimSpace(c.QueryParam("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "invalid limit"})
		}
		if n < 0 {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "limit cannot be negative"})
		}
		if n == 0 {
			limit = defaultNodeUtilLimit
		} else if n > listoptions.MaxLimit {
			limit = listoptions.MaxLimit
		} else {
			limit = n
		}
	}

	offset := 0
	if v := strings.TrimSpace(c.QueryParam("offset")); v != "" {
		o, err := strconv.Atoi(v)
		if err != nil || o < 0 {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "invalid offset"})
		}
		offset = o
	}

	orderByKey, orderHow, err := queryparams.ParseOrderByAPIKey(c, nodeUtilAllowedOrderBy, nodeUtilDefaultOrderBy, nodeUtilDefaultOrderHow)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": err.Error(),
		})
	}
	orderCol := nodeUtilAllowedOrderBy[orderByKey]

	pool := database.GetPool()
	if pool == nil {
		hlog.Warnf("GetNodeUtilizationRecs: database pool unavailable")
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	ctx := c.Request().Context()

	allClusters, err := getClustersForOrg(ctx, orgID)
	if err != nil {
		hlog.Warnf("GetNodeUtilizationRecs: failed to resolve clusters: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	allowedClusters := filterClustersByRBAC(allClusters, userPerms)
	if len(allowedClusters) == 0 {
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, model.NodeUtilizationListResponse{
			Meta: model.NodeUtilizationMeta{Count: 0, Limit: limit, Offset: offset, Currency: costdata.DefaultCurrency},
			Data: []model.NodeUtilizationRec{},
		})
	}

	clusterFilter := queryparams.FirstFilter(c, "cluster")
	nodeFilter := queryparams.FirstFilter(c, "node")
	termFilter := queryparams.FirstFilter(c, "term")
	engineFilter := queryparams.FirstFilter(c, "engine")
	underutilFilter := queryparams.FirstFilter(c, "is_underutilized")
	overcommitFilter := queryparams.FirstFilter(c, "is_overcommitted")

	if engineFilter != "" && engineFilter != "cost" && engineFilter != "performance" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "invalid engine"})
	}

	baseFrom := `
		FROM node_recommendations nr
		WHERE nr.org_id = $1 AND nr.cluster_uuid::text = ANY($2)`

	args := []interface{}{orgID, allowedClusters}
	argIdx := 3

	if clusterFilter != "" {
		baseFrom += " AND nr.cluster_uuid = $" + strconv.Itoa(argIdx)
		args = append(args, clusterFilter)
		argIdx++
	}
	if nodeFilter != "" {
		baseFrom += " AND nr.node = $" + strconv.Itoa(argIdx)
		args = append(args, nodeFilter)
		argIdx++
	}
	if termFilter != "" {
		baseFrom += " AND nr.term = $" + strconv.Itoa(argIdx)
		args = append(args, termFilter)
		argIdx++
	}
	if engineFilter != "" {
		baseFrom += " AND nr.engine = $" + strconv.Itoa(argIdx)
		args = append(args, engineFilter)
		argIdx++
	}
	if underutilFilter == "true" {
		baseFrom += " AND nr.is_underutilized = true"
	} else if underutilFilter == "false" {
		baseFrom += " AND nr.is_underutilized = false"
	}
	if overcommitFilter == "true" {
		baseFrom += " AND nr.is_overcommitted = true"
	} else if overcommitFilter == "false" {
		baseFrom += " AND nr.is_overcommitted = false"
	}

	if config.TagsFeatureEnabled() {
		tagFilters, tagErr := parseTagFiltersFromRequest(c)
		if tagErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": tagErr.Error()})
		}
		if len(tagFilters) > 0 {
			// Nodes are included when any workload namespace on the cluster matches the tag filter.
			tagClause, tagArgs, nextIdx := model.TagFilterExistsClause(orgID, "nr.cluster_uuid", "ock.namespace", tagFilters, argIdx)
			if tagClause != "" {
				baseFrom += " AND " + tagClause
				args = append(args, tagArgs...)
				argIdx = nextIdx
			}
		}
	}

	countSQL := `
		SELECT COUNT(*) FROM (
			SELECT DISTINCT nr.cluster_uuid, nr.node` + baseFrom + `
		) node_keys`
	var totalCount int
	if err := pool.QueryRow(ctx, countSQL, args...).Scan(&totalCount); err != nil {
		hlog.Warnf("GetNodeUtilizationRecs: count query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to load node utilization recommendations",
		})
	}

	sortEngine := nodeUtilPrimaryEngine
	if engineFilter != "" {
		sortEngine = engineFilter
	}
	sortTerm := nodeUtilPrimaryTerm
	if termFilter != "" {
		sortTerm = termFilter
	}

	orderFragment := listoptions.SQLOrderByFragment(orderCol, orderHow)
	pageSQL := `
		WITH filtered AS (
			SELECT nr.*` + baseFrom + `
		),
		node_page AS (
			SELECT f.cluster_uuid, f.node,
				MAX(CASE WHEN f.term = $` + strconv.Itoa(argIdx) + ` AND f.engine = $` + strconv.Itoa(argIdx+1) + `
					THEN f.estimated_monthly_savings_usd END) AS sort_savings
			FROM filtered f
			GROUP BY f.cluster_uuid, f.node
			ORDER BY ` + orderFragment + `, f.node ASC
			LIMIT $` + strconv.Itoa(argIdx+2) + ` OFFSET $` + strconv.Itoa(argIdx+3) + `
		)
		SELECT f.node, f.cluster_uuid, COALESCE(f.term, 'medium'), COALESCE(f.engine, 'cost'),
			COALESCE(f.cpu_util_p50, 0), COALESCE(f.cpu_util_p95, 0),
			COALESCE(f.mem_util_p50, 0), COALESCE(f.mem_util_p95, 0),
			COALESCE(f.cpu_overcommit_ratio, 0),
			COALESCE(f.is_underutilized, false), COALESCE(f.is_overcommitted, false),
			f.stranded_resource, COALESCE(f.pod_count, 0),
			COALESCE(f.trend_slope, 0), COALESCE(f.notification_codes, '{}'),
			f.recommended_cpu_cores, f.recommended_memory_gib, COALESCE(f.node_count_reduction, 0),
			f.estimated_monthly_savings_usd,
			COALESCE(f.updated_at, 'epoch'::timestamptz)
		FROM filtered f
		INNER JOIN node_page np ON f.cluster_uuid = np.cluster_uuid AND f.node = np.node
		ORDER BY np.sort_savings ` + orderHow + ` NULLS LAST, f.node, f.term, f.engine`

	pageArgs := append(append([]interface{}{}, args...), sortTerm, sortEngine, limit, offset)

	rows, err := pool.Query(ctx, pageSQL, pageArgs...)
	if err != nil {
		hlog.Warnf("GetNodeUtilizationRecs: query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to load node utilization recommendations",
		})
	}
	defer rows.Close()

	var rawRows []nodeUtilRow
	var scanErrors int
	for rows.Next() {
		var row nodeUtilRow
		err := rows.Scan(
			&row.Node, &row.ClusterUUID, &row.Term, &row.Engine,
			&row.CPUUtilP50, &row.CPUUtilP95,
			&row.MemUtilP50, &row.MemUtilP95,
			&row.CPUOvercommitRatio,
			&row.IsUnderutilized, &row.IsOvercommitted,
			&row.StrandedResource, &row.PodCount,
			&row.TrendSlope, &row.NotificationCodes,
			&row.RecommendedCPUCores, &row.RecommendedMemoryGiB, &row.NodeCountReduction,
			&row.EstimatedMonthlySavings,
			&row.UpdatedAt,
		)
		if err != nil {
			scanErrors++
			hlog.Warnf("GetNodeUtilizationRecs: scan failed (skipping row): %v", err)
			continue
		}
		rawRows = append(rawRows, row)
	}
	if err := rows.Err(); err != nil {
		hlog.Warnf("GetNodeUtilizationRecs: rows iteration failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to load node utilization recommendations",
		})
	}

	pagedRecs := groupNodeUtilizationRows(rawRows, engineFilter, termFilter)
	if pagedRecs == nil {
		pagedRecs = []model.NodeUtilizationRec{}
	}

	resp := model.NodeUtilizationListResponse{
		Meta: model.NodeUtilizationMeta{
			Count:    totalCount,
			Limit:    limit,
			Offset:   offset,
			Currency: fetchClusterCurrency(ctx, orgID, clusterFilter),
		},
		Data:  pagedRecs,
		Links: buildUtilLinks(c.Request(), totalCount, limit, offset),
	}
	if scanErrors > 0 {
		rowWord := "rows"
		if scanErrors == 1 {
			rowWord = "row"
		}
		resp.Meta.Warnings = append(resp.Meta.Warnings, fmt.Sprintf("%d %s could not be read", scanErrors, rowWord))
	}
	if deprecated {
		resp.Meta.Warnings = append([]string{nodeUtilizationDeprecationMsg}, resp.Meta.Warnings...)
	}
	attachTagWarningsToNodeUtil(&resp, c, orgID, len(pagedRecs))
	resp.Warnings = resp.Meta.Warnings

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, resp)
}

func groupNodeUtilizationRows(rows []nodeUtilRow, engineFilter, termFilter string) []model.NodeUtilizationRec {
	if len(rows) == 0 {
		return nil
	}

	type grouped struct {
		rec     model.NodeUtilizationRec
		primary *nodeUtilRow
		order   int
	}

	groups := make(map[nodeUtilKey]*grouped)
	order := make([]nodeUtilKey, 0)

	for i, row := range rows {
		if engineFilter != "" && row.Engine != engineFilter {
			continue
		}
		if termFilter != "" && row.Term != termFilter {
			continue
		}

		key := nodeUtilKey{ClusterUUID: row.ClusterUUID, Node: row.Node}
		g, exists := groups[key]
		if !exists {
			g = &grouped{order: i}
			groups[key] = g
			order = append(order, key)
		}

		if g.primary == nil || nodeUtilPrimaryScore(&rows[i]) > nodeUtilPrimaryScore(g.primary) {
			g.primary = &rows[i]
		}

		termKey := model.NodeUtilTermAPIKey(row.Term)
		if g.rec.RecommendationTerms == nil {
			g.rec.RecommendationTerms = make(map[string]model.NodeUtilizationTermRec)
		}
		termRec := g.rec.RecommendationTerms[termKey]
		if termRec.RecommendationEngines == nil {
			termRec.RecommendationEngines = &model.NodeUtilizationEngines{}
		}

		engineRec := nodeUtilRowToEngineRec(row)
		switch row.Engine {
		case "cost":
			termRec.RecommendationEngines.Cost = engineRec
		case "performance":
			termRec.RecommendationEngines.Performance = engineRec
		}
		g.rec.RecommendationTerms[termKey] = termRec
	}

	out := make([]model.NodeUtilizationRec, 0, len(order))
	for _, key := range order {
		g := groups[key]
		if g.primary == nil {
			continue
		}
		p := g.primary
		g.rec.Node = p.Node
		g.rec.ClusterUUID = p.ClusterUUID
		g.rec.RecommendationType = "cpu_memory_utilization"
		g.rec.Classification = model.NodeUtilizationClassification{
			IsUnderutilized:  p.IsUnderutilized,
			IsOvercommitted:  p.IsOvercommitted,
			StrandedResource: p.StrandedResource,
		}
		g.rec.Metrics = model.NodeUtilizationMetrics{
			CPUUtilP50: p.CPUUtilP50,
			CPUUtilP95: p.CPUUtilP95,
			MemUtilP50: p.MemUtilP50,
			MemUtilP95: p.MemUtilP95,
		}
		g.rec.PodCount = p.PodCount
		g.rec.CPUOvercommitRatio = p.CPUOvercommitRatio
		g.rec.TrendSlope = p.TrendSlope
		out = append(out, g.rec)
	}
	return out
}

func nodeUtilPrimaryScore(row *nodeUtilRow) int {
	if row == nil {
		return -1
	}
	score := 0
	if row.Term == nodeUtilPrimaryTerm {
		score += 2
	}
	if row.Engine == nodeUtilPrimaryEngine {
		score += 1
	}
	return score
}

func nodeUtilRowToEngineRec(row nodeUtilRow) *model.NodeUtilizationEngineRec {
	rec := &model.NodeUtilizationEngineRec{
		NodeCountReduction: row.NodeCountReduction,
		Notifications:      notifications.MapToKruizeFormat(row.NotificationCodes),
		UpdatedAt:          row.UpdatedAt.Format(time.RFC3339),
	}
	if row.RecommendedCPUCores.Valid {
		rec.RecommendedCPUCores = float32(row.RecommendedCPUCores.Float64)
	}
	if row.RecommendedMemoryGiB.Valid {
		rec.RecommendedMemoryGiB = float32(row.RecommendedMemoryGiB.Float64)
	}
	if row.EstimatedMonthlySavings.Valid {
		rec.EstimatedMonthlySavings = money.FormatCentsToSavingsPtr(&row.EstimatedMonthlySavings.Int64, money.DefaultCurrency)
	}
	return rec
}

func buildUtilLinks(r *http.Request, total, limit, offset int) model.PaginationLinks {
	return model.PaginationLinks{
		First:    buildLinkURL(r, 0, limit),
		Previous: buildPrevLink(r, offset, limit),
		Next:     buildNextLink(r, offset, limit, total),
		Last:     buildLinkURL(r, lastPageOffset(total, limit), limit),
	}
}

func buildLinkURL(r *http.Request, offset, limit int) string {
	q := r.URL.Query()
	q.Set("offset", strconv.Itoa(offset))
	q.Set("limit", strconv.Itoa(limit))
	params, _ := url.PathUnescape(q.Encode())
	return fmt.Sprintf("%s?%s", r.URL.Path, params)
}

func buildPrevLink(r *http.Request, offset, limit int) string {
	if offset <= 0 || limit <= 0 {
		return ""
	}
	prev := offset - limit
	if prev < 0 {
		prev = 0
	}
	return buildLinkURL(r, prev, limit)
}

func buildNextLink(r *http.Request, offset, limit, total int) string {
	if limit <= 0 || offset+limit >= total {
		return ""
	}
	return buildLinkURL(r, offset+limit, limit)
}

func lastPageOffset(total, limit int) int {
	if total <= 0 || limit <= 0 {
		return 0
	}
	return ((total - 1) / limit) * limit
}
