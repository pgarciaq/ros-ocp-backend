package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

var nodeGPUTimeslicingOrderBy = map[string]string{
	"node_name":                 "t.node_name",
	"cluster_uuid":              "t.cluster_uuid::text",
	"gpu_model":                 "t.gpu_model",
	"gpu_model_name":            "t.gpu_model",
	"recommended_replicas":      "t.recommended_replicas",
	"confidence":                "t.confidence",
	"estimated_monthly_savings": "t.estimated_savings_cents",
	"total_node_savings":        "t.estimated_savings_cents",
	"total_node_savings_usd":    "t.estimated_savings_cents",
}

const nodeGPUTimeslicingSelectSQL = `
	SELECT t.org_id, t.cluster_uuid, t.node_name, t.gpu_model, t.term,
		t.recommended_replicas, t.confidence, t.confidence_level,
		t.candidate_count, t.impacted_count,
		t.candidate_containers, t.impacted_containers,
		t.notification_codes,
		t.estimated_savings_cents, t.savings_per_gpu_cents,
		t.last_seen_at, t.updated_at,
		t.expl_data_days, t.expl_candidate_count, t.expl_impacted_count, t.expl_classification_rule
	FROM node_gpu_timeslicing_recommendations t`

func orgHasPersistedNodeGPUTimeslicingRecs(ctx context.Context, pool *pgxpool.Pool, orgID string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM node_gpu_timeslicing_recommendations WHERE org_id = $1 LIMIT 1
		)`, orgID).Scan(&exists)
	return exists, err
}

// countPersistedNodeGPUTimeslicingRecs returns the number of actionable
// time-slicing recommendations in the persisted table, scoped to the given
// org and RBAC-filtered cluster UUIDs. When userPerms restricts node access,
// only matching node names are counted.
func countPersistedNodeGPUTimeslicingRecs(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUIDs []string, userPerms map[string][]string) (int, error) {
	if len(clusterUUIDs) == 0 {
		return 0, nil
	}
	q := `SELECT COUNT(*) FROM node_gpu_timeslicing_recommendations t
		WHERE t.org_id = $1 AND t.cluster_uuid::text = ANY($2::text[])`
	args := []interface{}{orgID, clusterUUIDs}
	argIdx := 3

	if restrictNodes, allowedNodes := openshiftNodeRBACScope(userPerms); restrictNodes {
		if len(allowedNodes) == 0 {
			return 0, nil
		}
		q += fmt.Sprintf(" AND t.node_name = ANY($%d)", argIdx)
		args = append(args, allowedNodes)
	}

	var n int
	err := pool.QueryRow(ctx, q, args...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count persisted node GPU timeslicing recs: %w", err)
	}
	return n, nil
}

func respondNodeGPURecommendationsFromTable(
	c echo.Context,
	ctx context.Context,
	pool *pgxpool.Pool,
	orgIDStr string,
	userPerms map[string][]string,
	opts listoptions.ListOptions,
	clusterUUIDs []string,
	clusterFilter, nodeNameFilter, gpuModelFilter, termFilter string,
	includeExplanation bool,
) error {
	hlog := requestLogger(c, orgIDStr)

	cursor, hasCursor, cursorErr := applyNodeGPUCursor(c, opts.OrderBy)
	if cursorErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": cursorErr.Error()})
	}
	if hasCursor {
		opts.Offset = 0
	}

	pageLimit := opts.Limit
	if opts.Format == listoptions.ResponseFormatCSV {
		pageLimit = capNodeListLimit(config.GetConfig().RecordLimitCSV)
		if pageLimit <= 0 {
			pageLimit = listoptions.MaxLimit
		}
	}
	if pageLimit <= 0 {
		pageLimit = listoptions.DefaultLimit
	}

	orderByKey := opts.OrderBy
	if orderByKey == "" {
		orderByKey = listoptions.DefaultNodeRecsOrderBy
	}
	orderCol, ok := nodeGPUTimeslicingOrderBy[orderByKey]
	if !ok {
		if mapped, mappedOK := listoptions.NodeRecsAllowedOrderBy[orderByKey]; mappedOK {
			orderCol, ok = nodeGPUTimeslicingOrderBy[mapped]
		}
	}
	if !ok {
		orderCol = nodeGPUTimeslicingOrderBy[listoptions.DefaultNodeRecsOrderBy]
	}

	filterSQL, args, argIdx, tagFilterActive, tagFilterErr := buildNodeGPUTimeslicingFilterSQL(
		c, orgIDStr, clusterUUIDs, userPerms,
		clusterFilter, nodeNameFilter, gpuModelFilter, termFilter,
	)
	if tagFilterErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": tagFilterErr.Error()})
	}

	if tagFilterActive {
		allRecs, listErr := queryPersistedNodeGPURecs(ctx, pool, filterSQL, args, orderCol, opts.OrderHow, 0, 0, false, includeExplanation)
		if listErr != nil {
			hlog.Errorf("GetNodeRecommendations: persisted list failed: %v", listErr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to load node GPU recommendations"})
		}
		allRecs = filterNodeRecsByRBAC(allRecs, userPerms)
		var tagErr error
		allRecs, tagErr = applyNodeGPURecTagFilters(ctx, c, pool, orgIDStr, allRecs)
		if tagErr != nil {
			return tagErr
		}
		if allRecs == nil {
			allRecs = []model.NodeGPURecommendation{}
		}
		totalCount := len(allRecs)
		paged, hasNext, nextCursor, pageErr := paginateNodeGPURecs(allRecs, listoptions.ListOptions{
			Limit:    pageLimit,
			Offset:   opts.Offset,
			OrderBy:  opts.OrderBy,
			OrderHow: opts.OrderHow,
		}, cursor, hasCursor)
		if pageErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": pageErr.Error()})
		}
		if opts.Format == listoptions.ResponseFormatCSV && pageLimit > 0 && len(paged) > pageLimit {
			paged = paged[:pageLimit]
			hasNext = false
			nextCursor = ""
		}
		nodeCurrency := nodeGPURecsCurrency(ctx, orgIDStr, clusterFilter, paged)
		totalSavings := sumNodeGPUSavings(paged, nodeCurrency)
		return respondNodeGPURecommendations(c, listoptions.ListOptions{
			Limit: pageLimit, Offset: opts.Offset, OrderBy: opts.OrderBy, OrderHow: opts.OrderHow, Format: opts.Format,
		}, totalCount, paged, totalSavings, nil, nodeCurrency, hasNext, nextCursor)
	}

	countQuery := `SELECT COUNT(*) FROM node_gpu_timeslicing_recommendations t WHERE t.org_id = $1` + filterSQL
	var totalCount int
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		hlog.Errorf("GetNodeRecommendations: persisted count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to load node GPU recommendations"})
	}

	pageLimitPlusOne := pageLimit
	if pageLimit > 0 {
		pageLimitPlusOne++
	}
	paged, listErr := queryPersistedNodeGPURecsPage(
		ctx, pool, filterSQL, args, argIdx, orderCol, opts.OrderHow,
		pageLimitPlusOne, opts.Offset, cursor, hasCursor, includeExplanation,
	)
	if listErr != nil {
		if strings.Contains(listErr.Error(), "invalid after parameter") {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": listErr.Error()})
		}
		hlog.Errorf("GetNodeRecommendations: persisted page failed: %v", listErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to load node GPU recommendations"})
	}

	hasNext := false
	nextCursor := ""
	if pageLimit > 0 && len(paged) > pageLimit {
		hasNext = true
		last := paged[pageLimit-1]
		nextCursor = nodeGPUNextCursor(last, nodeGPUSortValue(last, opts.OrderBy), opts.OrderBy)
		paged = paged[:pageLimit]
	}

	if opts.Format == listoptions.ResponseFormatCSV && pageLimit > 0 && len(paged) > pageLimit {
		paged = paged[:pageLimit]
		hasNext = false
		nextCursor = ""
	}

	nodeCurrency := nodeGPURecsCurrency(ctx, orgIDStr, clusterFilter, paged)
	totalSavings := sumNodeGPUSavings(paged, nodeCurrency)
	return respondNodeGPURecommendations(c, listoptions.ListOptions{
		Limit: pageLimit, Offset: opts.Offset, OrderBy: opts.OrderBy, OrderHow: opts.OrderHow, Format: opts.Format,
	}, totalCount, paged, totalSavings, nil, nodeCurrency, hasNext, nextCursor)
}

func buildNodeGPUTimeslicingFilterSQL(
	c echo.Context,
	orgID string,
	clusterUUIDs []string,
	userPerms map[string][]string,
	clusterFilter, nodeNameFilter, gpuModelFilter, termFilter string,
) (filterSQL string, args []interface{}, nextArgIdx int, tagFilterActive bool, err error) {
	args = []interface{}{orgID}
	argIdx := 2

	if len(clusterUUIDs) > 0 {
		filterSQL += " AND t.cluster_uuid::text = ANY($" + strconv.Itoa(argIdx) + ")"
		args = append(args, clusterUUIDs)
		argIdx++
	}
	if clusterFilter != "" {
		filterSQL += " AND t.cluster_uuid = $" + strconv.Itoa(argIdx) + "::uuid"
		args = append(args, clusterFilter)
		argIdx++
	}
	if nodeNameFilter != "" {
		filterSQL += " AND lower(t.node_name) = lower($" + strconv.Itoa(argIdx) + ")"
		args = append(args, nodeNameFilter)
		argIdx++
	}
	if gpuModelFilter != "" {
		filterSQL += " AND t.gpu_model ILIKE $" + strconv.Itoa(argIdx)
		args = append(args, "%"+gpuModelFilter+"%")
		argIdx++
	}
	if termFilter != "" {
		filterSQL += " AND lower(t.term) = lower($" + strconv.Itoa(argIdx) + ")"
		args = append(args, termFilter)
		argIdx++
	}

	if restrictNodes, allowedNodes := openshiftNodeRBACScope(userPerms); restrictNodes {
		if len(allowedNodes) == 0 {
			filterSQL += " AND false"
		} else {
			filterSQL += " AND t.node_name = ANY($" + strconv.Itoa(argIdx) + ")"
			args = append(args, allowedNodes)
			argIdx++
		}
	}

	if config.TagsFeatureEnabled() {
		tagFilters, tagErr := parseTagFiltersFromRequest(c)
		if tagErr != nil {
			return "", nil, 0, false, tagErr
		}
		if len(tagFilters) > 0 {
			tagFilterActive = true
		}
	}

	return filterSQL, args, argIdx, tagFilterActive, nil
}

func queryPersistedNodeGPURecsPage(
	ctx context.Context,
	pool *pgxpool.Pool,
	filterSQL string,
	args []interface{},
	argIdx int,
	orderCol, orderHow string,
	limit, offset int,
	cursor NodeGPUCursor,
	hasCursor bool,
	includeExplanation bool,
) ([]model.NodeGPURecommendation, error) {
	query := nodeGPUTimeslicingSelectSQL + `
		WHERE t.org_id = $1` + filterSQL

	if hasCursor {
		seekSQL, seekArgs, nextIdx, seekErr := nodeGPUTimeslicingSeekSQL(orderCol, orderHow, cursor, len(cursor.SortValue) > 0, argIdx)
		if seekErr != nil {
			return nil, seekErr
		}
		query += " AND " + seekSQL
		args = append(args, seekArgs...)
		argIdx = nextIdx
	}

	query += " ORDER BY " + nodeGPUTimeslicingOrderNulls(orderCol, orderHow) +
		", t.cluster_uuid ASC, t.node_name ASC, t.gpu_model ASC, t.term ASC"

	if limit > 0 {
		query += " LIMIT $" + strconv.Itoa(argIdx)
		args = append(args, limit)
		argIdx++
		if !hasCursor {
			query += " OFFSET $" + strconv.Itoa(argIdx)
			args = append(args, offset)
		}
	}

	recs, err := queryPersistedNodeGPURecRows(ctx, pool, query, args, includeExplanation)
	return recs, err
}

func queryPersistedNodeGPURecs(
	ctx context.Context,
	pool *pgxpool.Pool,
	filterSQL string,
	args []interface{},
	orderCol, orderHow string,
	limit, offset int,
	useOffset bool,
	includeExplanation bool,
) ([]model.NodeGPURecommendation, error) {
	query := nodeGPUTimeslicingSelectSQL + `
		WHERE t.org_id = $1` + filterSQL +
		" ORDER BY " + nodeGPUTimeslicingOrderNulls(orderCol, orderHow) +
		", t.cluster_uuid ASC, t.node_name ASC, t.gpu_model ASC, t.term ASC"
	if limit > 0 {
		query += " LIMIT " + strconv.Itoa(limit)
	}
	if useOffset && offset > 0 {
		query += " OFFSET " + strconv.Itoa(offset)
	}
	return queryPersistedNodeGPURecRows(ctx, pool, query, args, includeExplanation)
}

func queryPersistedNodeGPURecRows(ctx context.Context, pool *pgxpool.Pool, query string, args []interface{}, includeExplanation bool) ([]model.NodeGPURecommendation, error) {
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	clusterCurrency := make(map[string]string)
	var recs []model.NodeGPURecommendation
	for rows.Next() {
		row, scanErr := scanNodeGPUTimeslicingRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		clusterID := row.ClusterUUID.String()
		currency, ok := clusterCurrency[clusterID]
		if !ok {
			currency = fetchClusterCurrency(ctx, row.OrgID, clusterID)
			clusterCurrency[clusterID] = currency
		}
		recs = append(recs, persistedRowToNodeGPURecommendation(row, currency, includeExplanation))
	}
	if recs == nil {
		recs = []model.NodeGPURecommendation{}
	}
	return recs, rows.Err()
}

func scanNodeGPUTimeslicingRow(row pgx.Row) (model.NodeGPUTimeslicingRecommendation, error) {
	var r model.NodeGPUTimeslicingRecommendation
	var notificationCodes []int16
	err := row.Scan(
		&r.OrgID, &r.ClusterUUID, &r.NodeName, &r.GPUModel, &r.Term,
		&r.RecommendedReplicas, &r.Confidence, &r.ConfidenceLevel,
		&r.CandidateCount, &r.ImpactedCount,
		&r.CandidateContainers, &r.ImpactedContainers,
		&notificationCodes,
		&r.EstimatedSavingsCents, &r.SavingsPerGPUCents,
		&r.LastSeenAt, &r.UpdatedAt,
		&r.ExplDataDays, &r.ExplCandidateCount, &r.ExplImpactedCount, &r.ExplClassificationRule,
	)
	if err != nil {
		return r, err
	}
	r.NotificationCodes = model.SmallintArray(notificationCodes)
	return r, nil
}

func persistedRowToNodeGPURecommendation(row model.NodeGPUTimeslicingRecommendation, currency string, includeExplanation bool) model.NodeGPURecommendation {
	rec := model.NodeGPURecommendation{
		NodeName:            row.NodeName,
		ClusterUUID:         row.ClusterUUID.String(),
		Term:                row.Term,
		RecommendationType:  "gpu_time_slicing",
		GPUModel:            row.GPUModel,
		RecommendedReplicas: int(row.RecommendedReplicas),
		Confidence:          row.Confidence,
		ConfidenceLevel:     row.ConfidenceLevel,
		NotificationCodes:   []int16(row.NotificationCodes),
	}
	if row.SavingsPerGPUCents != nil {
		rec.SavingsPerGPU = money.FormatCentsToAmountPtr(row.SavingsPerGPUCents, currency)
	}
	if row.EstimatedSavingsCents != nil {
		rec.EstimatedMonthlySavings = money.FormatCentsToAmountPtr(row.EstimatedSavingsCents, currency)
	}
	rec.CandidateContainers = []model.NodeContainerRef(row.CandidateContainers)
	if rec.CandidateContainers == nil {
		rec.CandidateContainers = []model.NodeContainerRef{}
	}
	rec.ImpactedContainers = []model.NodeContainerRef(row.ImpactedContainers)
	if rec.ImpactedContainers == nil {
		rec.ImpactedContainers = []model.NodeContainerRef{}
	}
	rec.Classification = dominantClassification(rec.CandidateContainers)
	if includeExplanation {
		rec.Explanation = model.BuildNodeGPUTimeslicingExplanationAPI(
			row.ExplDataDays, row.ExplCandidateCount, row.ExplImpactedCount, row.ExplClassificationRule,
		)
	}
	return rec
}

func nodeGPUTimeslicingSeekSQL(orderCol, orderHow string, cursor NodeGPUCursor, hasSort bool, argIdx int) (string, []interface{}, int, error) {
	tie := "(t.cluster_uuid::text, t.node_name, t.gpu_model, t.term)"
	if hasSort && len(cursor.SortValue) > 0 {
		sortVal, err := decodeCursorSortValue(cursor.SortValue)
		if err != nil {
			return "", nil, argIdx, fmt.Errorf("invalid after parameter: %w", err)
		}
		clause, args := nodeGPUTimeslicingKeysetSeek(orderCol, orderHow, sortVal, cursor)
		clause, args, argIdx = bindSeekClause(clause, args, argIdx)
		return clause, args, argIdx, nil
	}
	clause := tie + " > ($" + strconv.Itoa(argIdx) + ", $" + strconv.Itoa(argIdx+1) + ", $" + strconv.Itoa(argIdx+2) + ", $" + strconv.Itoa(argIdx+3) + ")"
	args := []interface{}{cursor.ClusterUUID, cursor.NodeName, cursor.GPUModel, cursor.Term}
	return clause, args, argIdx + 4, nil
}

func nodeGPUTimeslicingKeysetSeek(orderCol, orderHow string, sortValue interface{}, cursor NodeGPUCursor) (string, []interface{}) {
	sortOp := ">"
	if orderHow == listoptions.OrderDesc {
		sortOp = "<"
	}
	tie := "(t.cluster_uuid::text, t.node_name, t.gpu_model, t.term)"
	clause := fmt.Sprintf("((%s) %s ? OR ((%s) IS NOT DISTINCT FROM ? AND %s > (?, ?, ?, ?)))",
		orderCol, sortOp, orderCol, tie)
	args := []interface{}{sortValue, sortValue, cursor.ClusterUUID, cursor.NodeName, cursor.GPUModel, cursor.Term}
	return clause, args
}

func nodeGPUTimeslicingOrderNulls(orderCol, orderDir string) string {
	if orderDir == listoptions.OrderDesc {
		return orderCol + " DESC NULLS LAST"
	}
	return orderCol + " ASC NULLS LAST"
}

func nodeGPURecsCurrency(ctx context.Context, orgID, clusterFilter string, recs []model.NodeGPURecommendation) string {
	if clusterFilter != "" {
		return fetchClusterCurrency(ctx, orgID, clusterFilter)
	}
	if len(recs) > 0 && recs[0].ClusterUUID != "" {
		return fetchClusterCurrency(ctx, orgID, recs[0].ClusterUUID)
	}
	return costdata.DefaultCurrency
}
