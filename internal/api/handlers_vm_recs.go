package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/vm"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

// VM sizing resource blocks for API responses.
type vmSizingBlock struct {
	VCPU         int32   `json:"vcpu"`
	MemoryGiB    int32   `json:"memory_gib"`
	DiskGiB      *int32  `json:"disk_gib"`
	InstanceType *string `json:"instance_type"`
}

type vmRecommendedSizing struct {
	vmSizingBlock
	Series *string `json:"series,omitempty"`
}

type vmRecMetadata struct {
	GuestAgentDetected   bool    `json:"guest_agent_detected"`
	Confidence           string  `json:"confidence"`
	Term                 string  `json:"term"`
	Engine               string  `json:"engine"`
	IsIdle               bool    `json:"is_idle"`
	IsAbandoned          bool    `json:"is_abandoned"`
	IsPowerOffCandidate  bool    `json:"is_power_off_candidate"`
	PowerOffIdlePct      *int32  `json:"power_off_idle_pct,omitempty"`
	IsOversized          bool    `json:"is_oversized"`
	IsNetworkBound       bool    `json:"is_network_bound"`
	IsRedundantPlacement bool    `json:"is_redundant_placement"`
	HasSharedStorage     bool    `json:"has_shared_storage"`
	NUMAOversized        bool    `json:"numa_oversized"`
	PreferenceName       *string `json:"preference_name,omitempty"`
	PreferenceClass      *string `json:"preference_class,omitempty"`
}

type vmIOProfile struct {
	ReadIOPSP95  *int64  `json:"read_iops_p95"`
	WriteIOPSP95 *int64  `json:"write_iops_p95"`
	ReadBPSP95   *int64  `json:"read_bps_p95"`
	WriteBPSP95  *int64  `json:"write_bps_p95"`
	Hint         *string `json:"hint"`
	Pattern      string  `json:"pattern,omitempty"`
}

type vmDiskProjection struct {
	DaysUntilFull        *int32   `json:"days_until_full"`
	GrowthGiBPerDay      *float64 `json:"growth_gib_per_day"`
	RecommendedExpandGiB *int32   `json:"recommended_expand_gib"`
}

type vmGPURecommendation struct {
	GPUCount                  int32                   `json:"gpu_count"`
	GPUModel                  string                  `json:"gpu_model,omitempty"`
	GPUClassification         string                  `json:"gpu_classification,omitempty"`
	RecommendedGPUAction      string                  `json:"recommended_gpu_action,omitempty"`
	RecommendedGPUProfile     string                  `json:"recommended_gpu_profile,omitempty"`
	RecommendedTimeSliceCount int32                   `json:"recommended_time_slice_count,omitempty"`
	GPUTimeSliceConfidence    string                  `json:"gpu_timeslice_confidence,omitempty"`
	GPUTimeSliceRationale     string                  `json:"gpu_timeslice_rationale,omitempty"`
	RecommendedVGPUProfile    string                  `json:"recommended_vgpu_profile,omitempty"`
	GPUUtilizationAvgBP       int32                   `json:"gpu_utilization_avg_bp"`
	GPUDevices                []model.GPUDeviceDigest `json:"gpu_devices,omitempty"`
}

// VMRecommendationItem is a single VM recommendation in list/detail responses.
type VMRecommendationItem struct {
	ID                string                  `json:"id,omitempty"`
	VMName            string                  `json:"vm_name"`
	Namespace         string                  `json:"namespace"`
	ClusterUUID       string                  `json:"cluster_uuid"`
	GuestOS           string                  `json:"guest_os"`
	Category          string                  `json:"category,omitempty"`
	Current           vmSizingBlock           `json:"current"`
	Recommended       vmRecommendedSizing     `json:"recommended"`
	Metadata          vmRecMetadata           `json:"metadata"`
	IOProfile         vmIOProfile             `json:"io_profile"`
	DiskProjection    vmDiskProjection        `json:"disk_projection"`
	Notifications     []any                   `json:"notifications"`
	GPU               *vmGPURecommendation    `json:"gpu,omitempty"`
	Savings           *money.MoneyAmount      `json:"estimated_monthly_savings"`
	LastRecommendedAt string                  `json:"last_recommended_at"`
	DailyDigests      []vmDailyDigestItem     `json:"daily_digests,omitempty"`
	Explanation       *model.VMExplanationAPI `json:"explanation,omitempty"`
}

type vmDailyDigestItem struct {
	BucketDate       string                  `json:"bucket_date"`
	CPUUsageP95MC    int64                   `json:"cpu_usage_p95_mc"`
	MemUsageP95KiB   int64                   `json:"mem_usage_p95_kib"`
	SampleCount      int32                   `json:"sample_count"`
	CPUUsageP50MC    int64                   `json:"cpu_usage_p50_mc,omitempty"`
	CPUUsageP99MC    int64                   `json:"cpu_usage_p99_mc,omitempty"`
	CPUUsageMaxMC    int64                   `json:"cpu_usage_max_mc,omitempty"`
	MemUsageP50KiB   int64                   `json:"mem_usage_p50_kib,omitempty"`
	MemUsageP99KiB   int64                   `json:"mem_usage_p99_kib,omitempty"`
	MemUsageMaxKiB   int64                   `json:"mem_usage_max_kib,omitempty"`
	DiskReadIOPSP95  *int64                  `json:"disk_read_iops_p95,omitempty"`
	DiskWriteIOPSP95 *int64                  `json:"disk_write_iops_p95,omitempty"`
	DiskReadBPSP95   *int64                  `json:"disk_read_bps_p95,omitempty"`
	DiskWriteBPSP95  *int64                  `json:"disk_write_bps_p95,omitempty"`
	GPUDevices       []model.GPUDeviceDigest `json:"gpu_devices,omitempty"`
}

// VMRecommendationListResponse wraps paginated VM recommendations.
type VMRecommendationListResponse struct {
	Meta  Metadata               `json:"meta"`
	Links Links                  `json:"links"`
	Data  []VMRecommendationItem `json:"data"`
}

var vmRecAllowedOrderBy = map[string]string{
	"vm_name":                   "vm_name",
	"namespace":                 "namespace",
	"current_vcpu":              "current_vcpu",
	"current_memory_gib":        "current_memory_gib",
	"guest_os":                  "guest_os",
	"recommended_vcpu":          "recommended_vcpu",
	"recommended_memory_gib":    "recommended_memory_gib",
	"is_idle":                   "is_idle",
	"is_abandoned":              "is_abandoned",
	"is_oversized":              "is_oversized",
	"confidence":                "confidence",
	"last_recommended_at":       "last_recommended_at",
	"estimated_monthly_savings": "estimated_savings_cents",
	"savings":                   "estimated_savings_cents", // deprecated alias
	"savings_amount":            "estimated_savings_cents", // deprecated alias
}

const vmRecDefaultOrderBy = "vm_name"

var vmRecValidConfidence = map[string]struct{}{
	"high": {}, "moderate": {}, "low": {},
}

func parseVMRecBoolFilter(c echo.Context, param string) (*bool, error) {
	v := queryparams.FirstFilter(c, param)
	if v == "" {
		return nil, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: must be true or false", param)
	}
	return &b, nil
}

// GetVMRecommendations handles GET /recommendations/openshift/vm.
func GetVMRecommendations(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	limit := 10
	offset := 0
	if l := c.QueryParam("limit"); l != "" {
		v, err := strconv.Atoi(l)
		if err != nil || v <= 0 || v > 100 {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":  "error",
				"message": "limit must be an integer between 1 and 100",
			})
		}
		limit = v
	}
	if o := c.QueryParam("offset"); o != "" {
		v, err := strconv.Atoi(o)
		if err != nil || v < 0 {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":  "error",
				"message": "offset must be a non-negative integer",
			})
		}
		offset = v
	}

	orderByKey, orderHow, err := queryparams.ParseOrderByAPIKey(c, vmRecAllowedOrderBy, vmRecDefaultOrderBy, listoptions.OrderAsc)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	vmCursor, hasVMCursor, vmCursorErr := applyVMCursor(c, orderByKey)
	if vmCursorErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": vmCursorErr.Error()})
	}
	if hasVMCursor {
		offset = 0
	}

	responseFormat, formatErr := listoptions.ResolveResponseFormat(c.Request().Header.Get("Accept"), c.QueryParam("format"))
	if formatErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": formatErr.Error()})
	}

	isIdle, err := parseVMRecBoolFilter(c, "is_idle")
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	isOversized, err := parseVMRecBoolFilter(c, "is_oversized")
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	isAbandoned, err := parseVMRecBoolFilter(c, "is_abandoned")
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	guestAgentDetected, err := parseVMRecBoolFilter(c, "guest_agent_detected")
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	hasGPU, err := parseVMRecBoolFilter(c, "has_gpu")
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	isNetworkBound, err := parseVMRecBoolFilter(c, "is_network_bound")
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}
	isPowerOffCandidate, err := parseVMRecBoolFilter(c, "is_power_off_candidate")
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": err.Error()})
	}

	engineFilter := queryparams.FirstFilter(c, "engine")
	if engineFilter != "" && engineFilter != "cost" && engineFilter != "performance" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "invalid engine"})
	}
	confidenceFilter := queryparams.FirstFilter(c, "confidence")
	if confidenceFilter != "" {
		if _, ok := vmRecValidConfidence[confidenceFilter]; !ok {
			return c.JSON(http.StatusBadRequest, echo.Map{
				"status":  "error",
				"message": "confidence must be one of: high, moderate, low",
			})
		}
	}

	ctx := c.Request().Context()
	allClusters, err := getClustersForOrg(ctx, orgID)
	if err != nil {
		hlog.Errorf("GetVMRecommendations: resolve clusters: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	allowedClusters := filterClustersByRBAC(allClusters, userPerms)
	if len(allowedClusters) == 0 {
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, VMRecommendationListResponse{
			Meta: Metadata{Count: 0, Limit: limit, Offset: offset, Currency: resolveListCurrencyFromRequest(c, orgID)},
			Data: []VMRecommendationItem{},
		})
	}

	clusterFilter := queryparams.FirstFilter(c, "cluster")
	if clusterFilter != "" {
		allowed := false
		for _, u := range allowedClusters {
			if u == clusterFilter {
				allowed = true
				break
			}
		}
		if !allowed {
			setRecommendationNoStore(c)
			return c.JSON(http.StatusOK, VMRecommendationListResponse{
				Meta: Metadata{Count: 0, Limit: limit, Offset: offset, Currency: resolveListCurrencyFromRequest(c, orgID)},
				Data: []VMRecommendationItem{},
			})
		}
		allowedClusters = []string{clusterFilter}
	}

	vmGroupByField, vmGroupByErr := parseVMListGroupBy(c)
	if vmGroupByErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": vmGroupByErr.Error()})
	}
	if vmGroupByField != "" {
		return getVMRecsGrouped(c, ctx, pool, hlog, orgID, allowedClusters, limit, offset, vmGroupByField)
	}

	filters := vm.VMRecommendationFilters{
		ClusterUUIDs:        allowedClusters,
		Namespace:           queryparams.FirstFilter(c, "project"),
		VMName:              queryparams.FirstFilter(c, "vm_name"),
		Node:                queryparams.FirstFilter(c, "node"),
		Term:                queryparams.FirstFilter(c, "term"),
		Engine:              engineFilter,
		Confidence:          confidenceFilter,
		GuestAgentDetected:  guestAgentDetected,
		IsIdle:              isIdle,
		IsAbandoned:         isAbandoned,
		IsOversized:         isOversized,
		IsNetworkBound:      isNetworkBound,
		IsPowerOffCandidate: isPowerOffCandidate,
		HasGPU:              hasGPU,
		GPUClassification:   queryparams.FirstFilter(c, "gpu_classification"),
		GuestOS:             queryparams.FirstFilter(c, "guest_os"),
		OrderBy:             orderByKey,
		OrderDesc:           orderHow == listoptions.OrderDesc,
		Limit:               limit,
		Offset:              offset,
		UseKeyset:           hasVMCursor,
		KeysetCursor: vm.VMListCursor{
			ClusterUUID: vmCursor.ClusterUUID,
			VMName:      vmCursor.VMName,
			Namespace:   vmCursor.Namespace,
			Term:        vmCursor.Term,
			Engine:      vmCursor.Engine,
			SortValue:   vmCursor.SortValue,
			HasSort:     len(vmCursor.SortValue) > 0,
		},
	}
	if config.TagsFeatureEnabled() {
		tagFilters, tagErr := parseTagFiltersFromRequest(c)
		if tagErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": tagErr.Error()})
		}
		filters.TagFilters = tagFilters
	}

	recs, total, err := vm.ListVMRecommendations(ctx, pool, orgID, filters)
	if err != nil {
		hlog.Errorf("GetVMRecommendations: list failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch VM recommendations",
		})
	}

	hasNext := false
	var nextCursor string
	if limit > 0 && len(recs) > limit {
		hasNext = true
		last := recs[limit-1]
		nextCursor = vmNextCursor(orderByKey, last)
		recs = recs[:limit]
	} else if !hasVMCursor && limit > 0 {
		hasNext = offset+limit < int(total)
	}

	data := make([]VMRecommendationItem, 0, len(recs))
	for _, r := range recs {
		data = append(data, vmRecToAPIItem(r))
	}

	setRecommendationNoStore(c)
	if responseFormat == listoptions.ResponseFormatCSV {
		return streamCSV(c, csvFilename("vm-recommendations"), func(ctx context.Context, w io.Writer) error {
			return generateVMRecCSV(ctx, w, data)
		})
	}

	links := buildLinks(c.Request(), int(total), limit, offset)
	applyKeysetNextLink(&links, c.Request(), limit, hasNext, nextCursor)

	resp := VMRecommendationListResponse{
		Meta: Metadata{
			Count: int(total), Limit: limit, Offset: offset,
			HasNext: hasNext, NextCursor: nextCursor,
			Currency: resolveListCurrencyFromRequest(c, orgID),
		},
		Links: links,
		Data:  data,
	}
	if resp.Data == nil {
		resp.Data = []VMRecommendationItem{}
	}
	return c.JSON(http.StatusOK, resp)
}

// GetVMRecommendationDetail handles GET /recommendations/openshift/vm/detail.
func GetVMRecommendationDetail(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	clusterUUID := strings.TrimSpace(c.QueryParam("cluster_uuid"))
	if clusterUUID == "" {
		clusterUUID = queryparams.FirstFilter(c, "cluster")
	}
	vmName := strings.TrimSpace(c.QueryParam("vm_name"))
	namespace := strings.TrimSpace(c.QueryParam("namespace"))
	term := strings.TrimSpace(c.QueryParam("term"))
	engineName := strings.TrimSpace(c.QueryParam("engine"))

	if clusterUUID == "" || vmName == "" || namespace == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": "cluster_uuid, vm_name, and namespace are required",
		})
	}
	if term == "" {
		term = "medium_term"
	}
	if engineName == "" {
		engineName = "cost"
	}
	if engineName != "cost" && engineName != "performance" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "invalid engine"})
	}

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	ctx := c.Request().Context()
	allClusters, err := getClustersForOrg(ctx, orgID)
	if err != nil {
		hlog.Errorf("GetVMRecommendationDetail: resolve clusters: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	allowed := filterClustersByRBAC(allClusters, get_user_permissions(c))
	if !clusterAllowed(allowed, clusterUUID) {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": "VM recommendation not found",
		})
	}

	rec, digests, err := vm.GetVMRecommendationDetail(ctx, pool, orgID, clusterUUID, vmName, namespace, term, engineName)
	if err != nil {
		hlog.Errorf("GetVMRecommendationDetail: query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch VM recommendation",
		})
	}
	if rec == nil {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": "VM recommendation not found",
		})
	}

	item := vmRecToAPIItem(*rec)
	item.DailyDigests = vmDigestsToAPI(digests)
	if RequestIncludesExplanation(c.QueryParam("include")) {
		item.Explanation = model.BuildVMExplanationAPI(*rec)
	}
	if item.GPU != nil && len(digests) > 0 {
		gpuDetail := vm.AnalyzeVMGPU(digests, vm.VMRecConfigResolved())
		item.GPU.GPUDevices = gpuDetail.GPUDevices
	}
	if clusterID, parseErr := uuid.Parse(clusterUUID); parseErr == nil {
		enrichVMRecPreferenceMetadata(c.Request().Context(), pool, orgID, clusterID, &item)
	}
	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, item)
}

func enrichVMRecPreferenceMetadata(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID, item *VMRecommendationItem) {
	if item == nil || pool == nil {
		return
	}
	prefCtx, err := vm.QueryClusterVMPreferences(ctx, pool, orgID, clusterUUID)
	if err != nil || prefCtx == nil {
		return
	}
	name, class := prefCtx.PreferenceInfoForVM(item.Namespace, item.VMName)
	if name != "" {
		item.Metadata.PreferenceName = &name
	}
	if class != "" {
		item.Metadata.PreferenceClass = &class
	}
}

func clusterAllowed(allowed []string, clusterUUID string) bool {
	for _, u := range allowed {
		if u == clusterUUID {
			return true
		}
	}
	return false
}

func vmRecToAPIItem(r model.VMRecommendation) VMRecommendationItem {
	item := VMRecommendationItem{
		ID:          model.NativeVMID(r.ClusterUUID.String(), r.Namespace, r.VMName),
		VMName:      r.VMName,
		Namespace:   r.Namespace,
		ClusterUUID: r.ClusterUUID.String(),
		GuestOS:     r.GuestOS,
		Current: vmSizingBlock{
			VCPU:         r.CurrentVCPU,
			MemoryGiB:    r.CurrentMemoryGiB,
			DiskGiB:      r.CurrentDiskGiB,
			InstanceType: r.CurrentInstanceType,
		},
		Recommended: vmRecommendedSizing{
			vmSizingBlock: vmSizingBlock{
				VCPU:         r.RecommendedVCPU,
				MemoryGiB:    r.RecommendedMemoryGiB,
				DiskGiB:      r.RecommendedDiskGiB,
				InstanceType: r.RecommendedInstanceType,
			},
			Series: r.RecommendedSeries,
		},
		Metadata: vmRecMetadata{
			GuestAgentDetected:   r.GuestAgentDetected,
			Confidence:           r.Confidence,
			Term:                 r.Term,
			Engine:               r.Engine,
			IsIdle:               r.IsIdle,
			IsAbandoned:          r.IsAbandoned,
			IsPowerOffCandidate:  r.IsPowerOffCandidate,
			PowerOffIdlePct:      vmPowerOffIdlePctForAPI(r.PowerOffIdleRatio),
			IsOversized:          r.IsOversized,
			IsNetworkBound:       r.IsNetworkBound,
			IsRedundantPlacement: r.IsRedundantPlacement,
			HasSharedStorage:     r.HasSharedStorage,
			NUMAOversized:        r.NUMAOversized,
		},
		IOProfile: vmIOProfile{
			ReadIOPSP95:  r.IOReadIOPSP95,
			WriteIOPSP95: r.IOWriteIOPSP95,
			ReadBPSP95:   r.IOReadBPS95,
			WriteBPSP95:  r.IOWriteBPS95,
			Hint:         r.IOHint,
			Pattern:      r.IOPattern,
		},
		DiskProjection: vmDiskProjection{
			DaysUntilFull:        r.DiskDaysUntilFull,
			GrowthGiBPerDay:      r.DiskGrowthGiBPerDay,
			RecommendedExpandGiB: r.DiskRecommendedExpandGiB,
		},
		Notifications:     parseVMNotifications(r.Notifications),
		Savings:           vmRecMoneyAmount(r.EstimatedSavingsCents, r.SavingsCurrency),
		LastRecommendedAt: r.LastRecommendedAt.UTC().Format(time.RFC3339),
	}
	if r.GPUCount > 0 || r.GPUClassification != "" {
		item.GPU = &vmGPURecommendation{
			GPUCount:                  r.GPUCount,
			GPUModel:                  r.GPUModel,
			GPUClassification:         r.GPUClassification,
			RecommendedGPUAction:      r.RecommendedGPUAction,
			RecommendedGPUProfile:     r.RecommendedGPUProfile,
			RecommendedTimeSliceCount: r.RecommendedTimeSliceCount,
			GPUTimeSliceConfidence:    r.GPUTimeSliceConfidence,
			GPUTimeSliceRationale:     r.GPUTimeSliceRationale,
			RecommendedVGPUProfile:    r.RecommendedVGPUProfile,
			GPUUtilizationAvgBP:       r.GPUUtilizationAvgBP,
		}
	}

	switch {
	case r.IsIdle || r.IsAbandoned:
		item.Category = "oversized"
	case r.IsOversized:
		item.Category = "oversized"
	case r.RecommendedVCPU > r.CurrentVCPU || r.RecommendedMemoryGiB > r.CurrentMemoryGiB:
		item.Category = "undersized"
	case r.RecommendedVCPU < r.CurrentVCPU || r.RecommendedMemoryGiB < r.CurrentMemoryGiB:
		item.Category = "oversized"
	default:
		item.Category = "optimized"
	}

	return item
}

func vmPowerOffIdlePctForAPI(bp *int32) *int32 {
	if bp == nil {
		return nil
	}
	pct := vm.PowerOffIdlePercentFromBasisPoints(*bp)
	return &pct
}

func vmRecMoneyAmount(cents *int64, currency *string) *money.MoneyAmount {
	if cents == nil {
		return nil
	}
	cur := money.DefaultCurrency
	if currency != nil && *currency != "" {
		cur = *currency
	}
	return money.FormatCentsToAmountPtr(cents, cur)
}

func parseVMNotifications(raw []byte) []any {
	if len(raw) == 0 {
		return []any{}
	}
	var out []any
	if err := json.Unmarshal(raw, &out); err != nil {
		return []any{}
	}
	if out == nil {
		return []any{}
	}
	return out
}

func vmDigestsToAPI(digests []model.DailyVMDigest) []vmDailyDigestItem {
	visualInsights := config.VisualInsightsEnabled()
	out := make([]vmDailyDigestItem, 0, len(digests))
	for _, d := range digests {
		item := vmDailyDigestItem{
			BucketDate:     d.BucketDate.Format("2006-01-02"),
			CPUUsageP50MC:  d.CPUUsageP50MC,
			CPUUsageP95MC:  d.CPUUsageP95MC,
			CPUUsageP99MC:  d.CPUUsageP99MC,
			CPUUsageMaxMC:  d.CPUUsageMaxMC,
			MemUsageP50KiB: d.MemUsageP50KiB,
			MemUsageP95KiB: d.MemUsageP95KiB,
			MemUsageP99KiB: d.MemUsageP99KiB,
			MemUsageMaxKiB: d.MemUsageMaxKiB,
			SampleCount:    d.SampleCount,
		}
		if visualInsights {
			item.DiskReadIOPSP95 = d.DiskReadIOPSP95
			item.DiskWriteIOPSP95 = d.DiskWriteIOPSP95
			item.DiskReadBPSP95 = d.DiskReadBPS95
			item.DiskWriteBPSP95 = d.DiskWriteBPS95
		}
		if len(d.Devices) > 0 {
			item.GPUDevices = d.Devices
		}
		out = append(out, item)
	}
	return out
}
