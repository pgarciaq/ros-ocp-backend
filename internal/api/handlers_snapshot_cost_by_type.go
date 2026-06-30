package api

import (
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

	query := `
		SELECT recommendation_type,
		       COALESCE(SUM(estimated_cost_cents), 0)::bigint AS total_cost_cents,
		       COUNT(*)::int AS count
		FROM snapshot_recommendation_sets
		WHERE org_id = $1
		GROUP BY recommendation_type
		ORDER BY total_cost_cents DESC`

	rows, err := pool.Query(ctx, query, orgID)
	if err != nil {
		hlog.Errorf("snapshot cost-by-type query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch snapshot cost by type",
		})
	}
	defer rows.Close()

	var data []SnapshotCostByTypeItem
	for rows.Next() {
		var item SnapshotCostByTypeItem
		if scanErr := rows.Scan(&item.RecommendationType, &item.TotalCostCents, &item.Count); scanErr != nil {
			hlog.Errorf("scanning snapshot cost-by-type row: %v", scanErr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read snapshot cost by type",
			})
		}
		data = append(data, item)
	}
	if err := rows.Err(); err != nil {
		hlog.Errorf("snapshot cost-by-type row iteration failed: %v", err)
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
