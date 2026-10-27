package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	hcpcapacity "github.com/redhatinsights/ros-ocp-backend/librobne/hcp"
)

// W4 fleet-capacity serving surface (#392, ADR-0334 amendment).
//
// GET /recommendations/openshift/hcp/fleet-capacity — latest snapshot per
// authorized cluster (the UI card-list discovery surface); the cluster
// filter is optional and narrows to exactly that cluster.
// GET /recommendations/openshift/hcp/fleet-capacity/history — ordered
// per-cluster trend; the single-cluster filter is required (GPU
// time-slicing/VM history pattern).
//
// Authorization: org identity binds org_id; cluster RBAC runs before any
// data read via filterClustersByRBAC over getClustersForOrg (the
// established pool-surface mechanism — same openshift.cluster permission
// semantics as rbac.AddRBACFilter, which is gorm-qualified to
// recommendation_sets and cannot scope this table). Unauthorized clusters
// are non-disclosing: 200-empty, never 403/404.
//
// Serve-time derivation (not persisted): headroom/status/staleness/
// cautions come from the stored row. Affirmative pressure withholds exact
// headroom; unknown pressure stays an explicit warning (ADR-0334 rule 8).

type hcpCapacitySourceVersions struct {
	Operator string `json:"operator"`
	OCP      string `json:"ocp"`
	MCE      string `json:"mce"`
}

type hcpCapacityEligibleWorkers struct {
	CPUAllocatableMillicores int64 `json:"cpu_allocatable_millicores"`
	MemoryAllocatableBytes   int64 `json:"memory_allocatable_bytes"`
	MaxPods                  int64 `json:"max_pods"`
}

type hcpCapacityScenarioResponse struct {
	Name             string `json:"name"`
	MaxHCs           int64  `json:"max_hcs"`
	LimitingResource string `json:"limiting_resource"`
	Headroom         *int64 `json:"headroom"`
	Status           string `json:"status"`
}

type hcpCapacityWarnings struct {
	Stale                bool  `json:"stale"`
	BelowVersionFloor    bool  `json:"below_version_floor"`
	Pressured            bool  `json:"pressured"`
	PressureUnknown      bool  `json:"pressure_unknown"`
	MCEMayBeStale        *bool `json:"mce_may_be_stale,omitempty"`
	SharedInfrastructure bool  `json:"shared_infrastructure"`
}

type hcpCapacityRowResponse struct {
	ClusterUUID               string                        `json:"cluster_uuid"`
	ObservedAt                time.Time                     `json:"observed_at"`
	MCESampledAt              *time.Time                    `json:"mce_sampled_at"`
	NativeModelVersion        string                        `json:"native_model_version"`
	SourceVersions            hcpCapacitySourceVersions     `json:"source_versions"`
	CollectionIntervalMinutes int64                         `json:"collection_interval_minutes"`
	UploadCycleMinutes        int64                         `json:"upload_cycle_minutes"`
	SampleAgeSeconds          int64                         `json:"sample_age_seconds"`
	InventoryComplete         bool                          `json:"inventory_complete"`
	WorkerPlacementKnown      bool                          `json:"worker_placement_known"`
	CurrentHCCount            int64                         `json:"current_hc_count"`
	ReadyHCPCount             int64                         `json:"ready_hcp_count"`
	AverageReadyHCPQPS        *float64                      `json:"average_ready_hcp_qps"`
	ReadyHCPQPSKnown          bool                          `json:"ready_hcp_qps_known"`
	HostedAvailabilityBasis   string                        `json:"hosted_availability_basis"`
	PressureState             string                        `json:"pressure_state"`
	EligibleWorkers           hcpCapacityEligibleWorkers    `json:"eligible_workers"`
	Scenarios                 []hcpCapacityScenarioResponse `json:"scenarios"`
	MCEReferences             map[string]*int64             `json:"mce_references"`
	Warnings                  hcpCapacityWarnings           `json:"warnings"`
}

// hcpCapacityClusterParam accepts the bare cluster_uuid query param plus
// the filter[cluster_uuid] and filter[cluster] spellings — all resolve
// identically (existing alias machinery, VM-history precedent).
func hcpCapacityClusterParam(c echo.Context) string {
	if v := strings.TrimSpace(c.QueryParam("cluster_uuid")); v != "" {
		return v
	}
	if v := strings.TrimSpace(queryparams.FirstFilter(c, "cluster_uuid")); v != "" {
		return v
	}
	return strings.TrimSpace(queryparams.FirstFilter(c, "cluster"))
}

// buildHCPCapacityRow derives the serving row from the stored snapshot
// via the shared librobne/hcp rules (see snapshotToServeObservation and
// serve.go): headroom nil rules, scenario status, and the warning set
// are single-sourced with the robne CLI (#700); the characterization
// goldens pin this handler's output across the refactor.
func buildHCPCapacityRow(s model.HCPCapacitySnapshot, now time.Time, mceMayBeStale *bool) hcpCapacityRowResponse {
	age := now.Sub(s.ObservedAt)
	if age < 0 {
		age = 0
	}
	obs := snapshotToServeObservation(s)
	warnings := hcpcapacity.DeriveWarnings(obs, now, mceMayBeStale)
	status := hcpcapacity.ScenarioStatus(obs.Basis)

	row := hcpCapacityRowResponse{
		ClusterUUID:               s.ClusterUUID,
		ObservedAt:                s.ObservedAt.UTC(),
		MCESampledAt:              nil,
		NativeModelVersion:        s.NativeModelVersion,
		SourceVersions:            hcpCapacitySourceVersions{Operator: s.OperatorVersion, OCP: s.OCPVersion, MCE: s.MCEVersion},
		CollectionIntervalMinutes: s.CollectionIntervalMinutes,
		UploadCycleMinutes:        s.UploadCycleMinutes,
		SampleAgeSeconds:          int64(age.Seconds()),
		InventoryComplete:         s.InventoryComplete,
		WorkerPlacementKnown:      s.WorkerPlacementKnown,
		CurrentHCCount:            s.CurrentHCCount,
		ReadyHCPCount:             s.ReadyHCPCount,
		AverageReadyHCPQPS:        s.AverageReadyHCPQPS,
		ReadyHCPQPSKnown:          s.ReadyHCPQPSKnown,
		HostedAvailabilityBasis:   s.HostedAvailabilityBasis,
		PressureState:             s.PressureState,
		EligibleWorkers: hcpCapacityEligibleWorkers{
			CPUAllocatableMillicores: s.EligibleCPUMillicores,
			MemoryAllocatableBytes:   s.EligibleMemoryBytes,
			MaxPods:                  s.EligibleMaxPods,
		},
		Scenarios: make([]hcpCapacityScenarioResponse, 0, 5),
		MCEReferences: map[string]*int64{
			hcpcapacity.ScenarioRequest:    s.MCERequestMaxHCs,
			hcpcapacity.ScenarioLowQPS:     s.MCELowQPSMaxHCs,
			hcpcapacity.ScenarioAverageQPS: s.MCEAverageQPSMaxHCs,
			hcpcapacity.ScenarioMediumQPS:  s.MCEMediumQPSMaxHCs,
			hcpcapacity.ScenarioHighQPS:    s.MCEHighQPSMaxHCs,
		},
		Warnings: hcpCapacityWarnings{
			Stale:                warnings.Stale,
			BelowVersionFloor:    warnings.BelowVersionFloor,
			Pressured:            warnings.Pressured,
			PressureUnknown:      warnings.PressureUnknown,
			MCEMayBeStale:        warnings.MCEMayBeStale,
			SharedInfrastructure: warnings.SharedInfrastructure,
		},
	}
	if s.MCESampledAt != nil {
		utc := s.MCESampledAt.UTC()
		row.MCESampledAt = &utc
	}

	scenarioCols := []struct {
		name  string
		max   int64
		limit string
	}{
		{hcpcapacity.ScenarioRequest, s.RequestMaxHCs, s.RequestLimitingResource},
		{hcpcapacity.ScenarioLowQPS, s.LowQPSMaxHCs, s.LowQPSLimitingResource},
		{hcpcapacity.ScenarioAverageQPS, s.AverageQPSMaxHCs, s.AverageQPSLimitingResource},
		{hcpcapacity.ScenarioMediumQPS, s.MediumQPSMaxHCs, s.MediumQPSLimitingResource},
		{hcpcapacity.ScenarioHighQPS, s.HighQPSMaxHCs, s.HighQPSLimitingResource},
	}
	for _, col := range scenarioCols {
		sc := hcpCapacityScenarioResponse{
			Name:             col.name,
			MaxHCs:           col.max,
			LimitingResource: col.limit,
			Status:           status,
		}
		if h := hcpcapacity.DeriveHeadroom(obs, col.name, col.max, col.limit); h != nil {
			sc.Headroom = h
		}
		row.Scenarios = append(row.Scenarios, sc)
	}
	return row
}

// hcpCapacityAllowedClusters resolves the org's clusters intersected with
// the user's openshift.cluster read grants, before any data read.
func hcpCapacityAllowedClusters(ctx context.Context, orgID string, userPerms map[string][]string) ([]string, error) {
	all, err := getClustersForOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return filterClustersByRBAC(all, userPerms), nil
}

func hcpCapacityErr(c echo.Context, status int, msg string) error {
	return c.JSON(status, echo.Map{"status": "error", "message": msg})
}

// snapshotToServeObservation maps the DB snapshot into the shared
// derivation view (trivial field copies; the rules live in
// librobne/hcp/serve.go). Scenarios ride in the stable contract order.
func snapshotToServeObservation(s model.HCPCapacitySnapshot) hcpcapacity.ServeObservation {
	return hcpcapacity.ServeObservation{
		ObservedAt:           s.ObservedAt,
		MCESampledAt:         s.MCESampledAt,
		OperatorVersion:      s.OperatorVersion,
		OCPVersion:           s.OCPVersion,
		MCEVersion:           s.MCEVersion,
		NativeModelVersion:   s.NativeModelVersion,
		CollectionMinutes:    s.CollectionIntervalMinutes,
		UploadMinutes:        s.UploadCycleMinutes,
		InventoryComplete:    s.InventoryComplete,
		QPSKnown:             s.ReadyHCPQPSKnown,
		QPS:                  s.AverageReadyHCPQPS,
		CurrentHC:            s.CurrentHCCount,
		ReadyHC:              s.ReadyHCPCount,
		Basis:                s.HostedAvailabilityBasis,
		Pressure:             s.PressureState,
		SharedInfrastructure: s.SharedInfrastructure,
		Workers: hcpcapacity.WorkerCapacity{
			CPUAllocatableMillicores: s.EligibleCPUMillicores,
			MemoryAllocatableBytes:   s.EligibleMemoryBytes,
			MaxPods:                  s.EligibleMaxPods,
			PlacementKnown:           s.WorkerPlacementKnown,
		},
		MCERequest: s.MCERequestMaxHCs,
		MCELow:     s.MCELowQPSMaxHCs,
		MCEAverage: s.MCEAverageQPSMaxHCs,
		MCEMedium:  s.MCEMediumQPSMaxHCs,
		MCEHigh:    s.MCEHighQPSMaxHCs,
		Scenarios: []hcpcapacity.CapacityScenario{
			{Name: hcpcapacity.ScenarioRequest, MaxHCs: s.RequestMaxHCs, LimitingResource: s.RequestLimitingResource},
			{Name: hcpcapacity.ScenarioLowQPS, MaxHCs: s.LowQPSMaxHCs, LimitingResource: s.LowQPSLimitingResource},
			{Name: hcpcapacity.ScenarioAverageQPS, MaxHCs: s.AverageQPSMaxHCs, LimitingResource: s.AverageQPSLimitingResource},
			{Name: hcpcapacity.ScenarioMediumQPS, MaxHCs: s.MediumQPSMaxHCs, LimitingResource: s.MediumQPSLimitingResource},
			{Name: hcpcapacity.ScenarioHighQPS, MaxHCs: s.HighQPSMaxHCs, LimitingResource: s.HighQPSLimitingResource},
		},
	}
}

// hcpCapacityMCEMayBeStale implements the frozen-reference caution via
// the shared series rule: fetch the window rows, map, reverse to
// newest-first, delegate. Degrades to false on query error — the caution
// must never fail the read.
func hcpCapacityMCEMayBeStale(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, latest model.HCPCapacitySnapshot) bool {
	obs := snapshotToServeObservation(latest)
	window := hcpcapacity.FreshnessWindow(obs.CollectionMinutes, obs.UploadMinutes)
	rows, err := model.GetHCPCapacitySince(ctx, pool, orgID, clusterUUID, latest.ObservedAt.Add(-window))
	if err != nil || len(rows) < 2 {
		return false
	}
	series := make([]hcpcapacity.ServeObservation, 0, len(rows))
	for _, r := range rows {
		series = append(series, snapshotToServeObservation(r))
	}
	// DB rows are ascending; the series rule wants newest-first.
	for i, j := 0, len(series)-1; i < j; i, j = i+1, j-1 {
		series[i], series[j] = series[j], series[i]
	}
	return hcpcapacity.MCEMayBeStaleFromSeries(series)
}

// GetHCPCapacityLatest handles GET /recommendations/openshift/hcp/fleet-capacity.
func GetHCPCapacityLatest(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)
	ctx := c.Request().Context()

	clusterParam := hcpCapacityClusterParam(c)
	if clusterParam != "" {
		if _, err := uuid.Parse(clusterParam); err != nil {
			return hcpCapacityErr(c, http.StatusBadRequest, "invalid cluster_uuid")
		}
	}

	pool := database.GetPool()
	if pool == nil {
		return hcpCapacityErr(c, http.StatusServiceUnavailable, "database connection unavailable")
	}
	allowed, err := hcpCapacityAllowedClusters(ctx, orgID, userPerms)
	if err != nil {
		hlog.Errorf("GetHCPCapacityLatest: resolve clusters: %v", err)
		return hcpCapacityErr(c, http.StatusServiceUnavailable, "unable to resolve clusters for organization")
	}

	setRecommendationNoStore(c)
	var snapshots []model.HCPCapacitySnapshot
	if clusterParam != "" {
		if !clusterAllowed(allowed, clusterParam) {
			// Non-disclosing: an unauthorized (or nonexistent) cluster is
			// indistinguishable — 200-empty.
			return c.JSON(http.StatusOK, CollectionResponse([]hcpCapacityRowResponse{}, c.Request(), 0, 0, 0))
		}
		snap, err := model.GetLatestHCPCapacity(ctx, pool, orgID, clusterParam)
		if err != nil {
			hlog.Errorf("GetHCPCapacityLatest: query: %v", err)
			return hcpCapacityErr(c, http.StatusInternalServerError, "unable to fetch records from database")
		}
		if snap != nil {
			snapshots = []model.HCPCapacitySnapshot{*snap}
		}
	} else {
		if len(allowed) == 0 {
			return c.JSON(http.StatusOK, CollectionResponse([]hcpCapacityRowResponse{}, c.Request(), 0, 0, 0))
		}
		snapshots, err = model.ListLatestHCPCapacity(ctx, pool, orgID, allowed)
		if err != nil {
			hlog.Errorf("GetHCPCapacityLatest: list query: %v", err)
			return hcpCapacityErr(c, http.StatusInternalServerError, "unable to fetch records from database")
		}
	}

	now := time.Now().UTC()
	rows := make([]hcpCapacityRowResponse, 0, len(snapshots))
	for i := range snapshots {
		mceStale := hcpCapacityMCEMayBeStale(ctx, pool, orgID, snapshots[i].ClusterUUID, snapshots[i])
		rows = append(rows, buildHCPCapacityRow(snapshots[i], now, &mceStale))
	}
	return c.JSON(http.StatusOK, CollectionResponse(rows, c.Request(), len(rows), len(rows), 0))
}

// GetHCPCapacityHistory handles GET /recommendations/openshift/hcp/fleet-capacity/history.
func GetHCPCapacityHistory(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)
	ctx := c.Request().Context()

	clusterParam := hcpCapacityClusterParam(c)
	if clusterParam == "" {
		return hcpCapacityErr(c, http.StatusBadRequest, "filter[cluster_uuid] is required: history is a per-cluster trend")
	}
	if _, err := uuid.Parse(clusterParam); err != nil {
		return hcpCapacityErr(c, http.StatusBadRequest, "invalid cluster_uuid")
	}
	limit, offset, pageErr := listoptions.ParsePagination(c, 20)
	if pageErr != nil {
		return hcpCapacityErr(c, http.StatusBadRequest, pageErr.Error())
	}

	pool := database.GetPool()
	if pool == nil {
		return hcpCapacityErr(c, http.StatusServiceUnavailable, "database connection unavailable")
	}
	allowed, err := hcpCapacityAllowedClusters(ctx, orgID, userPerms)
	if err != nil {
		hlog.Errorf("GetHCPCapacityHistory: resolve clusters: %v", err)
		return hcpCapacityErr(c, http.StatusServiceUnavailable, "unable to resolve clusters for organization")
	}

	setRecommendationNoStore(c)
	if !clusterAllowed(allowed, clusterParam) {
		return c.JSON(http.StatusOK, CollectionResponse([]hcpCapacityRowResponse{}, c.Request(), 0, limit, offset))
	}

	snapshots, total, err := model.GetHCPCapacityHistory(ctx, pool, orgID, clusterParam, limit, offset)
	if err != nil {
		hlog.Errorf("GetHCPCapacityHistory: query: %v", err)
		return hcpCapacityErr(c, http.StatusInternalServerError, "unable to fetch records from database")
	}

	now := time.Now().UTC()
	rows := make([]hcpCapacityRowResponse, 0, len(snapshots))
	for i := range snapshots {
		rows = append(rows, buildHCPCapacityRow(snapshots[i], now, nil))
	}
	return c.JSON(http.StatusOK, CollectionResponse(rows, c.Request(), int(total), limit, offset))
}
