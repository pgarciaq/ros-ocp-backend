package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// GetQuotaTrend handles GET /recommendations/openshift/quota/:quota-id/trend.
// Returns per-day quota hard vs used values for CPU request and memory request.
func GetQuotaTrend(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	idStr := c.Param("quota-id")
	if _, err := uuid.Parse(idStr); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": "bad quota-id",
		})
	}

	pool := database.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	ctx := c.Request().Context()

	key, err := model.ResolveQuotaKeyByID(ctx, pool, orgID, idStr)
	if err != nil {
		hlog.Errorf("GetQuotaTrend: resolve quota key: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve quota recommendation",
		})
	}
	if key == nil {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": "quota recommendation not found",
		})
	}

	startDate, endDate, err := parseQuotaTrendDateRange(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": err.Error(),
		})
	}

	entries, err := model.QueryQuotaTrend(ctx, pool, orgID, key.ClusterUUID, key.Namespace, startDate, endDate)
	if err != nil {
		hlog.Errorf("GetQuotaTrend: query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch quota trend data",
		})
	}

	resp := model.QuotaTrendResponse{
		Meta: model.QuotaTrendMeta{
			Count:       len(entries),
			ClusterUUID: key.ClusterUUID,
			Namespace:   key.Namespace,
			StartDate:   startDate.Format("2006-01-02"),
			EndDate:     endDate.Format("2006-01-02"),
		},
		Data: entries,
	}

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, resp)
}

// parseQuotaTrendDateRange extracts optional start_date and end_date query parameters.
// Defaults: start_date = 30 days ago, end_date = today.
func parseQuotaTrendDateRange(c echo.Context) (time.Time, time.Time, error) {
	now := time.Now().UTC().Truncate(24 * time.Hour)
	defaultStart := now.AddDate(0, 0, -30)

	startDate := defaultStart
	endDate := now

	if s := c.QueryParam("start_date"); s != "" {
		parsed, err := time.Parse("2006-01-02", s)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start_date: must be ISO 8601 date (YYYY-MM-DD)")
		}
		startDate = parsed
	}

	if s := c.QueryParam("end_date"); s != "" {
		parsed, err := time.Parse("2006-01-02", s)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end_date: must be ISO 8601 date (YYYY-MM-DD)")
		}
		endDate = parsed
	}

	if startDate.After(endDate) {
		return time.Time{}, time.Time{}, fmt.Errorf("start_date must not be after end_date")
	}

	return startDate, endDate, nil
}
