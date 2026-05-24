package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/reship"
)

const businessHoursStorageWarning = "Enabling business hours approximately doubles digest storage for affected scopes."

var (
	validBusinessDays = map[string]struct{}{
		"monday": {}, "tuesday": {}, "wednesday": {}, "thursday": {},
		"friday": {}, "saturday": {}, "sunday": {},
	}
	hhmmPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
)

type businessHoursScheduleBody struct {
	Days      []string `json:"days"`
	StartTime string   `json:"start_time"`
	EndTime   string   `json:"end_time"`
}

type businessHoursPutRequest struct {
	Timezone       string                     `json:"timezone"`
	Schedule       businessHoursScheduleBody  `json:"schedule"`
	OffHoursWeight *float64                   `json:"off_hours_weight"`
	Enabled        *bool                      `json:"enabled"`
}

type businessHoursSettingsResponse struct {
	Timezone         string                     `json:"timezone,omitempty"`
	Schedule         *businessHoursScheduleBody `json:"schedule,omitempty"`
	OffHoursWeight   float64                    `json:"off_hours_weight,omitempty"`
	Enabled          bool                       `json:"enabled"`
	ReshipStatus     string                     `json:"reship_status,omitempty"`
	ReshipStatusSince *time.Time                `json:"reship_status_since,omitempty"`
}

type businessHoursPutResponse struct {
	businessHoursSettingsResponse
	Warnings []string `json:"warnings,omitempty"`
}

// BusinessHoursSettingsHandler serves org/cluster/namespace business-hours settings.
type BusinessHoursSettingsHandler struct {
	Reship reship.Triggerer
}

// NewBusinessHoursSettingsHandler returns a handler with the given reship trigger (noop if nil).
func NewBusinessHoursSettingsHandler(trigger reship.Triggerer) *BusinessHoursSettingsHandler {
	if trigger == nil {
		trigger = &reship.NoopTriggerer{}
	}
	return &BusinessHoursSettingsHandler{Reship: trigger}
}

// RegisterBusinessHoursRoutes wires GET/PUT/DELETE at org, cluster, and namespace scope.
func RegisterBusinessHoursRoutes(v1 *echo.Group, h *BusinessHoursSettingsHandler) {
	if h == nil {
		h = NewBusinessHoursSettingsHandler(nil)
	}
	v1.GET("/recommendations/openshift/settings/business-hours", h.GetOrgDefault)
	v1.PUT("/recommendations/openshift/settings/business-hours", h.PutOrgDefault)
	v1.DELETE("/recommendations/openshift/settings/business-hours", h.DeleteOrgDefault)

	v1.GET("/recommendations/openshift/settings/business-hours/clusters/:cluster_id", h.GetCluster)
	v1.PUT("/recommendations/openshift/settings/business-hours/clusters/:cluster_id", h.PutCluster)
	v1.DELETE("/recommendations/openshift/settings/business-hours/clusters/:cluster_id", h.DeleteCluster)

	v1.GET("/recommendations/openshift/settings/business-hours/clusters/:cluster_id/namespaces/:namespace", h.GetNamespace)
	v1.PUT("/recommendations/openshift/settings/business-hours/clusters/:cluster_id/namespaces/:namespace", h.PutNamespace)
	v1.DELETE("/recommendations/openshift/settings/business-hours/clusters/:cluster_id/namespaces/:namespace", h.DeleteNamespace)
}

func (h *BusinessHoursSettingsHandler) GetOrgDefault(c echo.Context) error {
	return h.getSettings(c, engine.OrgClusterSentinelUUID, "", false, false)
}

func (h *BusinessHoursSettingsHandler) PutOrgDefault(c echo.Context) error {
	return h.putSettings(c, engine.OrgClusterSentinelUUID, "", true)
}

func (h *BusinessHoursSettingsHandler) DeleteOrgDefault(c echo.Context) error {
	return h.deleteSettings(c, engine.OrgClusterSentinelUUID, "", true)
}

func (h *BusinessHoursSettingsHandler) GetCluster(c echo.Context) error {
	clusterUUID, err := h.parseClusterID(c)
	if err != nil {
		return err
	}
	return h.getSettings(c, clusterUUID, "", true, true)
}

func (h *BusinessHoursSettingsHandler) PutCluster(c echo.Context) error {
	clusterUUID, err := h.parseClusterID(c)
	if err != nil {
		return err
	}
	return h.putSettings(c, clusterUUID, "", false)
}

func (h *BusinessHoursSettingsHandler) DeleteCluster(c echo.Context) error {
	clusterUUID, err := h.parseClusterID(c)
	if err != nil {
		return err
	}
	return h.deleteSettings(c, clusterUUID, "", false)
}

func (h *BusinessHoursSettingsHandler) GetNamespace(c echo.Context) error {
	clusterUUID, err := h.parseClusterID(c)
	if err != nil {
		return err
	}
	namespace := c.Param("namespace")
	if namespace == "" {
		return badRequest(c, "namespace is required")
	}
	return h.getSettings(c, clusterUUID, namespace, true, true)
}

func (h *BusinessHoursSettingsHandler) PutNamespace(c echo.Context) error {
	clusterUUID, err := h.parseClusterID(c)
	if err != nil {
		return err
	}
	namespace := c.Param("namespace")
	if namespace == "" {
		return badRequest(c, "namespace is required")
	}
	return h.putSettings(c, clusterUUID, namespace, false)
}

func (h *BusinessHoursSettingsHandler) DeleteNamespace(c echo.Context) error {
	clusterUUID, err := h.parseClusterID(c)
	if err != nil {
		return err
	}
	namespace := c.Param("namespace")
	if namespace == "" {
		return badRequest(c, "namespace is required")
	}
	return h.deleteSettings(c, clusterUUID, namespace, false)
}

func (h *BusinessHoursSettingsHandler) getSettings(c echo.Context, clusterUUID, namespace string, useInheritance, includeReshipStatus bool) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID

	pool := db.GetPool()
	if pool == nil {
		return serviceUnavailable(c, "database connection unavailable")
	}

	ctx := c.Request().Context()

	if clusterUUID != engine.OrgClusterSentinelUUID {
		if err := h.ensureClusterExists(ctx, pool, orgID, clusterUUID); err != nil {
			return err
		}
	}

	if useInheritance {
		cache, err := engine.LoadSchedules(ctx, pool, orgID, clusterUUID)
		if err != nil {
			requestLogger(c, orgID).Errorf("load business hours schedules: %v", err)
			return serviceUnavailable(c, "unable to read business hours settings")
		}
		effective := cache.Resolve(namespace)
		resp := scheduleToResponse(effective)
		if includeReshipStatus {
			enrichReshipStatus(ctx, pool, orgID, clusterUUID, &resp)
		}
		return c.JSON(http.StatusOK, resp)
	}

	row, found, err := loadScheduleRow(ctx, pool, orgID, clusterUUID, namespace)
	if err != nil {
		requestLogger(c, orgID).Errorf("load business hours row: %v", err)
		return serviceUnavailable(c, "unable to read business hours settings")
	}
	if !found {
		resp := businessHoursSettingsResponse{Enabled: false}
		if includeReshipStatus {
			enrichReshipStatus(ctx, pool, orgID, clusterUUID, &resp)
		}
		return c.JSON(http.StatusOK, resp)
	}
	resp := scheduleToResponse(row)
	if includeReshipStatus {
		enrichReshipStatus(ctx, pool, orgID, clusterUUID, &resp)
	}
	return c.JSON(http.StatusOK, resp)
}

func (h *BusinessHoursSettingsHandler) putSettings(c echo.Context, clusterUUID, namespace string, orgLevel bool) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
	}
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	var req businessHoursPutRequest
	body := http.MaxBytesReader(c.Response(), c.Request().Body, 1<<20)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		return badRequest(c, "invalid JSON body")
	}

	sched, err := validateBusinessHoursPut(req)
	if err != nil {
		return badRequest(c, err.Error())
	}

	pool := db.GetPool()
	if pool == nil {
		return serviceUnavailable(c, "database connection unavailable")
	}

	ctx := c.Request().Context()

	if clusterUUID != engine.OrgClusterSentinelUUID {
		if err := h.ensureClusterExists(ctx, pool, orgID, clusterUUID); err != nil {
			return err
		}
	}

	sched.OrgID = orgID
	sched.ClusterUUID = clusterUUID
	sched.Namespace = namespace

	if err := engine.UpsertBusinessHoursSchedule(ctx, pool, sched); err != nil {
		hlog.Errorf("upsert business hours schedule: %v", err)
		return serviceUnavailable(c, "unable to save business hours settings")
	}

	if config.BusinessHoursFeatureEnabled() && !sched.Enabled {
		if namespace != "" {
			if err := engine.PruneNamespaceBusinessHoursDigests(ctx, pool, orgID, clusterUUID, namespace); err != nil {
				hlog.Errorf("prune namespace business hours digests: %v", err)
				return serviceUnavailable(c, "unable to prune business hours digests")
			}
		} else if clusterUUID != engine.OrgClusterSentinelUUID {
			if err := engine.PruneClusterBusinessHoursDigests(ctx, pool, orgID, clusterUUID); err != nil {
				hlog.Errorf("prune cluster business hours digests: %v", err)
				return serviceUnavailable(c, "unable to prune business hours digests")
			}
		}
	}

	clusterIDs, err := h.reshipClusterIDs(ctx, pool, orgID, clusterUUID, orgLevel)
	if err != nil {
		hlog.Errorf("resolve reship clusters: %v", err)
		return serviceUnavailable(c, "unable to trigger re-ingestion")
	}
	if config.BusinessHoursFeatureEnabled() {
		for _, clusterID := range clusterIDs {
			if err := reship.MarkReshipPending(ctx, pool, orgID, clusterID); err != nil {
				hlog.Errorf("mark reship pending: %v", err)
			}
		}
		reship.TriggerAsync(h.Reship, orgID, clusterIDs)
	}

	resp := businessHoursPutResponse{
		businessHoursSettingsResponse: scheduleToResponse(sched),
	}
	if orgLevel {
		resp.Warnings = []string{businessHoursStorageWarning}
	}

	return c.JSON(http.StatusAccepted, resp)
}

func (h *BusinessHoursSettingsHandler) deleteSettings(c echo.Context, clusterUUID, namespace string, orgLevel bool) error {
	if err := requireSettingsWrite(c); err != nil {
		return err
	}
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	pool := db.GetPool()
	if pool == nil {
		return serviceUnavailable(c, "database connection unavailable")
	}

	ctx := c.Request().Context()

	if clusterUUID != engine.OrgClusterSentinelUUID {
		if err := h.ensureClusterExists(ctx, pool, orgID, clusterUUID); err != nil {
			return err
		}
	}

	if err := engine.DeleteBusinessHoursSchedule(ctx, pool, orgID, clusterUUID, namespace); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.JSON(http.StatusNotFound, echo.Map{
				"status":  "error",
				"message": "no business hours override at this level",
			})
		}
		hlog.Errorf("delete business hours schedule: %v", err)
		return serviceUnavailable(c, "unable to delete business hours settings")
	}

	clusterIDs, err := h.reshipClusterIDs(ctx, pool, orgID, clusterUUID, orgLevel)
	if err != nil {
		hlog.Errorf("resolve reship clusters: %v", err)
		return serviceUnavailable(c, "unable to trigger re-ingestion")
	}
	if config.BusinessHoursFeatureEnabled() {
		for _, clusterID := range clusterIDs {
			if err := reship.MarkReshipPending(ctx, pool, orgID, clusterID); err != nil {
				hlog.Errorf("mark reship pending: %v", err)
			}
		}
		reship.TriggerAsync(h.Reship, orgID, clusterIDs)
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *BusinessHoursSettingsHandler) parseClusterID(c echo.Context) (string, error) {
	raw := c.Param("cluster_id")
	if raw == "" {
		return "", badRequest(c, "cluster_id is required")
	}
	if _, err := uuid.Parse(raw); err != nil {
		return "", badRequest(c, "cluster_id must be a valid UUID")
	}
	return raw, nil
}

func (h *BusinessHoursSettingsHandler) ensureClusterExists(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	exists, err := clusterExistsForOrg(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return serviceUnavailableEcho(err)
	}
	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, echo.Map{
			"status":  "error",
			"message": "cluster not found",
		})
	}
	return nil
}

func (h *BusinessHoursSettingsHandler) reshipClusterIDs(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, orgLevel bool) ([]uuid.UUID, error) {
	if orgLevel {
		return listClusterUUIDsForOrg(ctx, pool, orgID)
	}
	id, err := uuid.Parse(clusterUUID)
	if err != nil {
		return nil, err
	}
	return []uuid.UUID{id}, nil
}

func validateBusinessHoursPut(req businessHoursPutRequest) (engine.BusinessHoursSchedule, error) {
	var sched engine.BusinessHoursSchedule

	if req.Timezone == "" {
		return sched, fmt.Errorf("timezone is required")
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		return sched, fmt.Errorf("timezone must be a valid IANA location")
	}
	sched.Timezone = req.Timezone

	if len(req.Schedule.Days) == 0 {
		return sched, fmt.Errorf("schedule.days must not be empty")
	}
	days := make([]string, 0, len(req.Schedule.Days))
	for _, d := range req.Schedule.Days {
		if d != strings.ToLower(d) {
			return sched, fmt.Errorf("schedule.days must use lowercase day names")
		}
		if _, ok := validBusinessDays[d]; !ok {
			return sched, fmt.Errorf("schedule.days contains invalid day name %q", d)
		}
		days = append(days, d)
	}
	sched.Days = days

	if !hhmmPattern.MatchString(req.Schedule.StartTime) {
		return sched, fmt.Errorf("schedule.start_time must be HH:MM (24-hour)")
	}
	if !hhmmPattern.MatchString(req.Schedule.EndTime) {
		return sched, fmt.Errorf("schedule.end_time must be HH:MM (24-hour)")
	}
	if !timeWindowValid(req.Schedule.StartTime, req.Schedule.EndTime) {
		return sched, fmt.Errorf("schedule.end_time must be after schedule.start_time (overnight windows are not supported)")
	}
	sched.StartTime = req.Schedule.StartTime
	sched.EndTime = req.Schedule.EndTime

	weight := 0.0
	if req.OffHoursWeight != nil {
		weight = *req.OffHoursWeight
	}
	if weight < 0.0 || weight > 1.0 {
		return sched, fmt.Errorf("off_hours_weight must be between 0.0 and 1.0")
	}
	sched.OffHoursWeight = weight

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	sched.Enabled = enabled

	return sched, nil
}

func timeWindowValid(start, end string) bool {
	startMin := hhmmToMinutes(start)
	endMin := hhmmToMinutes(end)
	return endMin > startMin
}

func hhmmToMinutes(hhmm string) int {
	var h, m int
	fmt.Sscanf(hhmm, "%d:%d", &h, &m)
	return h*60 + m
}

func scheduleToResponse(sched engine.BusinessHoursSchedule) businessHoursSettingsResponse {
	if !sched.Enabled && sched.Timezone == "" && len(sched.Days) == 0 {
		return businessHoursSettingsResponse{Enabled: false}
	}
	return businessHoursSettingsResponse{
		Timezone:       sched.Timezone,
		OffHoursWeight: sched.OffHoursWeight,
		Enabled:        sched.Enabled,
		Schedule: &businessHoursScheduleBody{
			Days:      sched.Days,
			StartTime: sched.StartTime,
			EndTime:   sched.EndTime,
		},
	}
}

func enrichReshipStatus(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, resp *businessHoursSettingsResponse) {
	clusterID, err := uuid.Parse(clusterUUID)
	if err != nil {
		return
	}
	status, err := reship.GetClusterReshipStatus(ctx, pool, orgID, clusterID)
	if err != nil {
		return
	}
	resp.ReshipStatus = status.Status
	if status.Since != nil {
		ts := status.Since.UTC()
		resp.ReshipStatusSince = &ts
	}
}

func loadScheduleRow(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, namespace string) (engine.BusinessHoursSchedule, bool, error) {
	var sched engine.BusinessHoursSchedule
	err := pool.QueryRow(ctx, `
		SELECT timezone, days,
			to_char(start_time, 'HH24:MI') AS start_time,
			to_char(end_time, 'HH24:MI') AS end_time,
			off_hours_weight, enabled
		FROM business_hours_schedules
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3`,
		orgID, clusterUUID, namespace,
	).Scan(&sched.Timezone, &sched.Days, &sched.StartTime, &sched.EndTime, &sched.OffHoursWeight, &sched.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return sched, false, nil
	}
	if err != nil {
		return sched, false, err
	}
	sched.OrgID = orgID
	sched.ClusterUUID = clusterUUID
	sched.Namespace = namespace
	sched.Days = normalizeDaysForResponse(sched.Days)
	return sched, true, nil
}

func normalizeDaysForResponse(days []string) []string {
	out := make([]string, len(days))
	for i, d := range days {
		out[i] = strings.ToLower(strings.TrimSpace(d))
	}
	return out
}

func clusterExistsForOrg(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM clusters c
			JOIN rh_accounts a ON c.tenant_id = a.id
			WHERE a.org_id = $1 AND c.cluster_uuid = $2::uuid
		)`, orgID, clusterUUID).Scan(&exists)
	return exists, err
}

func listClusterUUIDsForOrg(ctx context.Context, pool *pgxpool.Pool, orgID string) ([]uuid.UUID, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT c.cluster_uuid::text
		FROM clusters c
		JOIN rh_accounts a ON c.tenant_id = a.id
		WHERE a.org_id = $1`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func badRequest(c echo.Context, message string) error {
	return c.JSON(http.StatusBadRequest, echo.Map{
		"status":  "error",
		"message": message,
	})
}

func serviceUnavailable(c echo.Context, message string) error {
	return c.JSON(http.StatusServiceUnavailable, echo.Map{
		"status":  "error",
		"message": message,
	})
}

func serviceUnavailableEcho(err error) error {
	return echo.NewHTTPError(http.StatusServiceUnavailable, echo.Map{
		"status":  "error",
		"message": err.Error(),
	})
}
