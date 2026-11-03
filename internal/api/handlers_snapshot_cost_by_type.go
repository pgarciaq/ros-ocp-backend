package api

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
)

// SnapshotCostByTypeItem represents a single recommendation type with aggregated cost and count.
type SnapshotCostByTypeItem struct {
	RecommendationType string `json:"recommendation_type"`
	TotalCostCents     int64  `json:"total_cost_cents"`
	Count              int    `json:"count"`
}

// SnapshotCostByTypeResponse is the top-level response for the cost-by-type endpoint.
type SnapshotCostByTypeResponse struct {
	Data []SnapshotCostByTypeItem `json:"data"`
}

// GetSnapshotCostByType handles GET /recommendations/openshift/snapshots/cost-by-type.
// Returns snapshot costs grouped by recommendation_type.
func GetSnapshotCostByType(c echo.Context) error {
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

	ctx := c.Request().Context()

	// #753 review: this route previously bypassed every RBAC data helper —
	// scope it to the caller's allowed clusters.
	allowedClusters, scoped, scopeErr := dataClusterScope(ctx, orgID, get_user_permissions(c))
	if scopeErr != nil {
		hlog.Errorf("snapshot cost-by-type: cluster scope failed: %v", scopeErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	if scoped && len(allowedClusters) == 0 {
		return c.JSON(http.StatusOK, SnapshotCostByTypeResponse{Data: []SnapshotCostByTypeItem{}})
	}

	query := `
		SELECT recommendation_type,
		       COALESCE(SUM(estimated_cost_cents), 0)::bigint AS total_cost_cents,
		       COUNT(*)::int AS count
		FROM snapshot_recommendation_sets
		WHERE org_id = $1`
	args := []interface{}{orgID}
	if scoped {
		query += ` AND cluster_uuid::text = ANY($2)`
		args = append(args, allowedClusters)
	}
	query += `
		GROUP BY recommendation_type
		ORDER BY total_cost_cents DESC`

	var data []SnapshotCostByTypeItem
	err = db.WithHeavyStatementTimeout(ctx, pool, func(ctx context.Context, q db.QueryRower) error {
		rows, qErr := q.Query(ctx, query, args...)
		if qErr != nil {
			return qErr
		}
		defer rows.Close()

		for rows.Next() {
			var item SnapshotCostByTypeItem
			if scanErr := rows.Scan(&item.RecommendationType, &item.TotalCostCents, &item.Count); scanErr != nil {
				return scanErr
			}
			data = append(data, item)
		}
		return rows.Err()
	})
	if err != nil {
		if db.IsStatementTimeoutCancellation(err) {
			db.RecordStatementTimeoutCancellation(err)
			hlog.Warnf("snapshot cost-by-type query timed out: %v", err)
		} else {
			hlog.Errorf("snapshot cost-by-type query failed: %v", err)
		}
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch snapshot cost by type",
		})
	}

	if data == nil {
		data = []SnapshotCostByTypeItem{}
	}

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, SnapshotCostByTypeResponse{
		Data: data,
	})
}
