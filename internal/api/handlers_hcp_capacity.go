package api

import (
	"context"
	"net/http"
	"strconv"
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

// hcpCapacityOCPFloorMajor/minor is the documented OCP management version
// floor (ADR-0334 amendment: OCP 4.17+ / MCE 2.6+).
const (
	hcpCapacityOCPFloorMajor = 4
	hcpCapacityOCPFloorMinor = 17
)

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

// hcpCapacityFreshnessWindow is 2× the row's own effective cadence: a
// 6-hour upload cycle must not mark a fresh hourly row stale.
func hcpCapacityFreshnessWindow(s model.HCPCapacitySnapshot) time.Duration {
	cadence := s.CollectionIntervalMinutes
	if s.UploadCycleMinutes > cadence {
		cadence = s.UploadCycleMinutes
	}
	return 2 * time.Duration(cadence) * time.Minute
}

// hcpCapacityBelowFloor reports a known ocp_version below the documented
// 4.17 floor. Unparseable or unknown versions never warn (only a clearly
// below-floor version does).
func hcpCapacityBelowFloor(ocpVersion string) bool {
	if ocpVersion == "" {
		return false
	}
	parts := strings.SplitN(ocpVersion, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil {
		return false
	}
	return major < hcpCapacityOCPFloorMajor ||
		(major == hcpCapacityOCPFloorMajor && minor < hcpCapacityOCPFloorMinor)
}

// buildHCPCapacityRow derives the serving row from the stored snapshot.
// Headroom nil rules mirror librobne/hcp exactly: no estimate (empty
// limiting resource), incomplete inventory, unknown worker placement, or
// unknown average QPS (that scenario only). Affirmative pressure
// additionally withholds headroom on every scenario (ADR-0334 rule 8);
// unknown pressure keeps headroom and warns explicitly.
func buildHCPCapacityRow(s model.HCPCapacitySnapshot, now time.Time, mceMayBeStale *bool) hcpCapacityRowResponse {
	age := now.Sub(s.ObservedAt)
	if age < 0 {
		age = 0
	}
	pressured := s.PressureState == "pressured"

	status := hcpcapacity.StatusOK
	if s.HostedAvailabilityBasis == hcpcapacity.BasisMixed || s.HostedAvailabilityBasis == hcpcapacity.BasisSingleReplica {
		status = hcpcapacity.StatusIllustrativeOnly
	}

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
			Stale:                age > hcpCapacityFreshnessWindow(s),
			BelowVersionFloor:    hcpCapacityBelowFloor(s.OCPVersion),
			Pressured:            pressured,
			PressureUnknown:      s.PressureState == "unknown",
			MCEMayBeStale:        mceMayBeStale,
			SharedInfrastructure: s.SharedInfrastructure,
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
		if col.limit != "" && s.InventoryComplete && s.WorkerPlacementKnown && !pressured &&
			!(col.name == hcpcapacity.ScenarioAverageQPS && !s.ReadyHCPQPSKnown) {
			h := col.max - s.CurrentHCCount
			sc.Headroom = &h
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

// hcpCapacityMCEEqual compares the five MCE reference gauges (NULL is a
// value: absent == absent, absent != zero).
func hcpCapacityMCEEqual(a, b model.HCPCapacitySnapshot) bool {
	eq := func(x, y *int64) bool {
		if x == nil || y == nil {
			return x == y
		}
		return *x == *y
	}
	return eq(a.MCERequestMaxHCs, b.MCERequestMaxHCs) &&
		eq(a.MCELowQPSMaxHCs, b.MCELowQPSMaxHCs) &&
		eq(a.MCEAverageQPSMaxHCs, b.MCEAverageQPSMaxHCs) &&
		eq(a.MCEMediumQPSMaxHCs, b.MCEMediumQPSMaxHCs) &&
		eq(a.MCEHighQPSMaxHCs, b.MCEHighQPSMaxHCs)
}

// hcpCapacityNativeDiffers reports whether the native inputs moved
// between two snapshots (worker capacity, occupancy, or traffic basis).
func hcpCapacityNativeDiffers(a, b model.HCPCapacitySnapshot) bool {
	return a.EligibleCPUMillicores != b.EligibleCPUMillicores ||
		a.EligibleMemoryBytes != b.EligibleMemoryBytes ||
		a.EligibleMaxPods != b.EligibleMaxPods ||
		a.CurrentHCCount != b.CurrentHCCount ||
		a.ReadyHCPCount != b.ReadyHCPCount ||
		a.ReadyHCPQPSKnown != b.ReadyHCPQPSKnown ||
		!hcpCapacityQPSEqual(a, b)
}

func hcpCapacityQPSEqual(a, b model.HCPCapacitySnapshot) bool {
	if a.AverageReadyHCPQPS == nil || b.AverageReadyHCPQPS == nil {
		return a.AverageReadyHCPQPS == b.AverageReadyHCPQPS
	}
	return *a.AverageReadyHCPQPS == *b.AverageReadyHCPQPS
}

// hcpCapacityMCEMayBeStale implements the frozen-reference caution: MCE
// gauges unchanged across the full freshness window while native inputs
// moved → the references may lag the fleet. Degrades to false on query
// error — the caution must never fail the read.
func hcpCapacityMCEMayBeStale(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, latest model.HCPCapacitySnapshot) bool {
	rows, err := model.GetHCPCapacitySince(ctx, pool, orgID, clusterUUID, latest.ObservedAt.Add(-hcpCapacityFreshnessWindow(latest)))
	if err != nil || len(rows) < 2 {
		return false
	}
	mceSame := true
	nativeChanged := false
	for _, prior := range rows {
		if prior.ObservedAt.Equal(latest.ObservedAt) {
			continue
		}
		mceSame = mceSame && hcpCapacityMCEEqual(prior, latest)
		nativeChanged = nativeChanged || hcpCapacityNativeDiffers(prior, latest)
	}
	return mceSame && nativeChanged
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
