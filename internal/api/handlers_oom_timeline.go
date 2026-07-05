package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// GetOOMTimeline handles GET /recommendations/openshift/containers/:recommendation-id/oom-timeline.
// Returns per-day OOM kill counts for a container, sparse (only days with events).
func GetOOMTimeline(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	idStr := c.Param("recommendation-id")
	if _, err := uuid.Parse(idStr); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": "bad recommendation-id",
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

	key, err := model.ResolveContainerKeyByID(ctx, pool, orgID, idStr)
	if err != nil {
		hlog.Errorf("GetOOMTimeline: resolve container key: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve container",
		})
	}
	if key == nil {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": "container not found",
		})
	}

	startDate, endDate, err := parseOOMTimelineDateRange(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": err.Error(),
		})
	}

	entries, err := model.QueryOOMTimeline(ctx, pool, orgID, *key, startDate, endDate)
	if err != nil {
		hlog.Errorf("GetOOMTimeline: query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch OOM timeline data",
		})
	}

	var totalCount int64
	for _, e := range entries {
		totalCount += e.OOMKillCount
	}

	resp := model.OOMTimelineResponse{
		Meta: model.OOMTimelineMeta{
			Count:       totalCount,
			ContainerID: idStr,
			StartDate:   startDate.Format("2006-01-02"),
			EndDate:     endDate.Format("2006-01-02"),
		},
		Data: entries,
	}

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, resp)
}

// parseOOMTimelineDateRange extracts optional start_date and end_date query parameters.
// Defaults: start_date = MaxLookbackDays ago (or 6 months if unconfigured), end_date = today.
// The MaxLookbackDays cap is enforced only when the user provides explicit dates.
func parseOOMTimelineDateRange(c echo.Context) (time.Time, time.Time, error) {
	now := time.Now().UTC().Truncate(24 * time.Hour)

	cfg := config.GetConfig()
	maxDays := 0
	if cfg != nil && cfg.MaxLookbackDays > 0 {
		maxDays = cfg.MaxLookbackDays
	}

	defaultStart := now.AddDate(0, -6, 0)
	if maxDays > 0 {
		capped := now.AddDate(0, 0, -maxDays)
		if capped.After(defaultStart) {
			defaultStart = capped
		}
	}

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

	if userProvidedDates && maxDays > 0 {
		maxRange := time.Duration(maxDays) * 24 * time.Hour
		if endDate.Sub(startDate) > maxRange {
			return time.Time{}, time.Time{}, fmt.Errorf("date range exceeds maximum of %d days", maxDays)
		}
	}

	return startDate, endDate, nil
}
