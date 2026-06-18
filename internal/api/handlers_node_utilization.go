package api

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

const defaultNodeUtilLimit = 10

const nodeUtilizationDeprecationMsg = `This path is deprecated. Use GET /api/cost-management/v1/recommendations/openshift/nodes for node CPU/memory utilization recommendations.`

var nodeUtilAllowedOrderBy = map[string]string{
	"node":                          "f.node",
	"estimated_monthly_savings":     "sort_savings",
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
	InstanceType            sql.NullString
	MachineSetName          sql.NullString
	Term                    string
	Engine                  string
	CPUUtilP50              float32
	CPUUtilP95              float32
	MemUtilP50              float32
	MemUtilP95              float32
	CPUOvercommitRatio      float32
	IsUnderutilized         bool
	IsOvercommitted         bool
	IdleState               string
	StrandedResource        *string
	PodCount                int64
	PodCapacity             sql.NullInt64
	TrendSlope              float32
	RecommendedCPUCores     sql.NullFloat64
	RecommendedMemoryGiB    sql.NullFloat64
	NodeCountReduction      int
	EstimatedMonthlySavings sql.NullInt64
	NotificationCodes       []int16
	SuggestedInstanceType   sql.NullString
	InstanceTypeReason      sql.NullString
	ConfidenceLevel         sql.NullFloat64
	DataDays                sql.NullInt32
	UpdatedAt               time.Time
	ExplDataDays            sql.NullInt32
	ExplTargetUtilizationBP sql.NullInt32
	ExplCurrentCPUMC        sql.NullInt64
	ExplCurrentMemKiB       sql.NullInt64
	ExplMaxCPUUsageP95MC    sql.NullInt64
	ExplMaxMemUsageP95KiB   sql.NullInt64
	ExplPodSchedulingHeadroomBP sql.NullInt32
	ExplEMAImbalanceBP      sql.NullInt32
	ExplConsolidationApplied sql.NullBool
	ExplSizingFormula       sql.NullString
}

const nodeUtilExplSelect = `,
			f.expl_data_days, f.expl_target_utilization_bp,
			f.expl_current_cpu_mc, f.expl_current_mem_kib,
			f.expl_max_cpu_usage_p95_mc, f.expl_max_mem_usage_p95_kib,
			f.expl_pod_scheduling_headroom_bp, f.expl_ema_imbalance_bp,
			f.expl_consolidation_applied, f.expl_sizing_formula`

type nodeUtilKey struct {
	ClusterUUID string
	Node        string
}
//
// order_by sorting (all DB-backed via SQL ORDER BY on node_utilization_recommendation_sets):
//   node (alias node_name), estimated_monthly_savings (alias estimated_monthly_savings_usd; sorts on sort_savings column).
//
// Supported filters: cluster, node, term, engine, is_underutilized, is_overcommitted,
// idle_state, stranded_resource (cpu|memory|none), instance_type, machineset_name.
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
	limit = capNodeListLimit(limit)

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

	utilCursor, hasUtilCursor, utilCursorErr := applyNodeUtilCursor(c, orderCol)
	if utilCursorErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": utilCursorErr.Error()})
	}
	if hasUtilCursor {
		offset = 0
	}

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
	termFilterRaw := queryparams.FirstFilter(c, "term")
	termFilter, termErr := queryparams.NormalizeRecommendationTermFilter(termFilterRaw)
	if termErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": termErr.Error()})
	}
	engineFilter := queryparams.FirstFilter(c, "engine")
	underutilFilter := queryparams.FirstFilter(c, "is_underutilized")
	overcommitFilter := queryparams.FirstFilter(c, "is_overcommitted")
	idleStateVals := queryparams.IncludeValues(c, "idle_state")
	strandedFilter := queryparams.FirstFilter(c, "stranded_resource")
	instanceTypeFilter := queryparams.FirstFilter(c, "instance_type")
	machinesetFilter := queryparams.FirstFilter(c, "machineset_name")

	if engineFilter != "" && engineFilter != "cost" && engineFilter != "performance" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "invalid engine"})
	}

	responseFormat, formatErr := resolveNodeUtilResponseFormat(c)
	if formatErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": formatErr.Error()})
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
	if len(idleStateVals) > 0 {
		states, idleErr := model.IdleStateFilterValues(strings.Join(idleStateVals, ","))
		if idleErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": idleErr.Error()})
		}
		baseFrom += " AND nr.idle_state = ANY($" + strconv.Itoa(argIdx) + ")"
		args = append(args, states)
		argIdx++
	}
	if strandedFilter != "" {
		strandedVal, matchNone, strandedErr := model.StrandedResourceFilterValue(strandedFilter)
		if strandedErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": strandedErr.Error()})
		}
		if matchNone {
			baseFrom += " AND nr.stranded_resource IS NULL"
		} else {
			baseFrom += " AND nr.stranded_resource = $" + strconv.Itoa(argIdx)
			args = append(args, strandedVal)
			argIdx++
		}
	}
	if instanceTypeFilter != "" {
		baseFrom += " AND nr.instance_type = $" + strconv.Itoa(argIdx)
		args = append(args, instanceTypeFilter)
		argIdx++
	}
	if machinesetFilter != "" {
		baseFrom += " AND nr.machineset_name = $" + strconv.Itoa(argIdx)
		args = append(args, machinesetFilter)
		argIdx++
	}

	if restrictNodes, allowedNodes := openshiftNodeRBACScope(userPerms); restrictNodes {
		if len(allowedNodes) == 0 {
			setRecommendationNoStore(c)
			if responseFormat == listoptions.ResponseFormatCSV {
				return streamNodeUtilizationCSV(c, nil)
			}
			return c.JSON(http.StatusOK, model.NodeUtilizationListResponse{
				Meta: model.NodeUtilizationMeta{Count: 0, Limit: limit, Offset: offset, Currency: costdata.DefaultCurrency},
				Data: []model.NodeUtilizationRec{},
			})
		}
		baseFrom += " AND nr.node = ANY($" + strconv.Itoa(argIdx) + ")"
		args = append(args, allowedNodes)
		argIdx++
	}

	if config.TagsFeatureEnabled() {
		tagFilters, tagErr := parseTagFiltersFromRequest(c)
		if tagErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": tagErr.Error()})
		}
		if len(tagFilters) > 0 {
			// Nodes are included when any workload namespace on the cluster matches the tag filter.
			tagClause, tagArgs, nextIdx := model.TagFilterExistsClause(orgID, "nr.cluster_uuid", "", tagFilters, argIdx)
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
	pageLimit := limit
	if pageLimit > 0 {
		pageLimit++
	}
	nodeKeysSeek := ""
	pageArgs := append(append([]interface{}{}, args...), sortTerm, sortEngine)
	seekIdx := argIdx + 2
	if hasUtilCursor {
		sortCol := "sort_savings"
		if orderCol == "f.node" {
			sortCol = "nk.node"
		}
		sortOp := ">"
		if orderHow == listoptions.OrderDesc {
			sortOp = "<"
		}
		tie := "(nk.cluster_uuid, nk.node)"
		if len(utilCursor.SortValue) > 0 {
			sortVal, decodeErr := decodeCursorSortValue(utilCursor.SortValue)
			if decodeErr != nil {
				return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": decodeErr.Error()})
			}
			nodeKeysSeek = fmt.Sprintf(` WHERE ((%s) %s $%d OR ((%s) IS NOT DISTINCT FROM $%d AND %s > ($%d, $%d)))`,
				sortCol, sortOp, seekIdx, sortCol, seekIdx, tie, seekIdx+1, seekIdx+2)
			pageArgs = append(pageArgs, sortVal, sortVal, utilCursor.ClusterUUID, utilCursor.Node)
			seekIdx += 4
		} else {
			nodeKeysSeek = fmt.Sprintf(` WHERE %s > ($%d, $%d)`, tie, seekIdx, seekIdx+1)
			pageArgs = append(pageArgs, utilCursor.ClusterUUID, utilCursor.Node)
			seekIdx += 2
		}
	}
	limitClause := ""
	if pageLimit > 0 {
		limitClause = ` LIMIT $` + strconv.Itoa(seekIdx)
		pageArgs = append(pageArgs, pageLimit)
		seekIdx++
		if !hasUtilCursor {
			limitClause += ` OFFSET $` + strconv.Itoa(seekIdx)
			pageArgs = append(pageArgs, offset)
		}
	}
	pageSQL := `
		WITH filtered AS (
			SELECT nr.*` + baseFrom + `
		),
		node_keys AS (
			SELECT f.cluster_uuid, f.node,
				MAX(CASE WHEN f.term = $` + strconv.Itoa(argIdx) + ` AND f.engine = $` + strconv.Itoa(argIdx+1) + `
					THEN f.estimated_savings_cents END) AS sort_savings
			FROM filtered f
			GROUP BY f.cluster_uuid, f.node
		),
		node_page AS (
			SELECT nk.cluster_uuid, nk.node, nk.sort_savings FROM node_keys nk` + nodeKeysSeek + `
			ORDER BY ` + strings.ReplaceAll(orderFragment, "f.", "nk.") + `, nk.node ASC` + limitClause + `
		)
		SELECT f.node, f.cluster_uuid, f.instance_type, f.machineset_name, COALESCE(f.term, 'medium'), COALESCE(f.engine, 'cost'),
			COALESCE(f.cpu_util_p50, 0), COALESCE(f.cpu_util_p95, 0),
			COALESCE(f.mem_util_p50, 0), COALESCE(f.mem_util_p95, 0),
			COALESCE(f.cpu_overcommit_ratio, 0),
			COALESCE(f.is_underutilized, false), COALESCE(f.is_overcommitted, false),
			COALESCE(f.idle_state, 'active'),
			f.stranded_resource, COALESCE(f.pod_count, 0), f.pod_capacity,
			COALESCE(f.trend_slope, 0), COALESCE(f.notification_codes, '{}'),
			f.recommended_cpu_cores, f.recommended_memory_gib, COALESCE(f.node_count_reduction, 0),
			f.estimated_savings_cents,
			f.suggested_instance_type, f.instance_type_reason,
			f.confidence_level, f.data_days,
			COALESCE(f.updated_at, 'epoch'::timestamptz)` + nodeUtilExplSelect + `
		FROM filtered f
		INNER JOIN node_page np ON f.cluster_uuid = np.cluster_uuid AND f.node = np.node
		ORDER BY np.sort_savings ` + orderHow + ` NULLS LAST, f.node, f.term, f.engine`

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
			&row.Node, &row.ClusterUUID, &row.InstanceType, &row.MachineSetName, &row.Term, &row.Engine,
			&row.CPUUtilP50, &row.CPUUtilP95,
			&row.MemUtilP50, &row.MemUtilP95,
			&row.CPUOvercommitRatio,
			&row.IsUnderutilized, &row.IsOvercommitted,
			&row.IdleState,
			&row.StrandedResource, &row.PodCount, &row.PodCapacity,
			&row.TrendSlope, &row.NotificationCodes,
			&row.RecommendedCPUCores, &row.RecommendedMemoryGiB, &row.NodeCountReduction,
			&row.EstimatedMonthlySavings,
			&row.SuggestedInstanceType, &row.InstanceTypeReason,
			&row.ConfidenceLevel, &row.DataDays,
			&row.UpdatedAt,
			&row.ExplDataDays, &row.ExplTargetUtilizationBP,
			&row.ExplCurrentCPUMC, &row.ExplCurrentMemKiB,
			&row.ExplMaxCPUUsageP95MC, &row.ExplMaxMemUsageP95KiB,
			&row.ExplPodSchedulingHeadroomBP, &row.ExplEMAImbalanceBP,
			&row.ExplConsolidationApplied, &row.ExplSizingFormula,
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

	pagedRecs := groupNodeUtilizationRows(rawRows, engineFilter, termFilter, false)
	enrichFleetConsolidationNotifications(pagedRecs, rawRows, termFilter, engineFilter)
	if pagedRecs == nil {
		pagedRecs = []model.NodeUtilizationRec{}
	}

	hasNext := limit > 0 && len(pagedRecs) > limit
	var nextCursor string
	if hasNext {
		last := pagedRecs[limit-1]
		sortVal := nodeUtilSortValue(last, orderByKey)
		nextCursor = nodeUtilNextCursor(last, sortVal, orderCol)
		pagedRecs = pagedRecs[:limit]
	}

	resp := model.NodeUtilizationListResponse{
		Meta: model.NodeUtilizationMeta{
			Count:      totalCount,
			Limit:      limit,
			Offset:     offset,
			HasNext:    hasNext,
			NextCursor: nextCursor,
			Currency:   fetchClusterCurrency(ctx, orgID, clusterFilter),
		},
		Data:  pagedRecs,
		Links: buildUtilLinks(c.Request(), totalCount, limit, offset),
	}
	if hasNext && nextCursor != "" {
		q := c.Request().URL.Query()
		q.Set("limit", strconv.Itoa(limit))
		q.Del("offset")
		q.Set("after", nextCursor)
		params, _ := url.PathUnescape(q.Encode())
		resp.Links.Next = fmt.Sprintf("%s?%s", c.Request().URL.Path, params)
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
	if responseFormat == listoptions.ResponseFormatCSV {
		return streamNodeUtilizationCSV(c, flattenNodeUtilizationForCSV(pagedRecs))
	}
	return c.JSON(http.StatusOK, resp)
}

func resolveNodeUtilResponseFormat(c echo.Context) (string, error) {
	return listoptions.ResolveResponseFormat(c.Request().Header.Get("Accept"), c.QueryParam("format"))
}

type nodeUtilCSVRow struct {
	Node                       string
	ClusterUUID                string
	InstanceType               string
	MachineSetName             string
	RecommendationType         string
	SuggestedInstanceType      string
	InstanceTypeReason         string
	Term                       string
	Engine                     string
	IsUnderutilized            bool
	IsOvercommitted            bool
	IdleState                  string
	StrandedResource           string
	Classification             string
	CPUUtilP50                 float32
	CPUUtilP95                 float32
	MemUtilP50                 float32
	MemUtilP95                 float32
	CPUOvercommitRatio         float32
	TrendSlope                 float32
	PodCount                   int64
	PodCapacity                string
	PodSchedulingHeadroom      string
	RecommendedCPUCores        float32
	RecommendedMemoryGiB       float32
	NodeCountReduction         int
	EstimatedMonthlySavings    string
	EstimatedMonthlySavingsUnits string
	UpdatedAt                  string
	NotificationCodes          string
}

func flattenNodeUtilizationForCSV(recs []model.NodeUtilizationRec) []nodeUtilCSVRow {
	var rows []nodeUtilCSVRow
	for _, rec := range recs {
		for termKey, termRec := range rec.RecommendationTerms {
			if termRec.RecommendationEngines == nil {
				continue
			}
			term := strings.TrimSuffix(termKey, "_term")
			for _, eng := range []struct {
				name string
				rec  *model.NodeUtilizationEngineRec
			}{
				{"cost", termRec.RecommendationEngines.Cost},
				{"performance", termRec.RecommendationEngines.Performance},
			} {
				if eng.rec == nil {
					continue
				}
				savings, savingsUnits := "", ""
				if eng.rec.EstimatedMonthlySavings != nil {
					savings = eng.rec.EstimatedMonthlySavings.Value
					savingsUnits = eng.rec.EstimatedMonthlySavings.Units
				}
				stranded := ""
				if rec.Classification.StrandedResource != nil {
					stranded = *rec.Classification.StrandedResource
				}
				rows = append(rows, nodeUtilCSVRow{
					Node:                         rec.Node,
					ClusterUUID:                  rec.ClusterUUID,
					InstanceType:                 rec.InstanceType,
					MachineSetName:               rec.MachineSetName,
					RecommendationType:           rec.RecommendationType,
					SuggestedInstanceType:        rec.SuggestedInstanceType,
					InstanceTypeReason:           rec.InstanceTypeReason,
					Term:                         term,
					Engine:                       eng.name,
					IsUnderutilized:              rec.Classification.IsUnderutilized,
					IsOvercommitted:              rec.Classification.IsOvercommitted,
					IdleState:                    rec.Classification.IdleState,
					StrandedResource:             stranded,
					Classification:               nodeUtilClassificationLabel(rec),
					CPUUtilP50:                   rec.Metrics.CPUUtilP50,
					CPUUtilP95:                   rec.Metrics.CPUUtilP95,
					MemUtilP50:                   rec.Metrics.MemUtilP50,
					MemUtilP95:                   rec.Metrics.MemUtilP95,
					CPUOvercommitRatio:           rec.CPUOvercommitRatio,
					TrendSlope:                   rec.TrendSlope,
					PodCount:                     rec.PodCount,
					PodCapacity:                  formatOptionalInt64(rec.PodCapacity),
					PodSchedulingHeadroom:        formatOptionalFloat32(rec.PodSchedulingHeadroom),
					RecommendedCPUCores:          eng.rec.RecommendedCPUCores,
					RecommendedMemoryGiB:         eng.rec.RecommendedMemoryGiB,
					NodeCountReduction:           eng.rec.NodeCountReduction,
					EstimatedMonthlySavings:      savings,
					EstimatedMonthlySavingsUnits: savingsUnits,
					UpdatedAt:                    eng.rec.UpdatedAt,
					NotificationCodes:            notificationMapCodesStr(eng.rec.Notifications),
				})
			}
		}
	}
	return rows
}

func nodeUtilClassificationLabel(rec model.NodeUtilizationRec) string {
	var parts []string
	if rec.Classification.IsUnderutilized {
		parts = append(parts, "underutilized")
	}
	if rec.Classification.IsOvercommitted {
		parts = append(parts, "overcommitted")
	}
	if rec.Classification.IdleState != "" && rec.Classification.IdleState != "active" {
		parts = append(parts, rec.Classification.IdleState)
	}
	if rec.Classification.StrandedResource != nil && *rec.Classification.StrandedResource != "" {
		parts = append(parts, "stranded_"+*rec.Classification.StrandedResource)
	}
	if len(parts) == 0 {
		return "active"
	}
	return strings.Join(parts, ";")
}

func streamNodeUtilizationCSV(c echo.Context, rows []nodeUtilCSVRow) error {
	filename := "node-utilization-" + time.Now().Format("20060102")
	c.Response().Header().Set(echo.HeaderContentType, "text/csv")
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
	pipeReader, pipeWriter := io.Pipe()
	go func() {
		var genErr error
		defer func() {
			if genErr != nil {
				_ = pipeWriter.CloseWithError(genErr)
			} else {
				_ = pipeWriter.Close()
			}
		}()
		w := csv.NewWriter(pipeWriter)
		genErr = w.Write([]string{
			"node", "cluster_uuid", "instance_type", "machineset_name", "recommendation_type",
			"suggested_instance_type", "instance_type_reason", "term", "engine",
			"is_underutilized", "is_overcommitted", "idle_state", "stranded_resource", "classification",
			"cpu_util_p50", "cpu_util_p95", "mem_util_p50", "mem_util_p95",
			"cpu_overcommit_ratio", "trend_slope",
			"pod_count", "pod_capacity", "pod_scheduling_headroom",
			"recommended_cpu_cores", "recommended_memory_gib", "node_count_reduction",
			"estimated_monthly_savings_value", "estimated_monthly_savings_units",
			"updated_at", "notification_codes",
		})
		if genErr != nil {
			return
		}
		for _, row := range rows {
			genErr = w.Write([]string{
				row.Node,
				row.ClusterUUID,
				row.InstanceType,
				row.MachineSetName,
				row.RecommendationType,
				row.SuggestedInstanceType,
				row.InstanceTypeReason,
				row.Term,
				row.Engine,
				strconv.FormatBool(row.IsUnderutilized),
				strconv.FormatBool(row.IsOvercommitted),
				row.IdleState,
				row.StrandedResource,
				row.Classification,
				fmt.Sprintf("%g", row.CPUUtilP50),
				fmt.Sprintf("%g", row.CPUUtilP95),
				fmt.Sprintf("%g", row.MemUtilP50),
				fmt.Sprintf("%g", row.MemUtilP95),
				fmt.Sprintf("%g", row.CPUOvercommitRatio),
				fmt.Sprintf("%g", row.TrendSlope),
				strconv.FormatInt(row.PodCount, 10),
				row.PodCapacity,
				row.PodSchedulingHeadroom,
				fmt.Sprintf("%g", row.RecommendedCPUCores),
				fmt.Sprintf("%g", row.RecommendedMemoryGiB),
				strconv.Itoa(row.NodeCountReduction),
				row.EstimatedMonthlySavings,
				row.EstimatedMonthlySavingsUnits,
				row.UpdatedAt,
				row.NotificationCodes,
			})
			if genErr != nil {
				return
			}
		}
		w.Flush()
		genErr = w.Error()
	}()
	return c.Stream(http.StatusOK, "text/csv", pipeReader)
}

func groupNodeUtilizationRows(rows []nodeUtilRow, engineFilter, termFilter string, includeExplanation bool) []model.NodeUtilizationRec {
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

		if g.primary == nil || nodeUtilPrimaryScore(&rows[i], termFilter, engineFilter) > nodeUtilPrimaryScore(g.primary, termFilter, engineFilter) {
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

		engineRec := nodeUtilRowToEngineRec(row, includeExplanation)
		switch row.Engine {
		case "cost":
			termRec.RecommendationEngines.Cost = engineRec
		case "performance":
			termRec.RecommendationEngines.Performance = engineRec
		}
		if row.ConfidenceLevel.Valid {
			termRec.ConfidenceLevel = float32(row.ConfidenceLevel.Float64)
		}
		if row.DataDays.Valid {
			termRec.DataDays = int(row.DataDays.Int32)
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
		if p.InstanceType.Valid {
			g.rec.InstanceType = p.InstanceType.String
		}
		if p.MachineSetName.Valid {
			g.rec.MachineSetName = p.MachineSetName.String
		}
		if p.SuggestedInstanceType.Valid {
			g.rec.SuggestedInstanceType = p.SuggestedInstanceType.String
		}
		if p.InstanceTypeReason.Valid {
			g.rec.InstanceTypeReason = p.InstanceTypeReason.String
		}
		g.rec.RecommendationType = "cpu_memory_utilization"
		idleState := p.IdleState
		if idleState == "" {
			idleState = "active"
		}
		g.rec.Classification = model.NodeUtilizationClassification{
			IsUnderutilized:  p.IsUnderutilized,
			IsOvercommitted:  p.IsOvercommitted,
			IdleState:        idleState,
			StrandedResource: p.StrandedResource,
		}
		g.rec.Metrics = model.NodeUtilizationMetrics{
			CPUUtilP50: p.CPUUtilP50,
			CPUUtilP95: p.CPUUtilP95,
			MemUtilP50: p.MemUtilP50,
			MemUtilP95: p.MemUtilP95,
		}
		g.rec.PodCount = p.PodCount
		g.rec.PodCapacity = nullInt64Ptr(p.PodCapacity)
		g.rec.PodSchedulingHeadroom = computePodSchedulingHeadroom(p.PodCount, p.PodCapacity)
		g.rec.CPUOvercommitRatio = p.CPUOvercommitRatio
		g.rec.TrendSlope = p.TrendSlope
		out = append(out, g.rec)
	}
	return out
}

func nodeUtilPrimaryScore(row *nodeUtilRow, termFilter, engineFilter string) int {
	if row == nil {
		return -1
	}
	primaryTerm := nodeUtilPrimaryTerm
	if termFilter != "" {
		primaryTerm = termFilter
	}
	primaryEngine := nodeUtilPrimaryEngine
	if engineFilter != "" {
		primaryEngine = engineFilter
	}
	score := 0
	if row.Term == primaryTerm {
		score += 2
	}
	if row.Engine == primaryEngine {
		score += 1
	}
	return score
}

func nodeUtilRowToEngineRec(row nodeUtilRow, includeExplanation bool) *model.NodeUtilizationEngineRec {
	rec := &model.NodeUtilizationEngineRec{
		NodeCountReduction: row.NodeCountReduction,
		Notifications:      notifications.MapToKruizeFormatForNode(row.NotificationCodes, row.StrandedResource, "", 0),
		UpdatedAt:          row.UpdatedAt.Format(time.RFC3339),
	}
	if row.RecommendedCPUCores.Valid {
		rec.RecommendedCPUCores = float32(row.RecommendedCPUCores.Float64)
	}
	if row.RecommendedMemoryGiB.Valid {
		rec.RecommendedMemoryGiB = float32(row.RecommendedMemoryGiB.Float64)
	}
	if row.EstimatedMonthlySavings.Valid {
		rec.EstimatedMonthlySavings = money.FormatCentsToAmountPtr(&row.EstimatedMonthlySavings.Int64, money.DefaultCurrency)
	}
	if includeExplanation {
		var confidence *float32
		if row.ConfidenceLevel.Valid {
			v := float32(row.ConfidenceLevel.Float64)
			confidence = &v
		}
		rec.Explanation = model.BuildNodeExplanationAPI(
			confidence,
			nullInt32ToIntPtr(row.ExplDataDays),
			nullInt32Ptr(row.ExplTargetUtilizationBP),
			nullInt32Ptr(row.ExplPodSchedulingHeadroomBP),
			nullInt32Ptr(row.ExplEMAImbalanceBP),
			nullInt64Ptr(row.ExplCurrentCPUMC),
			nullInt64Ptr(row.ExplMaxCPUUsageP95MC),
			nullInt64Ptr(row.ExplCurrentMemKiB),
			nullInt64Ptr(row.ExplMaxMemUsageP95KiB),
			nullBoolPtr(row.ExplConsolidationApplied),
			nullStringPtr(row.ExplSizingFormula),
		)
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

// computePodSchedulingHeadroom returns (capacity - count) / capacity when capacity is known.
func computePodSchedulingHeadroom(podCount int64, podCapacity sql.NullInt64) *float32 {
	if !podCapacity.Valid || podCapacity.Int64 <= 0 {
		return nil
	}
	h := float32(podCapacity.Int64-podCount) / float32(podCapacity.Int64)
	return &h
}

func formatOptionalInt64(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

func formatOptionalFloat32(v *float32) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%g", *v)
}

type nodeUtilFleetKey struct {
	clusterUUID    string
	machineSetName string
}

// enrichFleetConsolidationNotifications adds MachineSet-scoped fleet consolidation text to API notifications.
func enrichFleetConsolidationNotifications(recs []model.NodeUtilizationRec, rawRows []nodeUtilRow, termFilter, engineFilter string) {
	if len(recs) == 0 {
		return
	}
	term := nodeUtilPrimaryTerm
	if termFilter != "" {
		term = termFilter
	}
	engine := nodeUtilPrimaryEngine
	if engineFilter != "" {
		engine = engineFilter
	}

	totals := make(map[nodeUtilFleetKey]int)
	for _, row := range rawRows {
		if row.Term != term || row.Engine != engine {
			continue
		}
		if !row.MachineSetName.Valid || row.MachineSetName.String == "" || row.NodeCountReduction <= 0 {
			continue
		}
		k := nodeUtilFleetKey{clusterUUID: row.ClusterUUID, machineSetName: row.MachineSetName.String}
		totals[k] += row.NodeCountReduction
	}

	for i := range recs {
		if recs[i].MachineSetName == "" {
			continue
		}
		fleetTotal := totals[nodeUtilFleetKey{clusterUUID: recs[i].ClusterUUID, machineSetName: recs[i].MachineSetName}]
		if fleetTotal <= 0 {
			continue
		}
		termKey := model.NodeUtilTermAPIKey(term)
		termRec, ok := recs[i].RecommendationTerms[termKey]
		if !ok || termRec.RecommendationEngines == nil {
			continue
		}
		var engRec *model.NodeUtilizationEngineRec
		switch engine {
		case "performance":
			engRec = termRec.RecommendationEngines.Performance
		default:
			engRec = termRec.RecommendationEngines.Cost
		}
		if engRec == nil {
			continue
		}
		var codes []int16
		var stranded *string
		for _, row := range rawRows {
			if row.Node == recs[i].Node && row.ClusterUUID == recs[i].ClusterUUID && row.Term == term && row.Engine == engine {
				codes = row.NotificationCodes
				stranded = row.StrandedResource
				break
			}
		}
		engRec.Notifications = notifications.MapToKruizeFormatForNode(codes, stranded, recs[i].MachineSetName, fleetTotal)
	}
}

func nullInt32ToIntPtr(v sql.NullInt32) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int32)
	return &i
}

func nullInt32Ptr(v sql.NullInt32) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}

func nullBoolPtr(v sql.NullBool) *bool {
	if !v.Valid {
		return nil
	}
	return &v.Bool
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}
