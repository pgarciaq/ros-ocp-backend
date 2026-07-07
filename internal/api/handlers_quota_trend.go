package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
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

	var entries []model.QuotaTrendEntry
	err = database.WithHeavyStatementTimeout(ctx, pool, func(ctx context.Context, q database.QueryRower) error {
		var innerErr error
		entries, innerErr = model.QueryQuotaTrend(ctx, q, orgID, key.ClusterUUID, key.Namespace, startDate, endDate)
		return innerErr
	})
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
// The MaxLookbackDays cap is enforced only when the user provides explicit dates.
func parseQuotaTrendDateRange(c echo.Context) (time.Time, time.Time, error) {
	now := time.Now().UTC().Truncate(24 * time.Hour)
	defaultStart := now.AddDate(0, 0, -30)

	startDate := defaultStart
	endDate := now
	userProvidedDates := false

	if s := c.QueryParam("start_date"); s != "" {
		parsed, err := time.Parse("2006-01-02", s)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start_date: must be ISO 8601 date (YYYY-MM-DD)")
		}
		startDate = parsed
		userProvidedDates = true
	}

	if s := c.QueryParam("end_date"); s != "" {
		parsed, err := time.Parse("2006-01-02", s)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end_date: must be ISO 8601 date (YYYY-MM-DD)")
		}
		endDate = parsed
		userProvidedDates = true
	}

	if startDate.After(endDate) {
		return time.Time{}, time.Time{}, fmt.Errorf("start_date must not be after end_date")
	}

	const maxSpanDays = 90
	if endDate.Sub(startDate) > time.Duration(maxSpanDays)*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("date range must not exceed %d days", maxSpanDays)
	}

	if userProvidedDates {
		cfg := config.GetConfig()
		if cfg != nil && cfg.MaxLookbackDays > 0 {
			maxRange := time.Duration(cfg.MaxLookbackDays) * 24 * time.Hour
			if endDate.Sub(startDate) > maxRange {
				return time.Time{}, time.Time{}, fmt.Errorf("date range exceeds maximum of %d days", cfg.MaxLookbackDays)
			}
		}
	}

	return startDate, endDate, nil
}
