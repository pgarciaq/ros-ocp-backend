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
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

const gpuSavingsFleetSummaryNote = "GPU savings are computed at API read time and are not included in this fleet summary. Query container GPU recommendations or node GPU endpoints for per-workload dollar estimates."

// FleetSavingsByPlugin breaks down persisted savings by recommendation plugin.
type FleetSavingsByPlugin struct {
	Container float64 `json:"container"`
	GPU       float64 `json:"gpu"`
	Node      float64 `json:"node"`
	PVC       float64 `json:"pvc"`
	Snapshot  float64 `json:"snapshot"`
	VM        float64 `json:"vm"`
}

// FleetClusterSavings aggregates savings for a single cluster.
type FleetClusterSavings struct {
	ClusterUUID             string             `json:"cluster_uuid"`
	ClusterAlias            string             `json:"cluster_alias"`
	EstimatedMonthlySavings money.SavingsObject `json:"estimated_monthly_savings"`
	HasCostData             bool               `json:"has_cost_data"`
}

// FleetIdleStateSavingsRow is one idle_state group in savings-summary group_by[idle_state] responses.
type FleetIdleStateSavingsRow struct {
	IdleState             string             `json:"idle_state"`
	EstimatedMonthlyWaste money.SavingsObject `json:"estimated_monthly_waste"`
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
	EstimatedMonthlySavings money.SavingsObject   `json:"estimated_monthly_savings"`
	ByCluster               []FleetClusterSavings `json:"by_cluster"`
	ByPlugin                FleetSavingsByPlugin  `json:"by_plugin"`
	GPUSavingsNote          string                `json:"gpu_savings_note,omitempty"`
}

func roundUSD(v float64) float64 {
	return math.Round(v*100) / 100
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
			return c.JSON(http.StatusOK, FleetSavingsSummaryResponse{
				Currency:                costdata.DefaultCurrency,
				EstimatedMonthlySavings: money.FormatUSDToSavings(0, costdata.DefaultCurrency),
				ByCluster:               []FleetClusterSavings{},
				ByPlugin:                FleetSavingsByPlugin{},
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
		byIdle, qerr := queryFleetSavingsByIdleState(ctx, pool, orgID, clusterUUIDs, engineProfile, termProfile)
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
		byTag, qerr := queryFleetSavingsByTag(ctx, pool, fleetSavingsByTagQuery{
			OrgID:           orgID,
			ClusterUUIDs:    clusterUUIDs,
			NamespaceFilter: namespaceFilter,
			EngineProfile:   engineProfile,
			TermProfile:     termProfile,
			TagKey:          groupByTagKey,
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

	currency := resolveFleetCurrency(ctx, orgID, clusterUUIDs)
	summary, qerr := queryFleetSavingsSummary(ctx, pool, orgID, clusterUUIDs, engineProfile, termProfile, currency)
	if qerr != nil {
		hlog.Errorf("fleet savings summary query failed: %v", qerr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch fleet savings summary",
		})
	}

	summary.GPUSavingsNote = gpuSavingsFleetSummaryNote
	setRecommendationNoStore(c)
	if responseFormat == listoptions.ResponseFormatCSV {
		return streamCSV(c, csvFilename("savings-summary"), func(ctx context.Context, w io.Writer) error {
			return generateFleetSavingsSummaryCSV(ctx, w, summary)
		})
	}
	return c.JSON(http.StatusOK, summary)
}

func queryFleetSavingsSummary(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUIDs []string, engineProfile, termProfile, currency string) (FleetSavingsSummaryResponse, error) {
	if currency == "" {
		currency = costdata.DefaultCurrency
	}
	resp := FleetSavingsSummaryResponse{
		Currency:  currency,
		ByCluster: []FleetClusterSavings{},
	}

	byPlugin, err := queryFleetSavingsByPlugin(ctx, pool, orgID, clusterUUIDs, engineProfile, termProfile)
	if err != nil {
		return resp, err
	}
	resp.ByPlugin = byPlugin
	totalUSD := roundUSD(
		byPlugin.Container + byPlugin.Node + byPlugin.PVC + byPlugin.Snapshot + byPlugin.VM,
	)
	resp.EstimatedMonthlySavings = money.FormatUSDToSavings(totalUSD, currency)

	byCluster, err := queryFleetSavingsByCluster(ctx, pool, orgID, clusterUUIDs, engineProfile, termProfile, currency)
	if err != nil {
		return resp, err
	}
	resp.ByCluster = byCluster
	return resp, nil
}

func queryFleetSavingsByPlugin(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUIDs []string, engineProfile, termProfile string) (FleetSavingsByPlugin, error) {
	var out FleetSavingsByPlugin
	clusterFilter, args, engineParam, termParam := savingsSummaryQueryArgs(orgID, clusterUUIDs, engineProfile, termProfile)
	engineRef := fmt.Sprintf("$%d", engineParam)
	termRef := fmt.Sprintf("$%d", termParam)
	vmTerm := savingsSummaryVMTerm(termProfile)

	err := pool.QueryRow(ctx, `
		SELECT
			COALESCE((
				SELECT SUM(estimated_monthly_savings_usd)::float / 100.0
				FROM recommendation_sets
				WHERE org_id = $1 AND term = `+termRef+` AND engine = `+engineRef+` AND stale = false`+clusterFilter+`
			), 0),
			COALESCE((
				SELECT SUM(estimated_monthly_savings_usd)::float / 100.0
				FROM node_recommendations
				WHERE org_id = $1 AND term = `+termRef+` AND engine = `+engineRef+clusterFilter+`
			), 0),
			COALESCE((
				SELECT SUM(estimated_monthly_savings_usd)::float / 100.0
				FROM pvc_recommendation_sets
				WHERE org_id = $1 AND term = `+termRef+clusterFilter+`
			), 0),
			COALESCE((
				SELECT SUM(estimated_monthly_cost_usd)
				FROM snapshot_recommendation_sets
				WHERE org_id = $1`+clusterFilter+`
			), 0),
			COALESCE((
				SELECT SUM(savings_amount)
				FROM vm_recommendations
				WHERE org_id = $1 AND term = '`+vmTerm+`' AND engine = `+engineRef+clusterFilter+`
			), 0)`,
		args...,
	).Scan(&out.Container, &out.Node, &out.PVC, &out.Snapshot, &out.VM)
	if err != nil {
		return out, err
	}

	out.Container = roundUSD(out.Container)
	out.Node = roundUSD(out.Node)
	out.PVC = roundUSD(out.PVC)
	out.Snapshot = roundUSD(out.Snapshot)
	out.VM = roundUSD(out.VM)
	return out, nil
}

func queryFleetSavingsByCluster(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUIDs []string, engineProfile, termProfile, currency string) ([]FleetClusterSavings, error) {
	if currency == "" {
		currency = costdata.DefaultCurrency
	}
	clusterFilter, args, engineParam, termParam := savingsSummaryQueryArgs(orgID, clusterUUIDs, engineProfile, termProfile)
	engineRef := fmt.Sprintf("$%d", engineParam)
	termRef := fmt.Sprintf("$%d", termParam)
	vmTerm := savingsSummaryVMTerm(termProfile)
	noCostCode := fmt.Sprintf("%d", engine.NotifNoCostData)

	rows, err := pool.Query(ctx, `
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
			WHERE org_id = $1 AND term = '`+vmTerm+`' AND engine = `+engineRef+clusterFilter+`
		),
		container_savings AS (
			SELECT cluster_uuid::text AS cluster_uuid,
			       COALESCE(SUM(estimated_monthly_savings_usd), 0)::float / 100.0 AS savings
			FROM recommendation_sets
			WHERE org_id = $1 AND term = `+termRef+` AND engine = `+engineRef+` AND stale = false`+clusterFilter+`
			GROUP BY cluster_uuid
		),
		node_savings AS (
			SELECT cluster_uuid::text AS cluster_uuid,
			       COALESCE(SUM(estimated_monthly_savings_usd), 0)::float / 100.0 AS savings
			FROM node_recommendations
			WHERE org_id = $1 AND term = `+termRef+` AND engine = `+engineRef+clusterFilter+`
			GROUP BY cluster_uuid
		),
		pvc_savings AS (
			SELECT cluster_uuid::text AS cluster_uuid,
			       COALESCE(SUM(estimated_monthly_savings_usd), 0)::float / 100.0 AS savings
			FROM pvc_recommendation_sets
			WHERE org_id = $1 AND term = `+termRef+clusterFilter+`
			GROUP BY cluster_uuid
		),
		snapshot_savings AS (
			SELECT cluster_uuid::text AS cluster_uuid,
			       COALESCE(SUM(estimated_monthly_cost_usd), 0) AS savings
			FROM snapshot_recommendation_sets
			WHERE org_id = $1`+clusterFilter+`
			GROUP BY cluster_uuid
		),
		vm_savings AS (
			SELECT cluster_uuid::text AS cluster_uuid,
			       COALESCE(SUM(savings_amount), 0) AS savings
			FROM vm_recommendations
			WHERE org_id = $1 AND term = '`+vmTerm+`' AND engine = `+engineRef+clusterFilter+`
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
		row.EstimatedMonthlySavings = money.FormatUSDToSavings(roundUSD(savingsUSD), currency)
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

func savingsSummaryQueryArgs(orgID string, clusterUUIDs []string, engineProfile, termProfile string) (filterSQL string, args []interface{}, engineParam, termParam int) {
	return savingsSummaryQueryArgsForColumn(orgID, clusterUUIDs, engineProfile, termProfile, "cluster_uuid")
}

func savingsSummaryQueryArgsForColumn(orgID string, clusterUUIDs []string, engineProfile, termProfile, clusterColumn string) (filterSQL string, args []interface{}, engineParam, termParam int) {
	filterSQL, args = savingsSummaryClusterArgsForColumn(orgID, clusterUUIDs, clusterColumn)
	engineParam = len(args) + 1
	args = append(args, engineProfile)
	termParam = len(args) + 1
	args = append(args, termProfile)
	return filterSQL, args, engineParam, termParam
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

func queryFleetSavingsByIdleState(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUIDs []string, engineProfile, termProfile string) (FleetSavingsByIdleStateResponse, error) {
	resp := FleetSavingsByIdleStateResponse{
		Data: []FleetIdleStateSavingsRow{},
		Meta: FleetSavingsByIdleMeta{Count: 0},
	}
	clusterFilter, args, engineParam, termParam := savingsSummaryQueryArgs(orgID, clusterUUIDs, engineProfile, termProfile)
	engineRef := fmt.Sprintf("$%d", engineParam)
	termRef := fmt.Sprintf("$%d", termParam)

	rows, err := pool.Query(ctx, `
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

	currency := resolveFleetCurrency(ctx, orgID, clusterUUIDs)
	for rows.Next() {
		var row FleetIdleStateSavingsRow
		var wasteUSD float64
		if err := rows.Scan(&row.IdleState, &row.ContainerCount, &wasteUSD); err != nil {
			return resp, err
		}
		row.EstimatedMonthlyWaste = money.FormatUSDToSavings(roundUSD(wasteUSD), currency)
		resp.Data = append(resp.Data, row)
	}
	if err := rows.Err(); err != nil {
		return resp, err
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
