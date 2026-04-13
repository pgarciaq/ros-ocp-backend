package model

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
	"gorm.io/gorm"
)

var log *logrus.Entry = logging.GetLogger()

// Fixed namespace UUID for deterministic ID generation (UUID v5).
var nativeIDNamespace = uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479")

// SmallintArray implements sql.Scanner and driver.Valuer for PostgreSQL
// SMALLINT[] columns so GORM can read/write []int16 via database/sql.
type SmallintArray []int16

func (a *SmallintArray) Scan(src interface{}) error {
	if src == nil {
		*a = nil
		return nil
	}
	s, ok := src.(string)
	if !ok {
		if b, ok := src.([]byte); ok {
			s = string(b)
		} else {
			return fmt.Errorf("SmallintArray.Scan: unsupported type %T", src)
		}
	}
	s = strings.Trim(s, "{}")
	if s == "" {
		*a = nil
		return nil
	}
	parts := strings.Split(s, ",")
	result := make(SmallintArray, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 16)
		if err != nil {
			return fmt.Errorf("SmallintArray.Scan: parsing %q: %w", p, err)
		}
		result = append(result, int16(v))
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

	VariationCPURequestPct *float32 `gorm:"column:variation_cpu_request_pct"`
	VariationMemRequestPct *float32 `gorm:"column:variation_memory_request_pct"`
	ConfidenceLevel        *float32 `gorm:"column:confidence_level"`
	// SMALLINT[] caps values at 32767. Current notification codes (1-24) are
	// well within range. If legacy 6-digit codes are ever needed, this column
	// type must be migrated to INTEGER[].
	NotificationCodes SmallintArray `gorm:"column:notification_codes;type:smallint[]"`
	Stale             bool          `gorm:"column:stale"`

	UpdatedAt    time.Time `gorm:"column:updated_at"`
	SourceID     string    `gorm:"column:source_id"`
	ClusterAlias string    `gorm:"column:cluster_alias"`
	LastReported time.Time `gorm:"column:last_reported_at"`
}

func (NativeRecommendationRow) TableName() string {
	return "recommendation_sets"
}

// NativeContainerResult is the API-ready format for a single container,
// with all 6 recommendation variants nested.
type NativeContainerResult struct {
	ID              string                        `json:"id"`
	ClusterAlias    string                        `json:"cluster_alias"`
	ClusterUUID     string                        `json:"cluster_uuid"`
	Container       string                        `json:"container"`
	Project         string                        `json:"project"`
	Workload        string                        `json:"workload"`
	WorkloadType    string                        `json:"workload_type"`
	SourceID        string                        `json:"source_id"`
	LastReported    string                        `json:"last_reported"`
	Recommendations map[string]TermRecommendation `json:"recommendations"`
}

// NativeContainerID generates a deterministic UUID v5 from the composite key.
func NativeContainerID(clusterUUID, namespace, workload, container string) string {
	name := fmt.Sprintf("%s/%s/%s/%s", clusterUUID, namespace, workload, container)
	return uuid.NewSHA1(nativeIDNamespace, []byte(name)).String()
}

// TermRecommendation holds cost and performance recommendations for a term.
type TermRecommendation struct {
	Cost        *EngineRecommendation `json:"cost,omitempty"`
	Performance *EngineRecommendation `json:"performance,omitempty"`
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
	VariationCPURequestPct *float32                                   `json:"variation_cpu_request_pct,omitempty"`
	VariationMemRequestPct *float32                                   `json:"variation_memory_request_pct,omitempty"`
	ConfidenceLevel        *float32                                   `json:"confidence_level,omitempty"`
	NotificationCodes      SmallintArray                              `json:"notification_codes"`
	Notifications          map[string]notifications.NotificationEntry `json:"notifications"`
}

// GetNativeRecommendations queries the native relational columns from recommendation_sets.
func GetNativeRecommendations(orgID string, opts listoptions.ListOptions, queryParams map[string]interface{}, userPerms map[string][]string) ([]NativeContainerResult, int, error) {
	db := database.GetDB()

	query := db.Table("recommendation_sets rs").
		Select(`rs.org_id, rs.cluster_uuid, rs.namespace, rs.workload, rs.workload_type,
			rs.container_name, rs.term, rs.engine,
			rs.rec_cpu_request_millicores, rs.rec_cpu_limit_millicores,
			rs.rec_memory_request_kib, rs.rec_memory_limit_kib,
			rs.current_cpu_request_millicores, rs.current_cpu_limit_millicores,
			rs.current_memory_request_kib, rs.current_memory_limit_kib,
			rs.variation_cpu_request_pct, rs.variation_memory_request_pct,
			rs.notification_codes, rs.confidence_level, rs.stale, rs.updated_at,
			c.source_id, c.cluster_alias, c.last_reported_at`).
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Joins(`JOIN rh_accounts ra ON ra.id = c.tenant_id`).
		Where("ra.org_id = ?", orgID).
		Where("rs.stale = false")

	query = applyNativeRBAC(query, userPerms)
	query = applyQueryParams(query, queryParams)

	// Total count of distinct containers (for pagination metadata).
	var totalContainers int64
	countQuery := db.Table("recommendation_sets rs").
		Select("COUNT(DISTINCT (rs.cluster_uuid, rs.namespace, rs.workload, rs.container_name))").
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Joins(`JOIN rh_accounts ra ON ra.id = c.tenant_id`).
		Where("ra.org_id = ?", orgID).
		Where("rs.stale = false")
	countQuery = applyNativeRBAC(countQuery, userPerms)
	countQuery = applyQueryParams(countQuery, queryParams)
	t0 := time.Now()
	if err := countQuery.Scan(&totalContainers).Error; err != nil {
		return nil, 0, err
	}
	log.Infof("native list count query: %dms (%d containers)", time.Since(t0).Milliseconds(), totalContainers)

	var rows []NativeRecommendationRow
	err := query.
		Order("rs.namespace, rs.workload, rs.container_name, rs.term, rs.engine").
		Offset(opts.Offset * 6).Limit(opts.Limit * 6).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	results := assembleNativeResults(rows)
	return results, int(totalContainers), nil
}

// applyNativeRBAC adds RBAC-based WHERE clauses using the native schema's column names.
func applyNativeRBAC(query *gorm.DB, userPerms map[string][]string) *gorm.DB {
	cfg := config.GetConfig()
	if !cfg.RBACEnabled {
		return query
	}
	if _, ok := userPerms["*"]; ok {
		return query
	}

	clusterPerms, hasCluster := userPerms["openshift.cluster"]
	projectPerms, hasProject := userPerms["openshift.project"]
	clusterAll := hasCluster && utils.StringInSlice("*", clusterPerms)
	projectAll := hasProject && utils.StringInSlice("*", projectPerms)

	if hasCluster && !clusterAll {
		query = query.Where("c.cluster_uuid IN (?)", clusterPerms)
	}
	if hasProject && !projectAll {
		query = query.Where("rs.namespace IN (?)", projectPerms)
	}
	return query
}

// applyQueryParams adds dynamic WHERE clauses from the parsed query parameters.
// For []string values (IN clauses), the slice is passed directly to GORM
// so it expands into IN ($1, $2, ...) rather than a single scalar.
func applyQueryParams(query *gorm.DB, queryParams map[string]interface{}) *gorm.DB {
	for key, values := range queryParams {
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
	rs.variation_cpu_request_pct, rs.variation_memory_request_pct,
	rs.notification_codes, rs.confidence_level, rs.stale, rs.updated_at,
	c.source_id, c.cluster_alias, c.last_reported_at`

// GetNativeRecommendationByID fetches a single container's recommendations
// by its deterministic UUID. Primary path uses the indexed container_id column
// (O(1)). If that yields no rows — e.g. pre-migration data where container_id
// is NULL — falls back to a bounded composite-key scan.
func GetNativeRecommendationByID(orgID, id string, userPerms map[string][]string) (*NativeContainerResult, error) {
	db := database.GetDB()

	// Primary path: indexed lookup on container_id.
	query := db.Table("recommendation_sets rs").
		Select(nativeDetailSelect).
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Joins(`JOIN rh_accounts ra ON ra.id = c.tenant_id`).
		Where("ra.org_id = ?", orgID).
		Where("rs.container_id = ?", id).
		Where("rs.stale = false")
	query = applyNativeRBAC(query, userPerms)

	var rows []NativeRecommendationRow
	if err := query.Order("rs.term, rs.engine").Find(&rows).Error; err != nil {
		return nil, err
	}

	if len(rows) > 0 {
		results := assembleNativeResults(rows)
		if len(results) > 0 {
			return &results[0], nil
		}
	}

	// Fallback for pre-migration rows where container_id is NULL.
	// Scan a bounded window of distinct container keys and match by UUID v5.
	return getNativeRecommendationByIDFallback(db, orgID, id, userPerms)
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
		ContainerName string `gorm:"column:container_name"`
	}

	keysQuery := db.Table("recommendation_sets rs").
		Select("DISTINCT rs.cluster_uuid, rs.namespace, rs.workload, rs.container_name").
		Joins(`JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid`).
		Joins(`JOIN rh_accounts ra ON ra.id = c.tenant_id`).
		Where("ra.org_id = ?", orgID).
		Where("rs.stale = false").
		Limit(500)
	keysQuery = applyNativeRBAC(keysQuery, userPerms)

	var keys []containerKey
	if err := keysQuery.Find(&keys).Error; err != nil {
		return nil, err
	}

	var matched *containerKey
	for i := range keys {
		if NativeContainerID(keys[i].ClusterUUID, keys[i].Namespace, keys[i].Workload, keys[i].ContainerName) == id {
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

	results := assembleNativeResults(rows)
	if len(results) == 0 {
		return nil, nil
	}
	return &results[0], nil
}

// assembleNativeResults groups flat rows into nested NativeContainerResult structs.
func assembleNativeResults(rows []NativeRecommendationRow) []NativeContainerResult {
	type containerKey struct {
		ClusterUUID   string
		Namespace     string
		Workload      string
		ContainerName string
	}

	orderKeys := []containerKey{}
	grouped := map[containerKey][]NativeRecommendationRow{}

	for _, r := range rows {
		key := containerKey{r.ClusterUUID, r.Namespace, r.Workload, r.ContainerName}
		if _, exists := grouped[key]; !exists {
			orderKeys = append(orderKeys, key)
		}
		grouped[key] = append(grouped[key], r)
	}

	var results []NativeContainerResult
	for _, key := range orderKeys {
		rowGroup := grouped[key]
		first := rowGroup[0]

		result := NativeContainerResult{
			ID:              NativeContainerID(first.ClusterUUID, first.Namespace, first.Workload, first.ContainerName),
			ClusterAlias:    first.ClusterAlias,
			ClusterUUID:     first.ClusterUUID,
			Container:       first.ContainerName,
			Project:         first.Namespace,
			Workload:        first.Workload,
			WorkloadType:    first.WorkloadType,
			SourceID:        first.SourceID,
			LastReported:    first.LastReported.Format(time.RFC3339),
			Recommendations: make(map[string]TermRecommendation),
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
			if notifMap == nil {
				notifMap = map[string]notifications.NotificationEntry{}
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
				VariationMemRequestPct: r.VariationMemRequestPct,
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
