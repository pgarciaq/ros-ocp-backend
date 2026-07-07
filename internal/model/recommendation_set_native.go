package model

import (
	"bytes"
	"database/sql/driver"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
	kruizeplugin "github.com/redhatinsights/ros-ocp-backend/internal/plugins/kruize"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
	"gorm.io/gorm"
)

var containerVariationOrderByRe = regexp.MustCompile(
	`^(cpu|memory)_variation_(short|medium|long)_(cost|performance)$`,
)

var containerVariationTermSuffix = map[string]string{
	"short":  kruizeplugin.ShortTerm,
	"medium": kruizeplugin.MediumTerm,
	"long":   kruizeplugin.LongTerm,
}

// remapNativeContainerOrderBy translates listoptions DB columns to native query aliases.
func remapNativeContainerOrderBy(col string) string {
	col = strings.Replace(col, "recommendation_sets.", "rs.", 1)
	col = strings.Replace(col, "clusters.", "c.", 1)
	col = strings.Replace(col, "workloads.workload_type", "rs.workload_type", 1)
	col = strings.Replace(col, "workloads.workload_name", "rs.workload", 1)
	col = strings.Replace(col, "workloads.namespace", "rs.namespace", 1)
	return col
}

func nativeContainerOrderHow(orderHow string) string {
	if orderHow == "" {
		return listoptions.OrderDesc
	}
	return orderHow
}

// nativeContainerSortExpr returns the ORDER BY expression and an optional rs WHERE clause
// (term+engine) for per-term variation sorts on native relational rows.
func nativeContainerSortExpr(orderByDBCol string) (sortExpr string, rsFilter string) {
	if orderByDBCol == "" {
		orderByDBCol = listoptions.DefaultContainerRecsDBColumn
	}
	for apiKey, dbCol := range listoptions.ContainerAllowedOrderBy {
		if dbCol != orderByDBCol {
			continue
		}
		if term, engine, resource, ok := nativeVariationOrderByParts(apiKey); ok {
			pctCol := "rs.variation_cpu_request_pct"
			if resource == "memory" {
				pctCol = "rs.variation_memory_request_pct"
			}
			return pctCol, fmt.Sprintf("rs.term = '%s' AND rs.engine = '%s'", term, engine)
		}
		switch apiKey {
		case "cpu_request_current":
			return "rs.current_cpu_request_millicores", ""
		case "memory_request_current":
			return "rs.current_memory_request_kib", ""
		}
		break
	}
	return remapNativeContainerOrderBy(orderByDBCol), ""
}

func nativeVariationOrderByParts(apiKey string) (term, engine, resource string, ok bool) {
	m := containerVariationOrderByRe.FindStringSubmatch(apiKey)
	if m == nil {
		return "", "", "", false
	}
	resource = m[1]
	term = containerVariationTermSuffix[m[2]]
	engine = m[3]
	return term, engine, resource, true
}

// nativeContainerSortUsesOrgKeysOnly reports whether sortExpr only references ock/c (org keys path).
func nativeContainerSortUsesOrgKeysOnly(sortExpr string) bool {
	return !strings.Contains(sortExpr, "rs.")
}

var log *logrus.Entry = logging.GetLogger()

// Fixed namespace UUID for deterministic ID generation (UUID v5).
//
// Deterministic IDs enable idempotent upserts: the same cluster/namespace/workload/container
// always maps to the same recommendation UUID across ingest runs. The UUID does NOT encode
// org_id — it is derived only from cluster and workload identity. Every detail lookup MUST
// filter by org_id (recommendation_sets.org_id) so one tenant cannot
// read another tenant's data by guessing a UUID. See docs/architecture/recommendation-ids.md.
var nativeIDNamespace = uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479")

// SmallintArray implements sql.Scanner and driver.Valuer for PostgreSQL
// SMALLINT[] columns so GORM can read/write []int16 via database/sql.
type SmallintArray []int16

func (a *SmallintArray) Scan(src interface{}) error {
	if src == nil {
		*a = nil
		return nil
	}
	switch v := src.(type) {
	case []int16:
		*a = append(SmallintArray(nil), v...)
		return nil
	case []byte:
		return scanSmallintArrayText(a, v)
	case string:
		return scanSmallintArrayText(a, []byte(v))
	default:
		return fmt.Errorf("SmallintArray.Scan: unsupported type %T", src)
	}
}

func scanSmallintArrayText(a *SmallintArray, b []byte) error {
	if len(b) == 0 {
		*a = nil
		return nil
	}
	if b[0] == '{' {
		b = b[1:]
	}
	if len(b) > 0 && b[len(b)-1] == '}' {
		b = b[:len(b)-1]
	}
	if len(b) == 0 {
		*a = nil
		return nil
	}
	result := make(SmallintArray, 0, bytes.Count(b, []byte{','})+1)
	start := 0
	for i := 0; i <= len(b); i++ {
		if i < len(b) && b[i] != ',' {
			continue
		}
		part := b[start:i]
		for len(part) > 0 && (part[0] == ' ' || part[0] == '\t') {
			part = part[1:]
		}
		for len(part) > 0 && (part[len(part)-1] == ' ' || part[len(part)-1] == '\t') {
			part = part[:len(part)-1]
		}
		if len(part) == 0 {
			start = i + 1
			continue
		}
		n, err := strconv.ParseInt(string(part), 10, 16)
		if err != nil {
			return fmt.Errorf("SmallintArray.Scan: parsing %q: %w", part, err)
		}
		result = append(result, int16(n))
		start = i + 1
	}
	*a = result
	return nil
}

func (a SmallintArray) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = strconv.FormatInt(int64(v), 10)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

// NativeRecommendationRow represents a single row from recommendation_sets
// using the new relational columns (one row per container+term+engine).
type NativeRecommendationRow struct {
	OrgID         string `gorm:"column:org_id"`
	ClusterUUID   string `gorm:"column:cluster_uuid"`
	Namespace     string `gorm:"column:namespace"`
	Workload      string `gorm:"column:workload"`
	WorkloadType  string `gorm:"column:workload_type"`
	ContainerName string `gorm:"column:container_name"`
	Term          string `gorm:"column:term"`
	Engine        string `gorm:"column:engine"`

	RecCPURequestMC  *int64 `gorm:"column:rec_cpu_request_millicores"`
	RecCPULimitMC    *int64 `gorm:"column:rec_cpu_limit_millicores"`
	RecMemRequestKiB *int64 `gorm:"column:rec_memory_request_kib"`
	RecMemLimitKiB   *int64 `gorm:"column:rec_memory_limit_kib"`

	CurrentCPURequestMC  *int64 `gorm:"column:current_cpu_request_millicores"`
	CurrentCPULimitMC    *int64 `gorm:"column:current_cpu_limit_millicores"`
	CurrentMemRequestKiB *int64 `gorm:"column:current_memory_request_kib"`
	CurrentMemLimitKiB   *int64 `gorm:"column:current_memory_limit_kib"`

	VariationCPURequestPct *int32   `gorm:"column:variation_cpu_request_pct"`
	VariationCPULimitPct   *int32   `gorm:"column:variation_cpu_limit_pct"`
	VariationMemRequestPct *int32   `gorm:"column:variation_memory_request_pct"`
	VariationMemLimitPct   *int32   `gorm:"column:variation_memory_limit_pct"`
	ConfidenceLevel        *float32 `gorm:"column:confidence_level"`
	// SMALLINT[] caps values at 32767. Current notification codes (1-24) are
	// well within range. If legacy 6-digit codes are ever needed, this column
	// type must be migrated to INTEGER[].
	NotificationCodes SmallintArray `gorm:"column:notification_codes;type:smallint[]"`
	Stale             bool          `gorm:"column:stale"`

	PodCountMin *int `gorm:"column:pod_count_min"`
	PodCountMax *int `gorm:"column:pod_count_max"`
	PodCountAvg *int `gorm:"column:pod_count_avg"`

	DesiredReplicas   *int `gorm:"column:desired_replicas"`
	AvailableReplicas *int `gorm:"column:available_replicas"`

	RecommendedReplicas *int    `gorm:"column:recommended_replicas"`
	ReplicaConfidence   *string `gorm:"column:replica_confidence"`
	ReplicaExplanation  *string `gorm:"column:replica_explanation"`

	EstimatedSavingsCents    *int64 `gorm:"column:estimated_savings_cents"`
	EstimatedCPUSavingsCents *int64 `gorm:"column:estimated_cpu_savings_cents"`
	EstimatedMemSavingsCents *int64 `gorm:"column:estimated_memory_savings_cents"`

	IdleState           string     `gorm:"column:idle_state"`
	IdleSince           *time.Time `gorm:"column:idle_since"`
	IdleDurationDays    *int       `gorm:"column:idle_duration_days"`
	PeakCPUMillicores   *int64     `gorm:"column:peak_cpu_millicores"`
	PeakMemoryBytes     *int64     `gorm:"column:peak_memory_bytes"`
	EstimatedWasteCents *int64     `gorm:"column:estimated_waste_cents"`

	RecommendationAppliedAt *time.Time `gorm:"column:recommendation_applied_at"`

	Category       *string `gorm:"column:category"`
	CategoryCPU    *string `gorm:"column:category_cpu"`
	CategoryMemory *string `gorm:"column:category_memory"`

	MonitoringEndTime *time.Time `gorm:"column:monitoring_end_time"`

	UpdatedAt    time.Time `gorm:"column:updated_at"`
	SourceID     string    `gorm:"column:source_id"`
	ClusterAlias string    `gorm:"column:cluster_alias"`
	LastReported time.Time `gorm:"column:last_reported_at"`
	AnalyticsIncomplete   bool       `gorm:"column:analytics_incomplete"`
	AnalyticsIncompleteAt *time.Time `gorm:"column:analytics_incomplete_at"`
	IngestHooksFailed     bool       `gorm:"column:ingest_hooks_failed"`
	IngestHooksFailedAt   *time.Time `gorm:"column:ingest_hooks_failed_at"`

	PageSortText *string `gorm:"column:ros_container_page_sort"`

	// Explanation factor columns (expl_*), always loaded; serialized only when requested.
	ExplDataDays            *int     `gorm:"column:expl_data_days"`
	ExplDecayHalfLifeHours  *float64 `gorm:"column:expl_decay_half_life_hours"`
	ExplCPUCostPctMC        *int64   `gorm:"column:expl_cpu_cost_pct_mc"`
	ExplCPUPerfPctMC        *int64   `gorm:"column:expl_cpu_perf_pct_mc"`
	ExplCPUUsageP95MC       *int64   `gorm:"column:expl_cpu_usage_p95_mc"`
	ExplCPUUsageP50MC       *int64   `gorm:"column:expl_cpu_usage_p50_mc"`
	ExplCPUUsageMeanMC      *int64   `gorm:"column:expl_cpu_usage_mean_mc"`
	ExplCPUAdaptiveMarginBP *int32   `gorm:"column:expl_cpu_adaptive_margin_bp"`
	ExplCPUTrendSlope       *float64 `gorm:"column:expl_cpu_trend_slope"`
	ExplMemCostPctKiB       *int64   `gorm:"column:expl_mem_cost_pct_kib"`
	ExplMemPerfPctKiB       *int64   `gorm:"column:expl_mem_perf_pct_kib"`
	ExplMemUsageP95KiB      *int64   `gorm:"column:expl_mem_usage_p95_kib"`
	ExplMemUsageP50KiB      *int64   `gorm:"column:expl_mem_usage_p50_kib"`
	ExplMemUsageMeanKiB     *int64   `gorm:"column:expl_mem_usage_mean_kib"`
	ExplMemAdaptiveMarginBP *int32   `gorm:"column:expl_mem_adaptive_margin_bp"`
	ExplMemTrendSlope       *float64 `gorm:"column:expl_mem_trend_slope"`
	ExplOOMCountSum         *int64   `gorm:"column:expl_oom_count_sum"`
	ExplOOMBumpApplied      *bool    `gorm:"column:expl_oom_bump_applied"`
	ExplCPUFloorApplied     *bool    `gorm:"column:expl_cpu_floor_applied"`
	ExplMemFloorApplied     *bool    `gorm:"column:expl_mem_floor_applied"`
	ExplIsIdle              *bool    `gorm:"column:expl_is_idle"`
	ExplGPUSMActiveAvgBP    *int32   `gorm:"column:expl_gpu_sm_active_avg_bp"`
	ExplGPUTensorActiveAvgBP *int32  `gorm:"column:expl_gpu_tensor_active_avg_bp"`
	ExplGPUDRAMActiveAvgBP  *int32   `gorm:"column:expl_gpu_dram_active_avg_bp"`
	ExplGPUFBUsageMaxMiB    *int32   `gorm:"column:expl_gpu_fb_usage_max_mib"`
	ExplGPUFBP98MiB         *int32   `gorm:"column:expl_gpu_fb_p98_mib"`
	ExplGPURecommendedProfile *string `gorm:"column:expl_gpu_recommended_profile"`
	ExplGPUCurrentProfile   *string  `gorm:"column:expl_gpu_current_profile"`
	ExplGPUHasProfilingData *bool    `gorm:"column:expl_gpu_has_profiling_data"`
	ExplGPUMemoryBound      *bool    `gorm:"column:expl_gpu_memory_bound"`
}

func (NativeRecommendationRow) TableName() string {
	return "recommendation_sets"
}

// NativeContainerResult is the API-ready format for a single container,
// with all 6 recommendation variants nested.
type NativeContainerResult struct {
	ID                      string                        `json:"id"`
	ClusterAlias            string                        `json:"cluster_alias"`
	ClusterUUID             string                        `json:"cluster_uuid"`
	Container               string                        `json:"container"`
	Project                 string                        `json:"project"`
	Workload                string                        `json:"workload"`
	WorkloadType            string                        `json:"workload_type"`
	SourceID                string                        `json:"source_id"`
	LastReported            string                        `json:"last_reported"`
	AnalyticsIncomplete     bool                          `json:"analytics_incomplete,omitempty"`
	AnalyticsIncompleteAt   *string                       `json:"analytics_incomplete_at,omitempty"`
	IngestHooksFailed       bool                          `json:"ingest_hooks_failed,omitempty"`
	IngestHooksFailedAt     *string                       `json:"ingest_hooks_failed_at,omitempty"`
	Replicas                *ReplicaInfo                  `json:"replicas,omitempty"`
	ReplicaOptimization     *ReplicaOptimization          `json:"replica_optimization,omitempty"`
	EstimatedMonthlySavings *money.MoneyAmount          `json:"estimated_monthly_savings,omitempty"`
	CPUSavings              *money.MoneyAmount          `json:"cpu_savings,omitempty"`
	MemorySavings           *money.MoneyAmount          `json:"memory_savings,omitempty"`
	EstimatedMonthlyWaste   *money.MoneyAmount          `json:"estimated_monthly_waste,omitempty"`
	Currency                string                        `json:"currency,omitempty"`
	IdleState               string                        `json:"idle_state"`
	IdleSince               *string                       `json:"idle_since,omitempty"`
	IdleDurationDays        *int                          `json:"idle_duration_days,omitempty"`
	PeakCPUMillicores       *int64                        `json:"peak_cpu_millicores,omitempty"`
	PeakMemoryBytes         *int64                        `json:"peak_memory_bytes,omitempty"`
	IdleRecommendation      *IdleRecommendation           `json:"idle_recommendation,omitempty"`
	Tags                    map[string]string             `json:"tags,omitempty"`
	Category                string                        `json:"category,omitempty"`
	CategoryCPU             string                        `json:"category_cpu,omitempty"`
	CategoryMemory          string                        `json:"category_memory,omitempty"`
	MonitoringEndTime       time.Time                     `json:"-"`
	Recommendations         map[string]TermRecommendation `json:"recommendations"`
	GPU                     map[string]*GPURecommendation `json:"gpu,omitempty"`

	// PaginationSort is the list order-by value for this container (not serialized).
	PaginationSort interface{} `json:"-"`
}

// NativeContainerID generates a deterministic UUID v5 from the composite key.
// workloadType is included to distinguish same-name workloads of different kinds
// (e.g. Deployment "api" vs StatefulSet "api" in the same namespace).
//
// Security: the returned ID is not tenant-scoped. Callers must always pair it with org_id
// when querying recommendation_sets (see nativeContainerDetailQuery).
func NativeContainerID(clusterUUID, namespace, workload, workloadType, container string) string {
	name := fmt.Sprintf("%s/%s/%s/%s/%s", clusterUUID, namespace, workload, workloadType, container)
	return uuid.NewSHA1(nativeIDNamespace, []byte(name)).String()
}

// NativeNamespaceID generates a deterministic UUID v5 for a namespace
// recommendation, keyed by cluster UUID and namespace name.
//
// Security: same org_id invariant as NativeContainerID — see nativeNamespaceDetailQuery.
func NativeNamespaceID(clusterUUID, namespace string) string {
	name := fmt.Sprintf("%s/%s", clusterUUID, namespace)
	return uuid.NewSHA1(nativeIDNamespace, []byte(name)).String()
}

// TermRecommendation holds cost and performance recommendations for a term.
type TermRecommendation struct {
	Cost        *EngineRecommendation `json:"cost,omitempty"`
	Performance *EngineRecommendation `json:"performance,omitempty"`
	Plots       *NativePlot           `json:"plots,omitempty"`
}

// BusinessHoursRecommendation is the optional business-hours perspective nested under an engine.
type BusinessHoursRecommendation struct {
	CPURequestMillicores *int64 `json:"-"`
	CPULimitMillicores   *int64 `json:"-"`
	MemRequestKiB        *int64 `json:"-"`
	MemLimitKiB          *int64 `json:"-"`
	Reason               string `json:"reason,omitempty"`
}

// EngineRecommendation holds the actual CPU/memory recommendation values.
type EngineRecommendation struct {
	CPURequestMillicores   *int64                                     `json:"cpu_request_millicores,omitempty"`
	CPULimitMillicores     *int64                                     `json:"cpu_limit_millicores,omitempty"`
	MemRequestKiB          *int64                                     `json:"memory_request_kib,omitempty"`
	MemLimitKiB            *int64                                     `json:"memory_limit_kib,omitempty"`
	CurrentCPURequestMC    *int64                                     `json:"current_cpu_request_millicores,omitempty"`
	CurrentCPULimitMC      *int64                                     `json:"current_cpu_limit_millicores,omitempty"`
	CurrentMemRequestKiB   *int64                                     `json:"current_memory_request_kib,omitempty"`
	CurrentMemLimitKiB     *int64                                     `json:"current_memory_limit_kib,omitempty"`
	VariationCPURequestPct *int32                                     `json:"variation_cpu_request_pct,omitempty"`
	VariationCPULimitPct   *int32                                     `json:"variation_cpu_limit_pct,omitempty"`
	VariationMemRequestPct *int32                                     `json:"variation_memory_request_pct,omitempty"`
	VariationMemLimitPct   *int32                                     `json:"variation_memory_limit_pct,omitempty"`
	ConfidenceLevel        *float32                                   `json:"confidence_level,omitempty"`
	NotificationCodes      SmallintArray                              `json:"notification_codes"`
	Notifications          map[string]notifications.NotificationEntry `json:"notifications"`
	BusinessHours          *BusinessHoursRecommendation               `json:"business_hours,omitempty"`
	Explanation            *ContainerExplanationAPI                   `json:"explanation,omitempty"`
}

const nativeContainerRSJoin = `JOIN recommendation_sets rs ON rs.org_id = ock.org_id
		AND rs.cluster_uuid = ock.cluster_uuid
		AND rs.namespace = ock.namespace
		AND rs.workload = ock.workload
		AND rs.workload_type = ock.workload_type
		AND rs.container_name = ock.container_name`

// buildNativeContainerKeysPageQuery selects one page of container keys from org_container_keys.
// Identity and cluster-metadata sorts paginate the key table directly; recommendation_sets
// columns join rs only for the sort expression (DISTINCT ON when multiple term/engine rows).
func buildNativeContainerKeysPageQuery(
	db *gorm.DB,
	orgID string,
	opts listoptions.ListOptions,
	keysParams map[string]interface{},
	queryParams map[string]interface{},
	userPerms map[string][]string,
	sortExpr, sortFilter, orderHow string,
) *gorm.DB {
	pageKeys := db.Table("org_container_keys ock").
		Joins("JOIN clusters c ON c.cluster_uuid = ock.cluster_uuid").
		Where("ock.org_id = ?", orgID)
	pageKeys = ApplyNativeRBAC(pageKeys, userPerms, "ock.namespace")
	pageKeys = ApplyQueryParamsToKeys(pageKeys, keysParams)
	if tagFilters := TagFiltersFromParams(queryParams); len(tagFilters) > 0 {
		pageKeys = ApplyTagFiltersToKeys(pageKeys, orgID, tagFilters)
	}

	keysOnlySort := nativeContainerSortUsesOrgKeysOnly(sortExpr)
	pageSortExpr := sortExpr
	if keysOnlySort {
		pageSortExpr = remapSortExprToOrgKeys(sortExpr)
	} else {
		pageKeys = pageKeys.Joins(nativeContainerRSJoin)
		if sortFilter != "" {
			pageKeys = pageKeys.Where(sortFilter)
		}
	}

	selectPrefix := ""
	if !keysOnlySort && sortFilter == "" {
		selectPrefix = "DISTINCT ON (ock.cluster_uuid, ock.namespace, ock.workload, ock.workload_type, ock.container_name) "
	}
	pageKeys = pageKeys.Select(fmt.Sprintf(
		"%sock.cluster_uuid, ock.namespace, ock.workload, ock.workload_type, ock.container_name, (%s)::text AS ros_container_page_sort",
		selectPrefix, pageSortExpr,
	))

	if keysOnlySort || sortFilter != "" {
		pageKeys = pageKeys.Order(nativeContainerKeysPageOrder(pageSortExpr, orderHow))
		pageKeys = applyNativeContainerKeysPageSeek(pageKeys, opts, pageSortExpr)
	} else {
		pageKeys = pageKeys.Order(nativeContainerKeysDistinctOnOrder(sortExpr, orderHow))
		pageKeys = applyNativeContainerPageSeek(pageKeys, opts, sortExpr)
	}
	return pageKeys
}

// GetNativeRecommendations queries the native relational columns from recommendation_sets.
func GetNativeRecommendations(orgID string, opts listoptions.ListOptions, queryParams map[string]interface{}, userPerms map[string][]string) (NativeListPage, error) {
	run := func(fn func(*gorm.DB) (NativeListPage, error)) (NativeListPage, error) {
		if isFleetWideNativeList(queryParams) {
			var page NativeListPage
			err := database.WithHeavyGORMStatementTimeout(func(tx *gorm.DB) error {
				var innerErr error
				page, innerErr = fn(tx)
				return innerErr
			})
			return page, err
		}
		return fn(database.GetDB())
	}

	// Tag filters require org_container_keys (resolved_tags / Koku tag joins). The legacy DISTINCT
	// path on recommendation_sets alone cannot apply tag predicates.
	if usesOrgContainerKeys(queryParams) || len(TagFiltersFromParams(queryParams)) > 0 {
		return run(func(gdb *gorm.DB) (NativeListPage, error) {
			return getNativeRecommendationsFromOrgKeys(gdb, orgID, opts, queryParams, userPerms)
		})
	}
	return run(func(gdb *gorm.DB) (NativeListPage, error) {
		return getNativeRecommendationsDistinct(gdb, orgID, opts, queryParams, userPerms)
	})
}

func isFleetWideNativeList(queryParams map[string]interface{}) bool {
	if len(TagFiltersFromParams(queryParams)) > 0 {
		return false
	}
	for key := range queryParams {
		if key == TagFiltersQueryKey || key == "rs.stale = ?" {
			continue
		}
		return false
	}
	return true
}

func getNativeRecommendationsFromOrgKeys(gdb *gorm.DB, orgID string, opts listoptions.ListOptions, queryParams map[string]interface{}, userPerms map[string][]string) (NativeListPage, error) {
	db := gdb
	keysParams, detailParams := splitNativeListQueryParams(queryParams)

	limit := opts.Limit
	if opts.Format == listoptions.ResponseFormatCSV {
		limit = config.GetConfig().RecordLimitCSV
	}
	pageLimit := limit + 1

	sortExpr, sortFilter := nativeContainerPageSortExpr(opts.OrderBy)
	orderHow := nativeContainerOrderHow(opts.OrderHow)

	countQuery := db.Table("org_container_keys ock").
		Select("ock.cluster_uuid, ock.namespace, ock.workload, ock.workload_type, ock.container_name").
		Joins("JOIN clusters c ON c.cluster_uuid = ock.cluster_uuid").
		Where("ock.org_id = ?", orgID)
	countQuery = ApplyNativeRBAC(countQuery, userPerms, "ock.namespace")
	countQuery = ApplyQueryParamsToKeys(countQuery, keysParams)
	if tagFilters := TagFiltersFromParams(queryParams); len(tagFilters) > 0 {
		countQuery = ApplyTagFiltersToKeys(countQuery, orgID, tagFilters)
	}

	pageKeysQuery := buildNativeContainerKeysPageQuery(db, orgID, opts, keysParams, queryParams, userPerms, sortExpr, sortFilter, orderHow)

	pageSubquery := db.Table("(?) AS page", pageKeysQuery).
		Select("page.cluster_uuid, page.namespace, page.workload, page.workload_type, page.container_name, page.ros_container_page_sort").
		Order(nativeContainerPageOrder("page", orderHow))
	if !opts.HasCursor {
		pageSubquery = pageSubquery.Offset(opts.Offset)
	}
	pageSubquery = pageSubquery.Limit(pageLimit)

	t0 := time.Now().UTC()
	detailQuery := db.Table("recommendation_sets rs").
		Select(nativeDetailSelect+", page.ros_container_page_sort").
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Joins(`JOIN (?) page ON page.cluster_uuid = rs.cluster_uuid
			AND page.namespace = rs.namespace
			AND page.workload = rs.workload
			AND page.workload_type = rs.workload_type
			AND page.container_name = rs.container_name`, pageSubquery)
	detailQuery = ApplyQueryParams(detailQuery, detailParams)
	sqlRows, err := detailQuery.Order(nativeContainerDetailOrder(orderHow)).Rows()
	if err != nil {
		return NativeListPage{}, err
	}
	defer sqlRows.Close()
	rows, err := scanNativeContainerRows(sqlRows, pageLimit*6)
	if err != nil {
		return NativeListPage{}, err
	}

	results := assembleNativeResults(rows, sortExpr, false)

	hasNext := len(results) > limit
	var lastAnchor *ContainerPaginationAnchor
	if hasNext {
		last := results[limit-1]
		lastAnchor = &ContainerPaginationAnchor{
			SortValue:     last.PaginationSort,
			ClusterUUID:   last.ClusterUUID,
			Namespace:     last.Project,
			Workload:      last.Workload,
			WorkloadType:  last.WorkloadType,
			ContainerName: last.Container,
		}
		results = results[:limit]
	}

	totalContainers, err := resolveOrgContainerCount(orgID, db, countQuery)
	if err != nil {
		return NativeListPage{}, err
	}

	log.Infof("native list query: %dms (%d containers, %d rows)", time.Since(t0).Milliseconds(), totalContainers, len(rows))

	return NativeListPage{
		Results:    results,
		Count:      int(totalContainers),
		HasNext:    hasNext,
		LastAnchor: lastAnchor,
	}, nil
}

func getNativeRecommendationsDistinct(gdb *gorm.DB, orgID string, opts listoptions.ListOptions, queryParams map[string]interface{}, userPerms map[string][]string) (NativeListPage, error) {
	db := gdb
	limit := opts.Limit
	if opts.Format == listoptions.ResponseFormatCSV {
		limit = config.GetConfig().RecordLimitCSV
	}
	pageLimit := limit + 1

	sortExpr, sortFilter := nativeContainerPageSortExpr(opts.OrderBy)
	orderHow := nativeContainerOrderHow(opts.OrderHow)

	distinctSubquery := db.Table("recommendation_sets rs").
		Select(fmt.Sprintf(
			"DISTINCT ON (rs.cluster_uuid, rs.namespace, rs.workload, rs.workload_type, rs.container_name) rs.cluster_uuid, rs.namespace, rs.workload, rs.workload_type, rs.container_name, (%s)::text AS ros_container_page_sort",
			sortExpr,
		)).
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Where("rs.org_id = ?", orgID)
	distinctSubquery = ApplyNativeRBAC(distinctSubquery, userPerms)
	distinctSubquery = ApplyQueryParams(distinctSubquery, queryParams)
	if sortFilter != "" {
		distinctSubquery = distinctSubquery.Where(sortFilter)
	}
	distinctSubquery = distinctSubquery.Order(nativeContainerDistinctOnOrder(sortExpr, orderHow))
	distinctSubquery = applyNativeContainerPageSeek(distinctSubquery, opts, sortExpr)

	countDistinct := db.Table("(?) AS dn", distinctSubquery).
		Select("dn.cluster_uuid, dn.namespace, dn.workload, dn.workload_type, dn.container_name")

	pageSubquery := db.Table("(?) AS page", distinctSubquery).
		Select("page.cluster_uuid, page.namespace, page.workload, page.workload_type, page.container_name, page.ros_container_page_sort").
		Order(nativeContainerPageOrder("page", orderHow))
	if !opts.HasCursor {
		pageSubquery = pageSubquery.Offset(opts.Offset)
	}
	pageSubquery = pageSubquery.Limit(pageLimit)

	t0 := time.Now().UTC()
	detailQuery := db.Table("recommendation_sets rs").
		Select(nativeDetailSelect+", page.ros_container_page_sort").
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Joins(`JOIN (?) page ON page.cluster_uuid = rs.cluster_uuid
			AND page.namespace = rs.namespace
			AND page.workload = rs.workload
			AND page.workload_type = rs.workload_type
			AND page.container_name = rs.container_name`, pageSubquery)
	detailQuery = ApplyQueryParams(detailQuery, queryParams)
	sqlRows, err := detailQuery.Order(nativeContainerDetailOrder(orderHow)).Rows()
	if err != nil {
		return NativeListPage{}, err
	}
	defer sqlRows.Close()
	rows, err := scanNativeContainerRows(sqlRows, pageLimit*6)
	if err != nil {
		return NativeListPage{}, err
	}

	results := assembleNativeResults(rows, sortExpr, false)

	hasNext := len(results) > limit
	var lastAnchor *ContainerPaginationAnchor
	if hasNext {
		last := results[limit-1]
		lastAnchor = &ContainerPaginationAnchor{
			SortValue:     last.PaginationSort,
			ClusterUUID:   last.ClusterUUID,
			Namespace:     last.Project,
			Workload:      last.Workload,
			WorkloadType:  last.WorkloadType,
			ContainerName: last.Container,
		}
		results = results[:limit]
	}

	totalContainers, err := resolveOrgContainerCount(orgID, db, countDistinct)
	if err != nil {
		return NativeListPage{}, err
	}

	log.Infof("native list query: %dms (%d containers, %d rows)", time.Since(t0).Milliseconds(), totalContainers, len(rows))

	return NativeListPage{
		Results:    results,
		Count:      int(totalContainers),
		HasNext:    hasNext,
		LastAnchor: lastAnchor,
	}, nil
}

func resolveOrgContainerCount(orgID string, db *gorm.DB, filteredDistinct *gorm.DB) (int64, error) {
	if filteredDistinct != nil {
		var total int64
		if err := db.Table("(?) AS dc", filteredDistinct).Count(&total).Error; err != nil {
			return 0, err
		}
		return total, nil
	}

	if count, ok, err := GetOrgContainerCount(orgID); err != nil {
		return 0, err
	} else if ok {
		return count, nil
	}

	var total int64
	if err := db.Table("org_container_keys").Where("org_id = ?", orgID).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// ApplyNativeRBAC adds RBAC-based WHERE clauses using the native schema's column names.
// nsColumn is the fully-qualified namespace column (e.g. "rs.namespace", "h.namespace").
// Exported for reuse in history/quality query functions.
func ApplyNativeRBAC(query *gorm.DB, userPerms map[string][]string, nsColumn ...string) *gorm.DB {
	cfg := config.GetConfig()
	if !cfg.RBACEnabled {
		return query
	}
	if _, ok := userPerms["*"]; ok {
		return query
	}

	col := "rs.namespace"
	if len(nsColumn) > 0 && nsColumn[0] != "" {
		col = nsColumn[0]
	}

	clusterPerms, hasCluster := userPerms["openshift.cluster"]
	projectPerms, hasProject := userPerms["openshift.project"]
	clusterAll := hasCluster && utils.StringInSlice("*", clusterPerms)
	projectAll := hasProject && utils.StringInSlice("*", projectPerms)

	if hasCluster && !clusterAll {
		query = query.Where("c.cluster_uuid IN (?)", clusterPerms)
	}
	if hasProject && !projectAll {
		query = query.Where(col+" IN (?)", projectPerms)
	}
	return query
}

// ApplyQueryParams adds dynamic WHERE clauses from the parsed query parameters.
// For []string values (IN clauses), the slice is passed directly to GORM
// so it expands into IN ($1, $2, ...) rather than a single scalar.
func ApplyQueryParams(query *gorm.DB, queryParams map[string]interface{}) *gorm.DB {
	for key, values := range queryParams {
		if !isAllowedNativeRecommendationQueryKey(key) {
			log.Warnf("ApplyQueryParams: skipping unknown query key %q", key)
			continue
		}
		query = query.Where(key, values)
	}
	return query
}

// nativeDetailSelect is the shared SELECT clause for detail queries.
const nativeDetailSelect = `rs.org_id, rs.cluster_uuid, rs.namespace, rs.workload, rs.workload_type,
	rs.container_name, rs.term, rs.engine,
	rs.rec_cpu_request_millicores, rs.rec_cpu_limit_millicores,
	rs.rec_memory_request_kib, rs.rec_memory_limit_kib,
	rs.current_cpu_request_millicores, rs.current_cpu_limit_millicores,
	rs.current_memory_request_kib, rs.current_memory_limit_kib,
	rs.variation_cpu_request_pct, rs.variation_cpu_limit_pct,
	rs.variation_memory_request_pct, rs.variation_memory_limit_pct,
	rs.notification_codes, rs.confidence_level, rs.stale,
	rs.pod_count_min, rs.pod_count_max, rs.pod_count_avg,
	rs.desired_replicas, rs.available_replicas,
	rs.recommended_replicas, rs.replica_confidence, rs.replica_explanation,
	rs.estimated_savings_cents,
	rs.estimated_cpu_savings_cents, rs.estimated_memory_savings_cents,
	rs.idle_state, rs.idle_since, rs.idle_duration_days,
	rs.peak_cpu_millicores, rs.peak_memory_bytes, rs.estimated_waste_cents,
	rs.category, rs.category_cpu, rs.category_memory,
	rs.monitoring_end_time,
	rs.expl_data_days, rs.expl_decay_half_life_hours,
	rs.expl_cpu_cost_pct_mc, rs.expl_cpu_perf_pct_mc,
	rs.expl_cpu_usage_p95_mc, rs.expl_cpu_usage_p50_mc, rs.expl_cpu_usage_mean_mc,
	rs.expl_cpu_adaptive_margin_bp, rs.expl_cpu_trend_slope,
	rs.expl_mem_cost_pct_kib, rs.expl_mem_perf_pct_kib,
	rs.expl_mem_usage_p95_kib, rs.expl_mem_usage_p50_kib, rs.expl_mem_usage_mean_kib,
	rs.expl_mem_adaptive_margin_bp, rs.expl_mem_trend_slope,
	rs.expl_oom_count_sum, rs.expl_oom_bump_applied, rs.expl_cpu_floor_applied, rs.expl_mem_floor_applied, rs.expl_is_idle,
	rs.expl_gpu_sm_active_avg_bp, rs.expl_gpu_tensor_active_avg_bp, rs.expl_gpu_dram_active_avg_bp,
	rs.expl_gpu_fb_usage_max_mib, rs.expl_gpu_fb_p98_mib,
	rs.expl_gpu_recommended_profile, rs.expl_gpu_current_profile,
	rs.expl_gpu_has_profiling_data, rs.expl_gpu_memory_bound,
	rs.updated_at,
	c.source_id, c.cluster_alias, c.last_reported_at,
	c.analytics_incomplete, c.analytics_incomplete_at,
	c.ingest_hooks_failed, c.ingest_hooks_failed_at`

// GetNativeRecommendationByID fetches a single container's recommendations
// by its deterministic UUID using the indexed container_id column (O(1)).
func GetNativeRecommendationByID(orgID, id string, userPerms map[string][]string, includeExplanation bool) (*NativeContainerResult, error) {
	db := database.GetDB()

	query := nativeContainerDetailQuery(db, orgID, id, userPerms)

	sqlRows, err := query.Order("rs.term, rs.engine").Rows()
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()
	rows, err := scanNativeContainerRowsNoSort(sqlRows, 6)
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return nil, nil
	}

	results := assembleNativeResults(rows, "", includeExplanation)
	if len(results) == 0 {
		return nil, nil
	}
	attachPersistedGPUExplanations(&results[0], rows, includeExplanation)
	return &results[0], nil
}

// nativeContainerDetailQuery builds the primary detail lookup for a container recommendation.
// orgID is required: recommendation IDs are deterministic UUID v5 values that do not encode tenant scope.
func nativeContainerDetailQuery(db *gorm.DB, orgID, id string, userPerms map[string][]string) *gorm.DB {
	query := db.Table("recommendation_sets rs").
		Select(nativeDetailSelect).
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Where("rs.org_id = ?", orgID).
		Where("rs.container_id = ?", id).
		Where("rs.stale = false")
	return ApplyNativeRBAC(query, userPerms)
}

// assembleNativeResults groups flat rows into nested NativeContainerResult structs.
func assembleNativeResults(rows []NativeRecommendationRow, sortExpr string, includeExplanation bool) []NativeContainerResult {
	type containerKey struct {
		ClusterUUID   string
		Namespace     string
		Workload      string
		WorkloadType  string
		ContainerName string
	}

	// Pre-allocate: typically 6 rows per container (3 terms × 2 engines).
	estContainers := len(rows)/6 + 1
	orderKeys := make([]containerKey, 0, estContainers)
	grouped := make(map[containerKey][]NativeRecommendationRow, estContainers)

	for _, r := range rows {
		key := containerKey{r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType, r.ContainerName}
		if _, exists := grouped[key]; !exists {
			orderKeys = append(orderKeys, key)
			grouped[key] = make([]NativeRecommendationRow, 0, 6)
		}
		grouped[key] = append(grouped[key], r)
	}

	results := make([]NativeContainerResult, 0, len(orderKeys))
	for _, key := range orderKeys {
		rowGroup := grouped[key]
		first := rowGroup[0]

		var replicas *ReplicaInfo
		if first.PodCountMax != nil && *first.PodCountMax > 0 {
			replicas = &ReplicaInfo{
				Min: derefInt(first.PodCountMin),
				Max: derefInt(first.PodCountMax),
				Avg: derefInt(first.PodCountAvg),
			}
			if first.DesiredReplicas != nil && *first.DesiredReplicas > 0 {
				replicas.Desired = *first.DesiredReplicas
				replicas.Available = derefInt(first.AvailableReplicas)
				replicas.Source = "kube_state_metrics"
			} else {
				replicas.Source = "derived"
			}
		}

		var replicaOpt *ReplicaOptimization
		if first.RecommendedReplicas != nil && *first.RecommendedReplicas > 0 {
			opt := ReplicaOptimization{
				RecommendedReplicas: *first.RecommendedReplicas,
			}
			if first.ReplicaConfidence != nil {
				opt.Confidence = *first.ReplicaConfidence
			}
			if first.ReplicaExplanation != nil {
				opt.Explanation = *first.ReplicaExplanation
			}
			replicaOpt = &opt
		}

		var maxMonEnd time.Time
		for _, r := range rowGroup {
			if r.MonitoringEndTime != nil && r.MonitoringEndTime.After(maxMonEnd) {
				maxMonEnd = *r.MonitoringEndTime
			}
		}

		pageSort := nativeContainerParseSortText(sortExpr, first.PageSortText)
		result := NativeContainerResult{
			ID:                    NativeContainerID(first.ClusterUUID, first.Namespace, first.Workload, first.WorkloadType, first.ContainerName),
			ClusterAlias:          first.ClusterAlias,
			ClusterUUID:           first.ClusterUUID,
			Container:             first.ContainerName,
			Project:               first.Namespace,
			Workload:              first.Workload,
			WorkloadType:          first.WorkloadType,
			SourceID:              first.SourceID,
			LastReported:          first.LastReported.Format(time.RFC3339),
			AnalyticsIncomplete:   first.AnalyticsIncomplete,
			AnalyticsIncompleteAt: formatOptionalRFC3339(first.AnalyticsIncompleteAt),
			IngestHooksFailed:     first.IngestHooksFailed,
			IngestHooksFailedAt:   formatOptionalRFC3339(first.IngestHooksFailedAt),
			Replicas:              replicas,
			ReplicaOptimization:   replicaOpt,
			MonitoringEndTime:     maxMonEnd,
			Recommendations:       make(map[string]TermRecommendation),
			PaginationSort:        pageSort,
		}
		idleRow := pickIdleSourceRow(rowGroup)
		savingsEnabled := idleRow.EstimatedSavingsCents != nil || idleRow.EstimatedWasteCents != nil
		PopulateContainerIdleFields(
			&result,
			idleRow.IdleState,
			idleRow.IdleSince,
			idleRow.IdleDurationDays,
			idleRow.PeakCPUMillicores,
			idleRow.PeakMemoryBytes,
			idleRow.EstimatedWasteCents,
			savingsEnabled,
		)
		if result.IdleState == "active" {
			result.EstimatedMonthlySavings = money.FormatCentsToAmountPtr(first.EstimatedSavingsCents, money.DefaultCurrency)
			result.CPUSavings = money.FormatCentsToAmountPtr(first.EstimatedCPUSavingsCents, money.DefaultCurrency)
			result.MemorySavings = money.FormatCentsToAmountPtr(first.EstimatedMemSavingsCents, money.DefaultCurrency)
		}

		if first.Category != nil {
			result.Category = *first.Category
		}
		if first.CategoryCPU != nil {
			result.CategoryCPU = *first.CategoryCPU
		}
		if first.CategoryMemory != nil {
			result.CategoryMemory = *first.CategoryMemory
		}

		for _, r := range rowGroup {
			termKey := r.Term + "_term"
			term, exists := result.Recommendations[termKey]
			if !exists {
				term = TermRecommendation{}
			}

			codes := r.NotificationCodes
			if codes == nil {
				codes = SmallintArray{}
			}

			eng := &EngineRecommendation{
				CPURequestMillicores:   r.RecCPURequestMC,
				CPULimitMillicores:     r.RecCPULimitMC,
				MemRequestKiB:          r.RecMemRequestKiB,
				MemLimitKiB:            r.RecMemLimitKiB,
				CurrentCPURequestMC:    r.CurrentCPURequestMC,
				CurrentCPULimitMC:      r.CurrentCPULimitMC,
				CurrentMemRequestKiB:   r.CurrentMemRequestKiB,
				CurrentMemLimitKiB:     r.CurrentMemLimitKiB,
				VariationCPURequestPct: r.VariationCPURequestPct,
				VariationCPULimitPct:   r.VariationCPULimitPct,
				VariationMemRequestPct: r.VariationMemRequestPct,
				VariationMemLimitPct:   r.VariationMemLimitPct,
				ConfidenceLevel:        r.ConfidenceLevel,
				NotificationCodes:      codes,
			}

			switch r.Engine {
			case "cost":
				term.Cost = eng
			case "performance":
				term.Performance = eng
			}
			if includeExplanation {
				if expl := BuildContainerExplanationAPI(r); expl != nil {
					eng.Explanation = expl
				}
			}

			result.Recommendations[termKey] = term
		}

		results = append(results, result)
	}

	return results
}

func attachPersistedGPUExplanations(result *NativeContainerResult, rows []NativeRecommendationRow, includeExplanation bool) {
	if !includeExplanation || result == nil {
		return
	}
	for _, r := range rows {
		expl := BuildGPUExplanationAPI(
			r.ExplGPUSMActiveAvgBP, r.ExplGPUTensorActiveAvgBP, r.ExplGPUDRAMActiveAvgBP,
			r.ExplGPUFBUsageMaxMiB, r.ExplGPUFBP98MiB,
			r.ExplGPURecommendedProfile, r.ExplGPUCurrentProfile,
			r.ExplGPUHasProfilingData, r.ExplGPUMemoryBound,
		)
		if expl == nil {
			continue
		}
		if result.GPU == nil {
			result.GPU = make(map[string]*GPURecommendation)
		}
		termKey := r.Term
		if existing, ok := result.GPU[termKey]; ok {
			existing.Explanation = expl
		} else {
			result.GPU[termKey] = &GPURecommendation{Explanation: expl}
		}
	}
}

// pickIdleSourceRow prefers the medium-term cost engine row for idle metadata.
func pickIdleSourceRow(rows []NativeRecommendationRow) NativeRecommendationRow {
	for _, r := range rows {
		if r.Term == "medium" && r.Engine == "cost" {
			return r
		}
	}
	if len(rows) > 0 {
		return rows[0]
	}
	return NativeRecommendationRow{IdleState: "active"}
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func formatOptionalRFC3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := t.UTC().Format(time.RFC3339)
	return &formatted
}
