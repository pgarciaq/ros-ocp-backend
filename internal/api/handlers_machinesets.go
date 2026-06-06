package api

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

const defaultMachineSetLimit = 10

// GetMachineSetRecommendations handles GET /recommendations/openshift/machinesets.
//
// Aggregates node_recommendations by machineset_name for the requested term and cost engine.
// Supported filters: cluster (UUID), machineset_name (exact or wildcard with *).
func GetMachineSetRecommendations(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	limit := defaultMachineSetLimit
	if v := strings.TrimSpace(c.QueryParam("limit")); v != "" {
		n, parseErr := strconv.Atoi(v)
		if parseErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "invalid limit"})
		}
		if n < 0 {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "limit cannot be negative"})
		}
		if n == 0 {
			limit = defaultMachineSetLimit
		} else if n > listoptions.MaxLimit {
			limit = listoptions.MaxLimit
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

	cursor, hasCursor, cursorErr := applyMachineSetCursor(c)
	if cursorErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": cursorErr.Error()})
	}
	if hasCursor {
		offset = 0
	}

	term, termErr := resolveMachineSetTerm(c)
	if termErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": termErr.Error()})
	}

	responseFormat, formatErr := listoptions.ResolveResponseFormat(c.Request().Header.Get("Accept"), c.QueryParam("format"))
	if formatErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": formatErr.Error()})
	}
	if responseFormat == listoptions.ResponseFormatCSV {
		limit = config.GetConfig().RecordLimitCSV
		offset = 0
	}

	pool := database.GetPool()
	if pool == nil {
		hlog.Warnf("GetMachineSetRecommendations: database pool unavailable")
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	ctx := c.Request().Context()

	allClusters, err := getClustersForOrg(ctx, orgID)
	if err != nil {
		hlog.Warnf("GetMachineSetRecommendations: failed to resolve clusters: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	allowedClusters := filterClustersByRBAC(allClusters, userPerms)
	if len(allowedClusters) == 0 {
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, model.MachineSetRecommendationListResponse{
			Meta:  model.MachineSetRecommendationMeta{Count: 0, Limit: limit, Offset: offset, Currency: resolveListCurrencyFromRequest(c, orgID)},
			Data:  []model.MachineSetRecommendation{},
			Links: buildMachineSetLinks(c.Request(), 0, limit, offset),
		})
	}

	clusterFilter := strings.TrimSpace(queryparams.FirstFilter(c, "cluster"))
	if clusterFilter == "" {
		clusterFilter = strings.TrimSpace(c.QueryParam("cluster_uuid"))
	}
	if clusterFilter != "" {
		found := false
		for _, id := range allowedClusters {
			if id == clusterFilter {
				found = true
				break
			}
		}
		if !found {
			setRecommendationNoStore(c)
			return c.JSON(http.StatusOK, model.MachineSetRecommendationListResponse{
				Meta:  model.MachineSetRecommendationMeta{Count: 0, Limit: limit, Offset: offset, Currency: resolveListCurrencyFromRequest(c, orgID)},
				Data:  []model.MachineSetRecommendation{},
				Links: buildMachineSetLinks(c.Request(), 0, limit, offset),
			})
		}
		allowedClusters = []string{clusterFilter}
	}

	machinesetFilter := queryparams.FirstFilter(c, "machineset_name")
	if machinesetFilter == "" {
		machinesetFilter = strings.TrimSpace(c.QueryParam("machineset_name"))
	}

	baseFrom := `
		FROM node_recommendations nr
		LEFT JOIN clusters c ON c.cluster_uuid = nr.cluster_uuid
		WHERE nr.org_id = $1
		  AND nr.cluster_uuid::text = ANY($2)
		  AND nr.term = $3
		  AND nr.engine = 'cost'
		  AND nr.machineset_name IS NOT NULL
		  AND BTRIM(nr.machineset_name) <> ''`

	args := []interface{}{orgID, allowedClusters, term}
	argIdx := 4

	if clusterFilter != "" {
		baseFrom += " AND nr.cluster_uuid = $" + strconv.Itoa(argIdx)
		args = append(args, clusterFilter)
		argIdx++
	}

	msClause, msArg, nextIdx, msErr := machinesetNameFilterClause(machinesetFilter, argIdx)
	if msErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": msErr.Error()})
	}
	if msClause != "" {
		baseFrom += msClause
		if msArg != nil {
			args = append(args, msArg)
		}
		argIdx = nextIdx
	}

	if restrictNodes, allowedNodes := openshiftNodeRBACScope(userPerms); restrictNodes {
		if len(allowedNodes) == 0 {
			setRecommendationNoStore(c)
			return c.JSON(http.StatusOK, model.MachineSetRecommendationListResponse{
				Meta:  model.MachineSetRecommendationMeta{Count: 0, Limit: limit, Offset: offset, Currency: resolveListCurrencyFromRequest(c, orgID)},
				Data:  []model.MachineSetRecommendation{},
				Links: buildMachineSetLinks(c.Request(), 0, limit, offset),
			})
		}
		baseFrom += " AND nr.node = ANY($" + strconv.Itoa(argIdx) + ")"
		args = append(args, allowedNodes)
		argIdx++
	}

	groupedSQL := `
		SELECT
			nr.machineset_name,
			nr.cluster_uuid::text,
			COALESCE(MAX(c.cluster_alias), '') AS cluster_alias,
			COALESCE(MAX(NULLIF(BTRIM(nr.instance_type), '')), '') AS instance_type,
			COUNT(DISTINCT nr.node)::int AS current_node_count,
			COALESCE(SUM(nr.node_count_reduction), 0)::int AS excess_nodes,
			COALESCE(SUM(nr.estimated_monthly_savings_usd), 0)::bigint AS total_savings_cents,
			COALESCE(AVG(nr.cpu_util_p95), 0)::float AS avg_cpu,
			COALESCE(AVG(nr.mem_util_p95), 0)::float AS avg_memory,
			array_agg(DISTINCT nr.node ORDER BY nr.node) AS nodes` + baseFrom + `
		GROUP BY nr.machineset_name, nr.cluster_uuid`

	countSQL := `SELECT COUNT(*) FROM (` + groupedSQL + `) ms_groups`
	var totalCount int
	if err := pool.QueryRow(ctx, countSQL, args...).Scan(&totalCount); err != nil {
		hlog.Warnf("GetMachineSetRecommendations: count query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to load MachineSet recommendations",
		})
	}

	pageSQL := `SELECT machineset_name, cluster_uuid, cluster_alias, instance_type, current_node_count,
		excess_nodes, total_savings_cents, avg_cpu, avg_memory, nodes
		FROM (` + groupedSQL + `) ms_groups`

	if hasCursor {
		seekSQL, seekArgs, nextIdx, seekErr := machineSetSeekSQL(cursor, len(cursor.SortValue) > 0, argIdx)
		if seekErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": seekErr.Error()})
		}
		pageSQL += ` WHERE ` + seekSQL
		args = append(args, seekArgs...)
		argIdx = nextIdx
	}

	pageSQL += ` ORDER BY total_savings_cents DESC, machineset_name ASC, cluster_uuid ASC`

	pageLimit := limit
	if pageLimit > 0 {
		pageLimit++
	}
	pageSQL += ` LIMIT $` + strconv.Itoa(argIdx)
	pageArgs := append(append([]interface{}{}, args...), pageLimit)
	argIdx++

	if !hasCursor {
		pageSQL += ` OFFSET $` + strconv.Itoa(argIdx)
		pageArgs = append(pageArgs, offset)
	}

	rows, err := pool.Query(ctx, pageSQL, pageArgs...)
	if err != nil {
		hlog.Warnf("GetMachineSetRecommendations: query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to load MachineSet recommendations",
		})
	}
	defer rows.Close()

	data := make([]model.MachineSetRecommendation, 0)
	for rows.Next() {
		var rec model.MachineSetRecommendation
		var totalCents int64
		var nodes []string
		if scanErr := rows.Scan(
			&rec.MachineSetName,
			&rec.ClusterUUID,
			&rec.ClusterAlias,
			&rec.InstanceType,
			&rec.CurrentNodeCount,
			&rec.ExcessNodes,
			&totalCents,
			&rec.AvgCPUUtilization,
			&rec.AvgMemoryUtilization,
			&nodes,
		); scanErr != nil {
			hlog.Warnf("GetMachineSetRecommendations: scan failed (skipping row): %v", scanErr)
			continue
		}
		if totalCents > 0 {
			currency := resolveListCurrencyFromRequest(c, orgID)
			rec.TotalMonthlySavings = money.FormatCentsToSavingsPtr(&totalCents, currency)
		}
		rec.RecommendedNodeCount = rec.CurrentNodeCount - rec.ExcessNodes
		if rec.RecommendedNodeCount < 0 {
			rec.RecommendedNodeCount = 0
		}
		if nodes == nil {
			rec.Nodes = []string{}
		} else {
			rec.Nodes = nodes
		}
		data = append(data, rec)
	}
	if err := rows.Err(); err != nil {
		hlog.Warnf("GetMachineSetRecommendations: rows iteration failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to load MachineSet recommendations",
		})
	}
	if data == nil {
		data = []model.MachineSetRecommendation{}
	}

	hasNext := false
	var nextCursor string
	if limit > 0 && len(data) > limit {
		hasNext = true
		last := data[limit-1]
		nextCursor = machineSetNextCursor(last, machineSetSortCents(last))
		data = data[:limit]
	}

	links := buildMachineSetLinks(c.Request(), totalCount, limit, offset)
	applyModelKeysetNextLink(&links, c.Request(), limit, hasNext, nextCursor)

	setRecommendationNoStore(c)
	if responseFormat == listoptions.ResponseFormatCSV {
		return streamCSV(c, csvFilename("machineset-recommendations"), func(ctx context.Context, w io.Writer) error {
			return generateMachineSetRecCSV(ctx, w, term, data)
		})
	}
	return c.JSON(http.StatusOK, model.MachineSetRecommendationListResponse{
		Meta: model.MachineSetRecommendationMeta{
			Count: totalCount, Limit: limit, Offset: offset,
			HasNext: hasNext, NextCursor: nextCursor,
			Currency: resolveListCurrencyFromRequest(c, orgID),
		},
		Data:  data,
		Links: links,
	})
}

func resolveMachineSetTerm(c echo.Context) (string, error) {
	term := strings.TrimSpace(queryparams.FirstFilter(c, "term"))
	if term == "" {
		term = strings.TrimSpace(c.QueryParam("term"))
	}
	if term == "" {
		return nodeUtilPrimaryTerm, nil
	}
	return queryparams.NormalizeRecommendationTermFilter(term)
}

// machinesetNameFilterClause builds an exact or ILIKE filter for machineset_name.
// Wildcard: * in the value is translated to SQL % (Koku-style substring match).
func machinesetNameFilterClause(filter string, argIdx int) (clause string, arg interface{}, nextIdx int, err error) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return "", nil, argIdx, nil
	}
	if strings.Contains(filter, "*") {
		pattern := strings.ReplaceAll(filter, "*", "%")
		return " AND nr.machineset_name ILIKE $" + strconv.Itoa(argIdx), pattern, argIdx + 1, nil
	}
	return " AND nr.machineset_name = $" + strconv.Itoa(argIdx), filter, argIdx + 1, nil
}

func buildMachineSetLinks(r *http.Request, total, limit, offset int) model.PaginationLinks {
	return model.PaginationLinks{
		First:    buildLinkURL(r, 0, limit),
		Previous: buildPrevLink(r, offset, limit),
		Next:     buildNextLink(r, offset, limit, total),
		Last:     buildLinkURL(r, lastPageOffset(total, limit), limit),
	}
}
