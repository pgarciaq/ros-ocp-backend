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
// filter by org_id (via rh_accounts or recommendation_sets.org_id) so one tenant cannot
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

	EstimatedSavingsCents *int64 `gorm:"column:estimated_savings_cents"`

	IdleState           string     `gorm:"column:idle_state"`
	IdleSince           *time.Time `gorm:"column:idle_since"`
	IdleDurationDays    *int       `gorm:"column:idle_duration_days"`
	PeakCPUMillicores   *int64     `gorm:"column:peak_cpu_millicores"`
	PeakMemoryBytes     *int64     `gorm:"column:peak_memory_bytes"`
	EstimatedWasteCents *int64     `gorm:"column:estimated_waste_cents"`

	RecommendationAppliedAt *time.Time `gorm:"column:recommendation_applied_at"`

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
	EstimatedMonthlySavings *money.MoneyAmount          `json:"estimated_monthly_savings,omitempty"`
	EstimatedMonthlyWaste   *money.MoneyAmount          `json:"estimated_monthly_waste,omitempty"`
	Currency                string                        `json:"currency,omitempty"`
	IdleState               string                        `json:"idle_state"`
	IdleSince               *string                       `json:"idle_since,omitempty"`
	IdleDurationDays        *int                          `json:"idle_duration_days,omitempty"`
	PeakCPUMillicores       *int64                        `json:"peak_cpu_millicores,omitempty"`
	PeakMemoryBytes         *int64                        `json:"peak_memory_bytes,omitempty"`
	IdleRecommendation      *IdleRecommendation           `json:"idle_recommendation,omitempty"`
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
}

// GetNativeRecommendations queries the native relational columns from recommendation_sets.
func GetNativeRecommendations(orgID string, opts listoptions.ListOptions, queryParams map[string]interface{}, userPerms map[string][]string) (NativeListPage, error) {
	// Tag filters require org_container_keys (resolved_tags / Koku tag joins). The legacy DISTINCT
	// path on recommendation_sets alone cannot apply tag predicates.
	if usesOrgContainerKeys(queryParams) || len(TagFiltersFromParams(queryParams)) > 0 {
		return getNativeRecommendationsFromOrgKeys(orgID, opts, queryParams, userPerms)
	}
	return getNativeRecommendationsDistinct(orgID, opts, queryParams, userPerms)
}

func getNativeRecommendationsFromOrgKeys(orgID string, opts listoptions.ListOptions, queryParams map[string]interface{}, userPerms map[string][]string) (NativeListPage, error) {
	db := database.GetDB()
	keysParams, detailParams := splitNativeListQueryParams(queryParams)

	limit := opts.Limit
	if opts.Format == listoptions.ResponseFormatCSV {
		limit = config.GetConfig().RecordLimitCSV
	}
	pageLimit := limit + 1

	sortExpr, sortFilter := nativeContainerPageSortExpr(opts.OrderBy)
	orderHow := nativeContainerOrderHow(opts.OrderHow)

	ockExists := db.Table("org_container_keys ock").
		Select("1").
		Where("ock.org_id = ?", orgID).
		Where("ock.cluster_uuid = rs.cluster_uuid").
		Where("ock.namespace = rs.namespace").
		Where("ock.workload = rs.workload").
		Where("ock.workload_type = rs.workload_type").
		Where("ock.container_name = rs.container_name")
	ockExists = ApplyNativeRBAC(ockExists, userPerms, "ock.namespace")
	ockExists = ApplyQueryParamsToKeys(ockExists, keysParams)
	if tagFilters := TagFiltersFromParams(queryParams); len(tagFilters) > 0 {
		ockExists = ApplyTagFiltersToKeys(ockExists, orgID, tagFilters)
	}

	distinctSubquery := db.Table("recommendation_sets rs").
		Select(fmt.Sprintf(
			"DISTINCT ON (rs.cluster_uuid, rs.namespace, rs.workload, rs.workload_type, rs.container_name) rs.cluster_uuid, rs.namespace, rs.workload, rs.workload_type, rs.container_name, (%s)::text AS ros_container_page_sort",
			sortExpr,
		)).
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Where("rs.org_id = ?", orgID).
		Where("rs.stale = false").
		Where("EXISTS (?)", ockExists)
	distinctSubquery = ApplyNativeRBAC(distinctSubquery, userPerms)
	distinctSubquery = ApplyQueryParams(distinctSubquery, detailParams)
	if sortFilter != "" {
		distinctSubquery = distinctSubquery.Where(sortFilter)
	}
	distinctSubquery = distinctSubquery.Order(nativeContainerDistinctOnOrder(sortExpr, orderHow))
	distinctSubquery = applyNativeContainerPageSeek(distinctSubquery, opts, sortExpr)

	countQuery := db.Table("org_container_keys ock").
		Select("ock.cluster_uuid, ock.namespace, ock.workload, ock.workload_type, ock.container_name").
		Joins("JOIN clusters c ON c.cluster_uuid = ock.cluster_uuid").
		Where("ock.org_id = ?", orgID)
	countQuery = ApplyNativeRBAC(countQuery, userPerms, "ock.namespace")
	countQuery = ApplyQueryParamsToKeys(countQuery, keysParams)
	if tagFilters := TagFiltersFromParams(queryParams); len(tagFilters) > 0 {
		countQuery = ApplyTagFiltersToKeys(countQuery, orgID, tagFilters)
	}

	pageSubquery := db.Table("(?) AS page", distinctSubquery).
		Select("page.cluster_uuid, page.namespace, page.workload, page.workload_type, page.container_name, page.ros_container_page_sort").
		Order(nativeContainerPageOrder("page", orderHow))
	if !opts.HasCursor {
		pageSubquery = pageSubquery.Offset(opts.Offset)
	}
	pageSubquery = pageSubquery.Limit(pageLimit)

	var rows []NativeRecommendationRow
	t0 := time.Now().UTC()
	detailQuery := db.Table("recommendation_sets rs").
		Select(nativeDetailSelect+", page.ros_container_page_sort").
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Joins(`JOIN (?) page ON page.cluster_uuid = rs.cluster_uuid
			AND page.namespace = rs.namespace
			AND page.workload = rs.workload
			AND page.workload_type = rs.workload_type
			AND page.container_name = rs.container_name`, pageSubquery).
		Where("rs.stale = false")
	detailQuery = ApplyQueryParams(detailQuery, detailParams)
	err := detailQuery.Order(nativeContainerDetailOrder(orderHow)).Find(&rows).Error
	if err != nil {
		return NativeListPage{}, err
	}

	results := assembleNativeResults(rows, sortExpr)

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

func getNativeRecommendationsDistinct(orgID string, opts listoptions.ListOptions, queryParams map[string]interface{}, userPerms map[string][]string) (NativeListPage, error) {
	db := database.GetDB()
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
		Where("rs.org_id = ?", orgID).
		Where("rs.stale = false")
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

	var rows []NativeRecommendationRow
	t0 := time.Now().UTC()
	detailQuery := db.Table("recommendation_sets rs").
		Select(nativeDetailSelect+", page.ros_container_page_sort").
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Joins(`JOIN (?) page ON page.cluster_uuid = rs.cluster_uuid
			AND page.namespace = rs.namespace
			AND page.workload = rs.workload
			AND page.workload_type = rs.workload_type
			AND page.container_name = rs.container_name`, pageSubquery).
		Where("rs.stale = false")
	detailQuery = ApplyQueryParams(detailQuery, queryParams)
	err := detailQuery.Order(nativeContainerDetailOrder(orderHow)).Find(&rows).Error
	if err != nil {
		return NativeListPage{}, err
	}

	results := assembleNativeResults(rows, sortExpr)

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
	rs.estimated_savings_cents,
	rs.idle_state, rs.idle_since, rs.idle_duration_days,
	rs.peak_cpu_millicores, rs.peak_memory_bytes, rs.estimated_waste_cents,
	rs.monitoring_end_time,
	rs.updated_at,
	c.source_id, c.cluster_alias, c.last_reported_at,
	c.analytics_incomplete, c.analytics_incomplete_at,
	c.ingest_hooks_failed, c.ingest_hooks_failed_at`

// GetNativeRecommendationByID fetches a single container's recommendations
// by its deterministic UUID. Primary path uses the indexed container_id column
// (O(1)). If that yields no rows — e.g. pre-migration data where container_id
// is NULL — falls back to a bounded composite-key scan.
func GetNativeRecommendationByID(orgID, id string, userPerms map[string][]string) (*NativeContainerResult, error) {
	db := database.GetDB()

	query := nativeContainerDetailQuery(db, orgID, id, userPerms)

	var rows []NativeRecommendationRow
	if err := query.Order("rs.term, rs.engine").Find(&rows).Error; err != nil {
		return nil, err
	}

	if len(rows) > 0 {
		results := assembleNativeResults(rows, "")
		if len(results) > 0 {
			return &results[0], nil
		}
	}

	// Fallback for pre-migration rows where container_id is NULL.
	// TODO: remove getNativeRecommendationByIDFallback after container_id backfill verified in production.
	return getNativeRecommendationByIDFallback(db, orgID, id, userPerms)
}

// nativeContainerDetailQuery builds the primary detail lookup for a container recommendation.
// orgID is required: recommendation IDs are deterministic UUID v5 values that do not encode tenant scope.
func nativeContainerDetailQuery(db *gorm.DB, orgID, id string, userPerms map[string][]string) *gorm.DB {
	query := db.Table("recommendation_sets rs").
		Select(nativeDetailSelect).
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Joins(`JOIN rh_accounts ra ON ra.id = c.tenant_id`).
		Where("ra.org_id = ?", orgID).
		Where("rs.container_id = ?", id).
		Where("rs.stale = false")
	return ApplyNativeRBAC(query, userPerms)
}

// getNativeRecommendationByIDFallback scans up to 500 distinct container keys
// for the org, computes the UUID v5 for each, and fetches the matching container.
// This path is only hit for rows written before migration 028 populated
// container_id. Once all data is reprocessed, this code path becomes dead.
func getNativeRecommendationByIDFallback(db *gorm.DB, orgID, id string, userPerms map[string][]string) (*NativeContainerResult, error) {
	log.Warnf("container_id miss for %s in org %s — using fallback scan (pre-migration data)", id, orgID)

	type containerKey struct {
		ClusterUUID   string `gorm:"column:cluster_uuid"`
		Namespace     string `gorm:"column:namespace"`
		Workload      string `gorm:"column:workload"`
		WorkloadType  string `gorm:"column:workload_type"`
		ContainerName string `gorm:"column:container_name"`
	}

	keysQuery := db.Table("recommendation_sets rs").
		Select("DISTINCT rs.cluster_uuid, rs.namespace, rs.workload, rs.workload_type, rs.container_name").
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Joins(`JOIN rh_accounts ra ON ra.id = c.tenant_id`).
		Where("ra.org_id = ?", orgID).
		Where("rs.stale = false").
		Limit(500)
	keysQuery = ApplyNativeRBAC(keysQuery, userPerms)

	var keys []containerKey
	if err := keysQuery.Find(&keys).Error; err != nil {
		return nil, err
	}

	var matched *containerKey
	for i := range keys {
		if NativeContainerID(keys[i].ClusterUUID, keys[i].Namespace, keys[i].Workload, keys[i].WorkloadType, keys[i].ContainerName) == id {
			matched = &keys[i]
			break
		}
	}
	if matched == nil {
		return nil, nil
	}

	var rows []NativeRecommendationRow
	err := db.Table("recommendation_sets rs").
		Select(nativeDetailSelect).
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Where("rs.org_id = ?", orgID).
		Where("rs.cluster_uuid = ?", matched.ClusterUUID).
		Where("rs.namespace = ?", matched.Namespace).
		Where("rs.workload = ?", matched.Workload).
		Where("rs.container_name = ?", matched.ContainerName).
		Where("rs.stale = false").
		Order("rs.term, rs.engine").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	results := assembleNativeResults(rows, "")
	if len(results) == 0 {
		return nil, nil
	}
	return &results[0], nil
}

// assembleNativeResults groups flat rows into nested NativeContainerResult structs.
func assembleNativeResults(rows []NativeRecommendationRow, sortExpr string) []NativeContainerResult {
	type containerKey struct {
		ClusterUUID   string
		Namespace     string
		Workload      string
		WorkloadType  string
		ContainerName string
	}

	orderKeys := []containerKey{}
	grouped := map[containerKey][]NativeRecommendationRow{}

	for _, r := range rows {
		key := containerKey{r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType, r.ContainerName}
		if _, exists := grouped[key]; !exists {
			orderKeys = append(orderKeys, key)
		}
		grouped[key] = append(grouped[key], r)
	}

	var results []NativeContainerResult
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
			notifMap := notifications.MapToKruizeFormat(r.NotificationCodes)

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
				Notifications:          notifMap,
			}

			switch r.Engine {
			case "cost":
				term.Cost = eng
			case "performance":
				term.Performance = eng
			}

			result.Recommendations[termKey] = term
		}

		results = append(results, result)
	}

	return results
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
