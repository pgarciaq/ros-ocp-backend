package api

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

const gpuSavingsFleetSummaryNote = "GPU savings are computed at API read time and are not included in this fleet summary. Query container GPU recommendations or node GPU endpoints for per-workload dollar estimates."

// FleetSavingsByPlugin breaks down persisted savings by recommendation plugin.
type FleetSavingsByPlugin struct {
	Container money.MoneyAmount `json:"container"`
	GPU       money.MoneyAmount `json:"gpu"`
	Node      money.MoneyAmount `json:"node"`
	PVC       money.MoneyAmount `json:"pvc"`
	Snapshot  money.MoneyAmount `json:"snapshot"`
	VM        money.MoneyAmount `json:"vm"`
}

// FleetClusterSavings aggregates savings for a single cluster.
type FleetClusterSavings struct {
	ClusterUUID             string             `json:"cluster_uuid"`
	ClusterAlias            string             `json:"cluster_alias"`
	EstimatedMonthlySavings money.MoneyAmount `json:"estimated_monthly_savings"`
	HasCostData             bool               `json:"has_cost_data"`
}

// FleetIdleStateSavingsRow is one idle_state group in savings-summary group_by[idle_state] responses.
type FleetIdleStateSavingsRow struct {
	IdleState             string             `json:"idle_state"`
	EstimatedMonthlyWaste money.MoneyAmount `json:"estimated_monthly_waste"`
	ContainerCount        int                `json:"container_count"`
}

// FleetSavingsByIdleMeta is metadata for group_by[idle_state] savings responses.
type FleetSavingsByIdleMeta struct {
	Count int `json:"count"`
}

// FleetSavingsByIdleStateResponse is returned when group_by[idle_state] is requested.
type FleetSavingsByIdleStateResponse struct {
	Data []FleetIdleStateSavingsRow `json:"data"`
	Meta FleetSavingsByIdleMeta     `json:"meta"`
}

// FleetSavingsSummaryResponse is the JSON payload for GET /recommendations/openshift/savings-summary.
type FleetSavingsSummaryResponse struct {
	Currency                string                `json:"currency"`
	EstimatedMonthlySavings money.MoneyAmount   `json:"estimated_monthly_savings"`
	ByCluster               []FleetClusterSavings `json:"by_cluster"`
	ByPlugin                FleetSavingsByPlugin  `json:"by_plugin"`
	GPUSavingsNote          string                `json:"gpu_savings_note,omitempty"`
}

func roundUSD(v float64) float64 {
	return math.Round(v*100) / 100
}

func runHeavyFleetSavingsQuery[T any](ctx context.Context, pool *pgxpool.Pool, fn func(ctx context.Context, q db.QueryRower) (T, error)) (T, error) {
	var zero T
	var result T
	err := db.WithHeavyStatementTimeout(ctx, pool, func(ctx context.Context, q db.QueryRower) error {
		var innerErr error
		result, innerErr = fn(ctx, q)
		return innerErr
	})
	if err != nil {
		return zero, err
	}
	return result, nil
}

// GetFleetSavingsSummary returns aggregated savings across all clusters for the authenticated org.
func GetFleetSavingsSummary(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	engineProfile := queryparams.FirstFilter(c, "engine")
	if engineProfile == "" {
		engineProfile = "cost"
	}
	if engineProfile != "cost" && engineProfile != "performance" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "invalid engine"})
	}

	termProfile := strings.TrimSpace(c.QueryParam("term"))
	if termProfile == "" {
		termProfile = "medium"
	}
	if termProfile != "short" && termProfile != "medium" && termProfile != "long" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "invalid term"})
	}

	responseFormat, formatErr := listoptions.ResolveResponseFormat(c.Request().Header.Get("Accept"), c.QueryParam("format"))
	if formatErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": formatErr.Error()})
	}
	if responseFormat == listoptions.ResponseFormatCSV {
		if queryparams.GroupByIdleState(c) || queryparams.GroupByTagKey(c) != "" {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":  "error",
				"message": "CSV export is not supported with group_by on savings-summary",
			})
		}
	}

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	ctx := c.Request().Context()
	rbacScoped := fleetSummaryNeedsClusterFilter(userPerms)

	if !queryparams.GroupByIdleState(c) && !(queryparams.GroupByTagKey(c) != "" && config.TagsFeatureEnabled()) {
		if cached, ok := fleetsummary.GetSavings(orgID, rbacScoped, userPerms, engineProfile, termProfile); ok {
			resp := fleetSavingsSummaryFromCache(cached)
			setRecommendationNoStore(c)
			if responseFormat == listoptions.ResponseFormatCSV {
				return streamCSV(c, csvFilename("savings-summary"), func(ctx context.Context, w io.Writer) error {
					return generateFleetSavingsSummaryCSV(ctx, w, resp)
				})
			}
			return c.JSON(http.StatusOK, resp)
		}
	}

	var clusterUUIDs []string
	if fleetSummaryNeedsClusterFilter(userPerms) {
		allClusters, qerr := getClustersForOrg(ctx, orgID)
		if qerr != nil {
			hlog.Errorf("fleet savings summary: get clusters failed: %v", qerr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to fetch fleet savings summary",
			})
		}
		clusterUUIDs = filterClustersByRBAC(allClusters, userPerms)
		if len(clusterUUIDs) == 0 {
			emptyCurrency := resolveListCurrencyFromRequest(c, orgID)
			zero := money.FormatCentsToAmount(0, emptyCurrency)
			return c.JSON(http.StatusOK, FleetSavingsSummaryResponse{
				Currency:                emptyCurrency,
				EstimatedMonthlySavings: zero,
				ByCluster:               []FleetClusterSavings{},
				ByPlugin:                fleetSavingsByPluginZeros(emptyCurrency),
				GPUSavingsNote:          gpuSavingsFleetSummaryNote,
			})
		}
	}

	if queryparams.GroupByIdleState(c) {
		clusterQueryFilter := queryparams.FirstFilter(c, "cluster")
		if clusterQueryFilter != "" {
			if len(clusterUUIDs) > 0 {
				allowed := false
				for _, cu := range clusterUUIDs {
					if cu == clusterQueryFilter {
						allowed = true
						break
					}
				}
				if !allowed {
					setRecommendationNoStore(c)
					return c.JSON(http.StatusOK, FleetSavingsByIdleStateResponse{
						Data: []FleetIdleStateSavingsRow{},
						Meta: FleetSavingsByIdleMeta{Count: 0},
					})
				}
			}
			clusterUUIDs = []string{clusterQueryFilter}
		}
		byIdle, qerr := runHeavyFleetSavingsQuery(ctx, pool, func(ctx context.Context, q db.QueryRower) (FleetSavingsByIdleStateResponse, error) {
			return queryFleetSavingsByIdleState(ctx, q, orgID, clusterUUIDs, engineProfile, termProfile)
		})
		if qerr != nil {
			hlog.Errorf("fleet savings by idle_state query failed: %v", qerr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to fetch fleet savings summary",
			})
		}
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, byIdle)
	}

	groupByTagKey := queryparams.GroupByTagKey(c)
	if groupByTagKey != "" && config.TagsFeatureEnabled() {
		clusterQueryFilter := queryparams.FirstFilter(c, "cluster")
		if clusterQueryFilter != "" {
			if len(clusterUUIDs) > 0 {
				allowed := false
				for _, cu := range clusterUUIDs {
					if cu == clusterQueryFilter {
						allowed = true
						break
					}
				}
				if !allowed {
					setRecommendationNoStore(c)
					return c.JSON(http.StatusOK, FleetSavingsByTagResponse{
						Data: []FleetTagSavingsRow{},
						Meta: FleetSavingsByTagMeta{Count: 0},
					})
				}
			}
			clusterUUIDs = []string{clusterQueryFilter}
		}
		namespaceFilter := queryparams.FirstFilter(c, "project")
		byTag, qerr := runHeavyFleetSavingsQuery(ctx, pool, func(ctx context.Context, q db.QueryRower) (FleetSavingsByTagResponse, error) {
			return queryFleetSavingsByTag(ctx, q, fleetSavingsByTagQuery{
				OrgID:           orgID,
				ClusterUUIDs:    clusterUUIDs,
				NamespaceFilter: namespaceFilter,
				EngineProfile:   engineProfile,
				TermProfile:     termProfile,
				TagKey:          groupByTagKey,
			})
		})
		if qerr != nil {
			hlog.Errorf("fleet savings by tag query failed: %v", qerr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to fetch fleet savings summary",
			})
		}
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, byTag)
	}

	storedCurrency := resolveFleetCurrency(ctx, orgID, clusterUUIDs)
	summary, qerr := runHeavyFleetSavingsQuery(ctx, pool, func(ctx context.Context, q db.QueryRower) (FleetSavingsSummaryResponse, error) {
		return queryFleetSavingsSummary(ctx, q, orgID, clusterUUIDs, engineProfile, termProfile, storedCurrency)
	})
	if qerr != nil {
		hlog.Errorf("fleet savings summary query failed: %v", qerr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch fleet savings summary",
		})
	}

	userCurrency := resolveUserCurrency(ctx, orgID)
	rate := fetchExchangeRate(ctx, orgID, storedCurrency, userCurrency)
	displayCurrency := userCurrency
	if rate == 1.0 && storedCurrency != userCurrency {
		displayCurrency = storedCurrency
	}
	convertAndPatchAmount(&summary.EstimatedMonthlySavings, rate, displayCurrency)
	convertAndPatchAmount(&summary.ByPlugin.Container, rate, displayCurrency)
	convertAndPatchAmount(&summary.ByPlugin.GPU, rate, displayCurrency)
	convertAndPatchAmount(&summary.ByPlugin.Node, rate, displayCurrency)
	convertAndPatchAmount(&summary.ByPlugin.PVC, rate, displayCurrency)
	convertAndPatchAmount(&summary.ByPlugin.Snapshot, rate, displayCurrency)
	convertAndPatchAmount(&summary.ByPlugin.VM, rate, displayCurrency)
	for i := range summary.ByCluster {
		convertAndPatchAmount(&summary.ByCluster[i].EstimatedMonthlySavings, rate, displayCurrency)
	}
	summary.Currency = displayCurrency

	summary.GPUSavingsNote = gpuSavingsFleetSummaryNote
	fleetsummary.PutSavings(orgID, rbacScoped, userPerms, engineProfile, termProfile, fleetSavingsSummaryToCache(summary))
	setRecommendationNoStore(c)
	if responseFormat == listoptions.ResponseFormatCSV {
		return streamCSV(c, csvFilename("savings-summary"), func(ctx context.Context, w io.Writer) error {
			return generateFleetSavingsSummaryCSV(ctx, w, summary)
		})
	}
	return c.JSON(http.StatusOK, summary)
}

func fleetSavingsSummaryToCache(summary FleetSavingsSummaryResponse) fleetsummary.CachedSavingsSummary {
	byCluster := make([]fleetsummary.CachedClusterSavings, 0, len(summary.ByCluster))
	for _, row := range summary.ByCluster {
		byCluster = append(byCluster, fleetsummary.CachedClusterSavings{
			ClusterUUID:             row.ClusterUUID,
			ClusterAlias:            row.ClusterAlias,
			EstimatedMonthlySavings: row.EstimatedMonthlySavings,
			HasCostData:             row.HasCostData,
		})
	}
	return fleetsummary.CachedSavingsSummary{
		Currency:                summary.Currency,
		EstimatedMonthlySavings: summary.EstimatedMonthlySavings,
		ByCluster:               byCluster,
		ByPlugin: fleetsummary.CachedSavingsByPlugin{
			Container: summary.ByPlugin.Container,
			GPU:       summary.ByPlugin.GPU,
			Node:      summary.ByPlugin.Node,
			PVC:       summary.ByPlugin.PVC,
			Snapshot:  summary.ByPlugin.Snapshot,
			VM:        summary.ByPlugin.VM,
		},
		GPUSavingsNote: summary.GPUSavingsNote,
	}
}

func fleetSavingsSummaryFromCache(cached fleetsummary.CachedSavingsSummary) FleetSavingsSummaryResponse {
	byCluster := make([]FleetClusterSavings, 0, len(cached.ByCluster))
	for _, row := range cached.ByCluster {
		byCluster = append(byCluster, FleetClusterSavings{
			ClusterUUID:             row.ClusterUUID,
			ClusterAlias:            row.ClusterAlias,
			EstimatedMonthlySavings: row.EstimatedMonthlySavings,
			HasCostData:             row.HasCostData,
		})
	}
	return FleetSavingsSummaryResponse{
		Currency:                cached.Currency,
		EstimatedMonthlySavings: cached.EstimatedMonthlySavings,
		ByCluster:               byCluster,
		ByPlugin: FleetSavingsByPlugin{
			Container: cached.ByPlugin.Container,
			GPU:       cached.ByPlugin.GPU,
			Node:      cached.ByPlugin.Node,
			PVC:       cached.ByPlugin.PVC,
			Snapshot:  cached.ByPlugin.Snapshot,
			VM:        cached.ByPlugin.VM,
		},
		GPUSavingsNote: cached.GPUSavingsNote,
	}
}

func queryFleetSavingsSummary(ctx context.Context, q db.QueryRower, orgID string, clusterUUIDs []string, engineProfile, termProfile, currency string) (FleetSavingsSummaryResponse, error) {
	if currency == "" {
		currency = costdata.DefaultCurrency
	}
	resp := FleetSavingsSummaryResponse{
		Currency:  currency,
		ByCluster: []FleetClusterSavings{},
	}

	byPlugin, totalCents, err := queryFleetSavingsByPlugin(ctx, q, orgID, clusterUUIDs, engineProfile, termProfile, currency)
	if err != nil {
		return resp, err
	}
	resp.ByPlugin = byPlugin
	resp.EstimatedMonthlySavings = money.FormatCentsToAmount(totalCents, currency)

	byCluster, err := queryFleetSavingsByCluster(ctx, q, orgID, clusterUUIDs, engineProfile, termProfile, currency)
	if err != nil {
		return resp, err
	}
	resp.ByCluster = byCluster
	return resp, nil
}

func fleetSavingsByPluginZeros(currency string) FleetSavingsByPlugin {
	zero := money.FormatCentsToAmount(0, currency)
	return FleetSavingsByPlugin{
		Container: zero,
		GPU:       zero,
		Node:      zero,
		PVC:       zero,
		Snapshot:  zero,
		VM:        zero,
	}
}

func queryFleetSavingsByPlugin(ctx context.Context, q db.QueryRower, orgID string, clusterUUIDs []string, engineProfile, termProfile, currency string) (FleetSavingsByPlugin, int64, error) {
	var out FleetSavingsByPlugin
	if currency == "" {
		currency = costdata.DefaultCurrency
	}
	clusterFilter, args, engineParam, termParam, vmTermParam := savingsSummaryQueryArgs(orgID, clusterUUIDs, engineProfile, termProfile)
	engineRef := fmt.Sprintf("$%d", engineParam)
	termRef := fmt.Sprintf("$%d", termParam)
	vmTermRef := fmt.Sprintf("$%d", vmTermParam)

	var containerCents, nodeCents, pvcCents, snapshotCents, vmCents int64
	err := q.QueryRow(ctx, `
		SELECT
			COALESCE((
				SELECT SUM(estimated_savings_cents)
				FROM recommendation_sets
				WHERE org_id = $1 AND term = `+termRef+` AND engine = `+engineRef+` AND stale = false`+clusterFilter+`
			), 0),
			COALESCE((
				SELECT SUM(estimated_savings_cents)
				FROM node_recommendations
				WHERE org_id = $1 AND term = `+termRef+` AND engine = `+engineRef+clusterFilter+`
			), 0),
			COALESCE((
				SELECT SUM(estimated_savings_cents)
				FROM pvc_recommendation_sets
				WHERE org_id = $1 AND term = `+termRef+clusterFilter+`
			), 0),
			COALESCE((
				SELECT SUM(estimated_cost_cents)
				FROM snapshot_recommendation_sets
				WHERE org_id = $1`+clusterFilter+`
			), 0),
			COALESCE((
				SELECT SUM(estimated_savings_cents)
				FROM vm_recommendations
				WHERE org_id = $1 AND term = `+vmTermRef+` AND engine = `+engineRef+clusterFilter+`
			), 0)`,
		args...,
	).Scan(&containerCents, &nodeCents, &pvcCents, &snapshotCents, &vmCents)
	if err != nil {
		return out, 0, err
	}

	zero := money.FormatCentsToAmount(0, currency)
	out = FleetSavingsByPlugin{
		Container: money.FormatCentsToAmount(containerCents, currency),
		GPU:       zero,
		Node:      money.FormatCentsToAmount(nodeCents, currency),
		PVC:       money.FormatCentsToAmount(pvcCents, currency),
		Snapshot:  money.FormatCentsToAmount(snapshotCents, currency),
		VM:        money.FormatCentsToAmount(vmCents, currency),
	}
	totalCents := containerCents + nodeCents + pvcCents + snapshotCents + vmCents
	return out, totalCents, nil
}

func queryFleetSavingsByCluster(ctx context.Context, q db.QueryRower, orgID string, clusterUUIDs []string, engineProfile, termProfile, currency string) ([]FleetClusterSavings, error) {
	if currency == "" {
		currency = costdata.DefaultCurrency
	}
	clusterFilter, args, engineParam, termParam, vmTermParam := savingsSummaryQueryArgs(orgID, clusterUUIDs, engineProfile, termProfile)
	engineRef := fmt.Sprintf("$%d", engineParam)
	termRef := fmt.Sprintf("$%d", termParam)
	vmTermRef := fmt.Sprintf("$%d", vmTermParam)
	noCostCode := fmt.Sprintf("%d", engine.NotifNoCostData)

	rows, err := q.Query(ctx, `
		WITH rec_clusters AS (
			SELECT DISTINCT cluster_uuid::text AS cluster_uuid
			FROM recommendation_sets
			WHERE org_id = $1 AND term = `+termRef+` AND engine = `+engineRef+` AND stale = false`+clusterFilter+`
			UNION
			SELECT DISTINCT cluster_uuid::text
			FROM node_recommendations
			WHERE org_id = $1 AND term = `+termRef+` AND engine = `+engineRef+clusterFilter+`
			UNION
			SELECT DISTINCT cluster_uuid::text
			FROM pvc_recommendation_sets
			WHERE org_id = $1 AND term = `+termRef+clusterFilter+`
			UNION
			SELECT DISTINCT cluster_uuid::text
			FROM snapshot_recommendation_sets
			WHERE org_id = $1`+clusterFilter+`
			UNION
			SELECT DISTINCT cluster_uuid::text
			FROM vm_recommendations
			WHERE org_id = $1 AND term = `+vmTermRef+` AND engine = `+engineRef+clusterFilter+`
		),
		container_savings AS (
			SELECT cluster_uuid::text AS cluster_uuid,
			       COALESCE(SUM(estimated_savings_cents), 0)::float / 100.0 AS savings
			FROM recommendation_sets
			WHERE org_id = $1 AND term = `+termRef+` AND engine = `+engineRef+` AND stale = false`+clusterFilter+`
			GROUP BY cluster_uuid
		),
		node_savings AS (
			SELECT cluster_uuid::text AS cluster_uuid,
			       COALESCE(SUM(estimated_savings_cents), 0)::float / 100.0 AS savings
			FROM node_recommendations
			WHERE org_id = $1 AND term = `+termRef+` AND engine = `+engineRef+clusterFilter+`
			GROUP BY cluster_uuid
		),
		pvc_savings AS (
			SELECT cluster_uuid::text AS cluster_uuid,
			       COALESCE(SUM(estimated_savings_cents), 0)::float / 100.0 AS savings
			FROM pvc_recommendation_sets
			WHERE org_id = $1 AND term = `+termRef+clusterFilter+`
			GROUP BY cluster_uuid
		),
		snapshot_savings AS (
			SELECT cluster_uuid::text AS cluster_uuid,
			       COALESCE(SUM(estimated_cost_cents), 0)::float / 100.0 AS savings
			FROM snapshot_recommendation_sets
			WHERE org_id = $1`+clusterFilter+`
			GROUP BY cluster_uuid
		),
		vm_savings AS (
			SELECT cluster_uuid::text AS cluster_uuid,
			       COALESCE(SUM(estimated_savings_cents), 0)::float / 100.0 AS savings
			FROM vm_recommendations
			WHERE org_id = $1 AND term = `+vmTermRef+` AND engine = `+engineRef+clusterFilter+`
			GROUP BY cluster_uuid
		),
		cost_recs AS (
			SELECT cluster_uuid::text AS cluster_uuid, notification_codes
			FROM recommendation_sets
			WHERE org_id = $1 AND term = `+termRef+` AND engine = `+engineRef+` AND stale = false`+clusterFilter+`
			UNION ALL
			SELECT cluster_uuid::text, notification_codes
			FROM node_recommendations
			WHERE org_id = $1 AND term = `+termRef+` AND engine = `+engineRef+clusterFilter+`
			UNION ALL
			SELECT cluster_uuid::text, notification_codes
			FROM pvc_recommendation_sets
			WHERE org_id = $1 AND term = `+termRef+clusterFilter+`
		),
		cost_data AS (
			SELECT cluster_uuid,
			       COUNT(*) AS total_recs,
			       COUNT(*) FILTER (WHERE notification_codes @> ARRAY[`+noCostCode+`::smallint]) AS no_cost_recs
			FROM cost_recs
			GROUP BY cluster_uuid
		)
		SELECT rc.cluster_uuid,
		       COALESCE(c.cluster_alias, rc.cluster_uuid) AS cluster_alias,
		       COALESCE(cs.savings, 0) + COALESCE(ns.savings, 0) + COALESCE(ps.savings, 0) + COALESCE(ss.savings, 0) + COALESCE(vs.savings, 0) AS savings,
		       COALESCE(cd.total_recs, 0) > 0
		           AND COALESCE(cd.no_cost_recs, 0) < COALESCE(cd.total_recs, 0) AS has_cost_data
		FROM rec_clusters rc
		LEFT JOIN clusters c ON c.cluster_uuid::text = rc.cluster_uuid
		LEFT JOIN rh_accounts a ON a.id = c.tenant_id AND a.org_id = $1
		LEFT JOIN container_savings cs ON cs.cluster_uuid = rc.cluster_uuid
		LEFT JOIN node_savings ns ON ns.cluster_uuid = rc.cluster_uuid
		LEFT JOIN pvc_savings ps ON ps.cluster_uuid = rc.cluster_uuid
		LEFT JOIN snapshot_savings ss ON ss.cluster_uuid = rc.cluster_uuid
		LEFT JOIN vm_savings vs ON vs.cluster_uuid = rc.cluster_uuid
		LEFT JOIN cost_data cd ON cd.cluster_uuid = rc.cluster_uuid`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []FleetClusterSavings
	for rows.Next() {
		var row FleetClusterSavings
		var savingsUSD float64
		if err := rows.Scan(&row.ClusterUUID, &row.ClusterAlias, &savingsUSD, &row.HasCostData); err != nil {
			return nil, err
		}
		row.EstimatedMonthlySavings = money.FormatUSDToAmount(roundUSD(savingsUSD), currency)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].ClusterAlias == result[j].ClusterAlias {
			return result[i].ClusterUUID < result[j].ClusterUUID
		}
		return result[i].ClusterAlias < result[j].ClusterAlias
	})

	if result == nil {
		result = []FleetClusterSavings{}
	}
	return result, nil
}

func savingsSummaryClusterArgs(orgID string, clusterUUIDs []string) (filterSQL string, args []interface{}) {
	return savingsSummaryClusterArgsForColumn(orgID, clusterUUIDs, "cluster_uuid")
}

// savingsSummaryClusterArgsForColumn builds a cluster UUID filter on column (optionally qualified, e.g. "ock.cluster_uuid").
func savingsSummaryClusterArgsForColumn(orgID string, clusterUUIDs []string, column string) (filterSQL string, args []interface{}) {
	args = []interface{}{orgID}
	if len(clusterUUIDs) == 0 {
		return "", args
	}
	args = append(args, clusterUUIDs)
	return ` AND ` + column + `::text = ANY($2::text[])`, args
}

func savingsSummaryQueryArgs(orgID string, clusterUUIDs []string, engineProfile, termProfile string) (filterSQL string, args []interface{}, engineParam, termParam, vmTermParam int) {
	return savingsSummaryQueryArgsForColumn(orgID, clusterUUIDs, engineProfile, termProfile, "cluster_uuid")
}

func savingsSummaryQueryArgsForColumn(orgID string, clusterUUIDs []string, engineProfile, termProfile, clusterColumn string) (filterSQL string, args []interface{}, engineParam, termParam, vmTermParam int) {
	filterSQL, args = savingsSummaryClusterArgsForColumn(orgID, clusterUUIDs, clusterColumn)
	engineParam = len(args) + 1
	args = append(args, engineProfile)
	termParam = len(args) + 1
	args = append(args, termProfile)
	vmTermParam = len(args) + 1
	args = append(args, savingsSummaryVMTerm(termProfile))
	return filterSQL, args, engineParam, termParam, vmTermParam
}

// savingsSummaryVMTerm maps container/node term names to vm_recommendations term values.
func savingsSummaryVMTerm(term string) string {
	switch term {
	case "short":
		return "short_term"
	case "long":
		return "long_term"
	default:
		return "medium_term"
	}
}

func queryFleetSavingsByIdleState(ctx context.Context, q db.QueryRower, orgID string, clusterUUIDs []string, engineProfile, termProfile string) (FleetSavingsByIdleStateResponse, error) {
	resp := FleetSavingsByIdleStateResponse{
		Data: []FleetIdleStateSavingsRow{},
		Meta: FleetSavingsByIdleMeta{Count: 0},
	}
	clusterFilter, args, engineParam, termParam, _ := savingsSummaryQueryArgs(orgID, clusterUUIDs, engineProfile, termProfile)
	engineRef := fmt.Sprintf("$%d", engineParam)
	termRef := fmt.Sprintf("$%d", termParam)

	rows, err := q.Query(ctx, `
		SELECT idle_state,
		       COUNT(DISTINCT (cluster_uuid::text, namespace, workload, container_name))::int AS container_count,
		       COALESCE(SUM(estimated_waste_cents), 0)::float / 100.0 AS waste_usd
		FROM recommendation_sets
		WHERE org_id = $1
		  AND term = `+termRef+`
		  AND engine = `+engineRef+`
		  AND stale = false
		  AND idle_state IN ('idle', 'zombie')`+clusterFilter+`
		GROUP BY idle_state
		ORDER BY idle_state`,
		args...,
	)
	if err != nil {
		return resp, err
	}
	defer rows.Close()

	storedCurrency := resolveFleetCurrency(ctx, orgID, clusterUUIDs)
	for rows.Next() {
		var row FleetIdleStateSavingsRow
		var wasteUSD float64
		if err := rows.Scan(&row.IdleState, &row.ContainerCount, &wasteUSD); err != nil {
			return resp, err
		}
		row.EstimatedMonthlyWaste = money.FormatUSDToAmount(roundUSD(wasteUSD), storedCurrency)
		resp.Data = append(resp.Data, row)
	}
	if err := rows.Err(); err != nil {
		return resp, err
	}

	userCurrency := resolveUserCurrency(ctx, orgID)
	rate := fetchExchangeRate(ctx, orgID, storedCurrency, userCurrency)
	displayCurrency := userCurrency
	if rate == 1.0 && storedCurrency != userCurrency {
		displayCurrency = storedCurrency
	}
	for i := range resp.Data {
		convertAndPatchAmount(&resp.Data[i].EstimatedMonthlyWaste, rate, displayCurrency)
	}

	resp.Meta.Count = len(resp.Data)
	return resp, nil
}

func sampleClusterUUIDFromRecommendations(ctx context.Context, orgID string) string {
	pool := db.GetPool()
	if pool == nil {
		return ""
	}
	var clusterUUID string
	err := pool.QueryRow(ctx, `
		SELECT cluster_uuid::text
		FROM recommendation_sets
		WHERE org_id = $1 AND stale = false
		LIMIT 1`, orgID).Scan(&clusterUUID)
	if err != nil {
		return ""
	}
	return clusterUUID
}

func resolveFleetCurrency(ctx context.Context, orgID string, clusterUUIDs []string) string {
	if len(clusterUUIDs) > 0 {
		return fetchClusterCurrency(ctx, orgID, clusterUUIDs[0])
	}
	clusters, err := getClustersForOrg(ctx, orgID)
	if err == nil && len(clusters) > 0 {
		return fetchClusterCurrency(ctx, orgID, clusters[0])
	}
	if sample := sampleClusterUUIDFromRecommendations(ctx, orgID); sample != "" {
		return fetchClusterCurrency(ctx, orgID, sample)
	}
	return costdata.DefaultCurrency
}
