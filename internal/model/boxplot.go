package model

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// PlotDetails holds pre-aggregated percentile-band metrics for a single time bucket.
type PlotDetails struct {
	P50    float64 `json:"p50"`
	P95    float64 `json:"p95"`
	P99    float64 `json:"p99"`
	Max    float64 `json:"max"`
	Format string  `json:"format"`
}

// ThrottlePlotDetails holds CPU throttle metrics (only P95 and Max exist in digests).
type ThrottlePlotDetails struct {
	P95    float64 `json:"p95"`
	Max    float64 `json:"max"`
	Format string  `json:"format"`
}

// NativePlotsData holds CPU and memory plot data for a single time bucket.
type NativePlotsData struct {
	CPUUsage    *PlotDetails         `json:"cpuUsage,omitempty"`
	CPUThrottle *ThrottlePlotDetails `json:"cpuThrottle,omitempty"`
	MemoryUsage *PlotDetails         `json:"memoryUsage,omitempty"`
}

// NativePlot is the top-level plots structure matching the Kruize JSON shape.
type NativePlot struct {
	DataPoints int                        `json:"datapoints"`
	PlotsData  map[string]NativePlotsData `json:"plots_data"`
}

// ContainerKey identifies a unique container for plot queries.
type ContainerKey struct {
	OrgID         string
	ClusterUUID   string
	Namespace     string
	Workload      string
	WorkloadType  string
	ContainerName string
}

// dailyBucketKeyFormat is the Go time format for daily digest bucket keys in plots_data.
const dailyBucketKeyFormat = "2006-01-02T15:04:05.000Z"

// TermWindow defines the time window for a recommendation term.
type TermWindow struct {
	Name        string
	WindowHours int
}

// defaultTermWindows are the hardcoded defaults when no org overrides exist.
var defaultTermWindows = map[string]TermWindow{
	"short_term": {
		Name:        "short_term",
		WindowHours: 24,
	},
	"medium_term": {
		Name:        "medium_term",
		WindowHours: 7 * 24,
	},
	"long_term": {
		Name:        "long_term",
		WindowHours: 15 * 24,
	},
}

// termKeyNames maps term ordinals to API term key suffixes.
var termKeyNames = [3]string{"short", "medium", "long"}

func plotDetailsFromDigest(cpuP50, cpuP95, cpuP99, cpuMax, memP50, memP95, memP99, memMax, throttleP95, throttleMax float64) NativePlotsData {
	pd := NativePlotsData{
		CPUUsage: &PlotDetails{
			P50: cpuP50 / 1000.0, P95: cpuP95 / 1000.0, P99: cpuP99 / 1000.0,
			Max: cpuMax / 1000.0, Format: "cores",
		},
		MemoryUsage: &PlotDetails{
			P50: memP50 / 1024.0, P95: memP95 / 1024.0, P99: memP99 / 1024.0,
			Max: memMax / 1024.0, Format: "MiB",
		},
	}
	if config.VisualInsightsEnabled() && (throttleP95 != 0 || throttleMax != 0) {
		pd.CPUThrottle = &ThrottlePlotDetails{
			P95: throttleP95 / 1000.0, Max: throttleMax / 1000.0, Format: "cores",
		}
	}
	return pd
}

func dailyBucketKey(bucketDate time.Time) string {
	return bucketDate.UTC().Format(dailyBucketKeyFormat)
}

// loadTermWindows loads org-specific term configs from the database and builds
// TermWindow entries. Falls back to defaultTermWindows when no overrides exist.
func loadTermWindows(ctx context.Context, pool *pgxpool.Pool, orgID string) (map[string]TermWindow, error) {
	if pool == nil {
		return defaultTermWindows, nil
	}

	rows, err := pool.Query(ctx,
		`SELECT term_ord, window_days
		 FROM org_recommendation_terms
		 WHERE org_id = $1
		 ORDER BY term_ord`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var customs []struct {
		Ord        int
		WindowDays int
	}
	for rows.Next() {
		var c struct {
			Ord        int
			WindowDays int
		}
		if err := rows.Scan(&c.Ord, &c.WindowDays); err != nil {
			return nil, err
		}
		customs = append(customs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(customs) == 0 {
		return defaultTermWindows, nil
	}

	result := make(map[string]TermWindow, len(customs))
	for _, c := range customs {
		name := termKeyNames[c.Ord-1] + "_term"
		result[name] = TermWindow{
			Name:        name,
			WindowHours: c.WindowDays * 24,
		}
	}
	return result, nil
}

// AssembleBoxplots queries daily_container_digests and returns pre-aggregated
// percentile-band plot data per daily bucket.
func AssembleBoxplots(ctx context.Context, pool *pgxpool.Pool, key ContainerKey, termName string, orgID string) (*NativePlot, error) {
	windows, err := loadTermWindows(ctx, pool, orgID)
	if err != nil {
		return nil, fmt.Errorf("load term windows: %w", err)
	}
	tw, ok := windows[termName]
	if !ok {
		return nil, fmt.Errorf("unknown term: %s", termName)
	}

	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.Add(-time.Duration(tw.WindowHours) * time.Hour).Truncate(24 * time.Hour)

	rows, err := pool.Query(ctx, `
		SELECT bucket_date,
			cpu_usage_p50_mc::float8,
			cpu_usage_p95_mc::float8,
			cpu_usage_p99_mc::float8,
			cpu_usage_max_mc::float8,
			memory_usage_p50_kib::float8,
			memory_usage_p95_kib::float8,
			memory_usage_p99_kib::float8,
			memory_usage_max_kib::float8,
			COALESCE(cpu_throttle_p95_mc, 0)::float8,
			COALESCE(cpu_throttle_max_mc, 0)::float8
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2
			AND namespace = $3 AND workload = $4
			AND workload_type = $5 AND container_name = $6
			AND bucket_date >= $7 AND bucket_date <= $8
			AND schedule_type = 'all_hours'
		ORDER BY bucket_date`,
		key.OrgID, key.ClusterUUID, key.Namespace, key.Workload, key.WorkloadType, key.ContainerName,
		start, end)
	if err != nil {
		return nil, fmt.Errorf("AssembleBoxplots query: %w", err)
	}
	defer rows.Close()

	plotsData := map[string]NativePlotsData{}
	for rows.Next() {
		var bucket time.Time
		var cpuP50, cpuP95, cpuP99, cpuMax float64
		var memP50, memP95, memP99, memMax float64
		var throttleP95, throttleMax float64

		if err := rows.Scan(&bucket,
			&cpuP50, &cpuP95, &cpuP99, &cpuMax,
			&memP50, &memP95, &memP99, &memMax,
			&throttleP95, &throttleMax); err != nil {
			return nil, fmt.Errorf("AssembleBoxplots scan: %w", err)
		}

		plotsData[dailyBucketKey(bucket)] = plotDetailsFromDigest(
			cpuP50, cpuP95, cpuP99, cpuMax, memP50, memP95, memP99, memMax,
			throttleP95, throttleMax)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("AssembleBoxplots rows: %w", err)
	}

	if len(plotsData) == 0 {
		return nil, nil
	}

	return &NativePlot{
		DataPoints: len(plotsData),
		PlotsData:  plotsData,
	}, nil
}

// AssembleAllTermBoxplots computes plots for multiple terms in a single query (UNION ALL).
func AssembleAllTermBoxplots(ctx context.Context, pool *pgxpool.Pool, key ContainerKey, termNames []string, orgID string) (map[string]*NativePlot, error) {
	if pool == nil || len(termNames) == 0 {
		return map[string]*NativePlot{}, nil
	}
	windows, err := loadTermWindows(ctx, pool, orgID)
	if err != nil {
		return nil, fmt.Errorf("load term windows: %w", err)
	}

	end := time.Now().UTC().Truncate(24 * time.Hour)
	var unionParts []string
	args := []any{key.OrgID, key.ClusterUUID, key.Namespace, key.Workload, key.WorkloadType, key.ContainerName, end}
	argN := 8

	for _, termName := range termNames {
		tw, ok := windows[termName]
		if !ok {
			continue
		}
		start := end.Add(-time.Duration(tw.WindowHours) * time.Hour).Truncate(24 * time.Hour)
		unionParts = append(unionParts, fmt.Sprintf(`
			SELECT '%s' AS term_name,
				bucket_date,
				cpu_usage_p50_mc::float8,
				cpu_usage_p95_mc::float8,
				cpu_usage_p99_mc::float8,
				cpu_usage_max_mc::float8,
				memory_usage_p50_kib::float8,
				memory_usage_p95_kib::float8,
				memory_usage_p99_kib::float8,
				memory_usage_max_kib::float8,
				COALESCE(cpu_throttle_p95_mc, 0)::float8,
				COALESCE(cpu_throttle_max_mc, 0)::float8
			FROM daily_container_digests
			WHERE org_id = $1 AND cluster_uuid = $2
				AND namespace = $3 AND workload = $4
				AND workload_type = $5 AND container_name = $6
				AND bucket_date >= $%d AND bucket_date <= $7
				AND schedule_type = 'all_hours'`, termName, argN))
		args = append(args, start)
		argN++
	}

	if len(unionParts) == 0 {
		return map[string]*NativePlot{}, nil
	}

	query := strings.Join(unionParts, " UNION ALL ") + " ORDER BY term_name, bucket_date"
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("AssembleAllTermBoxplots query: %w", err)
	}
	defer rows.Close()

	out := make(map[string]*NativePlot)
	plotsByTerm := make(map[string]map[string]NativePlotsData)
	for rows.Next() {
		var termName string
		var bucket time.Time
		var cpuP50, cpuP95, cpuP99, cpuMax float64
		var memP50, memP95, memP99, memMax float64
		var throttleP95, throttleMax float64
		if err := rows.Scan(&termName, &bucket,
			&cpuP50, &cpuP95, &cpuP99, &cpuMax,
			&memP50, &memP95, &memP99, &memMax,
			&throttleP95, &throttleMax); err != nil {
			return nil, fmt.Errorf("AssembleAllTermBoxplots scan: %w", err)
		}
		if plotsByTerm[termName] == nil {
			plotsByTerm[termName] = map[string]NativePlotsData{}
		}
		plotsByTerm[termName][dailyBucketKey(bucket)] = plotDetailsFromDigest(
			cpuP50, cpuP95, cpuP99, cpuMax, memP50, memP95, memP99, memMax,
			throttleP95, throttleMax)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("AssembleAllTermBoxplots rows: %w", err)
	}

	for termName, plotsData := range plotsByTerm {
		if len(plotsData) == 0 {
			continue
		}
		out[termName] = &NativePlot{DataPoints: len(plotsData), PlotsData: plotsData}
	}
	return out, nil
}

// NamespaceKey identifies a unique namespace for plot queries.
type NamespaceKey struct {
	OrgID       string
	ClusterUUID string
	Namespace   string
}

// AssembleNamespaceBoxplots queries daily_namespace_digests and returns
// pre-aggregated percentile-band plot data per daily bucket.
func AssembleNamespaceBoxplots(ctx context.Context, pool *pgxpool.Pool, key NamespaceKey, termName string, orgID string) (*NativePlot, error) {
	windows, err := loadTermWindows(ctx, pool, orgID)
	if err != nil {
		return nil, fmt.Errorf("load term windows: %w", err)
	}
	tw, ok := windows[termName]
	if !ok {
		return nil, fmt.Errorf("unknown term: %s", termName)
	}

	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.Add(-time.Duration(tw.WindowHours) * time.Hour).Truncate(24 * time.Hour)

	rows, err := pool.Query(ctx, `
		SELECT bucket_date,
			cpu_usage_p50_mc::float8,
			cpu_usage_p95_mc::float8,
			cpu_usage_p99_mc::float8,
			cpu_usage_max_mc::float8,
			memory_usage_p50_kib::float8,
			memory_usage_p95_kib::float8,
			memory_usage_p99_kib::float8,
			memory_usage_max_kib::float8
		FROM daily_namespace_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3
			AND bucket_date >= $4 AND bucket_date <= $5
			AND schedule_type = 'all_hours'
		ORDER BY bucket_date`,
		key.OrgID, key.ClusterUUID, key.Namespace, start, end)
	if err != nil {
		return nil, fmt.Errorf("AssembleNamespaceBoxplots query: %w", err)
	}
	defer rows.Close()

	plotsData := map[string]NativePlotsData{}
	for rows.Next() {
		var bucket time.Time
		var cpuP50, cpuP95, cpuP99, cpuMax float64
		var memP50, memP95, memP99, memMax float64

		if err := rows.Scan(&bucket,
			&cpuP50, &cpuP95, &cpuP99, &cpuMax,
			&memP50, &memP95, &memP99, &memMax); err != nil {
			return nil, fmt.Errorf("AssembleNamespaceBoxplots scan: %w", err)
		}

		plotsData[dailyBucketKey(bucket)] = plotDetailsFromDigest(
			cpuP50, cpuP95, cpuP99, cpuMax, memP50, memP95, memP99, memMax, 0, 0)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("AssembleNamespaceBoxplots rows: %w", err)
	}

	if len(plotsData) == 0 {
		return nil, nil
	}

	return &NativePlot{
		DataPoints: len(plotsData),
		PlotsData:  plotsData,
	}, nil
}

// AssembleAllTermNamespaceBoxplots computes namespace plots for multiple terms in one query.
func AssembleAllTermNamespaceBoxplots(ctx context.Context, pool *pgxpool.Pool, key NamespaceKey, termNames []string, orgID string) (map[string]*NativePlot, error) {
	if pool == nil || len(termNames) == 0 {
		return map[string]*NativePlot{}, nil
	}
	windows, err := loadTermWindows(ctx, pool, orgID)
	if err != nil {
		return nil, fmt.Errorf("load term windows: %w", err)
	}

	end := time.Now().UTC().Truncate(24 * time.Hour)
	var unionParts []string
	args := []any{key.OrgID, key.ClusterUUID, key.Namespace, end}
	argN := 5

	for _, termName := range termNames {
		tw, ok := windows[termName]
		if !ok {
			continue
		}
		start := end.Add(-time.Duration(tw.WindowHours) * time.Hour).Truncate(24 * time.Hour)
		unionParts = append(unionParts, fmt.Sprintf(`
			SELECT '%s' AS term_name,
				bucket_date,
				cpu_usage_p50_mc::float8,
				cpu_usage_p95_mc::float8,
				cpu_usage_p99_mc::float8,
				cpu_usage_max_mc::float8,
				memory_usage_p50_kib::float8,
				memory_usage_p95_kib::float8,
				memory_usage_p99_kib::float8,
				memory_usage_max_kib::float8
			FROM daily_namespace_digests
			WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3
				AND bucket_date >= $%d AND bucket_date <= $4
				AND schedule_type = 'all_hours'`, termName, argN))
		args = append(args, start)
		argN++
	}

	if len(unionParts) == 0 {
		return map[string]*NativePlot{}, nil
	}

	query := strings.Join(unionParts, " UNION ALL ") + " ORDER BY term_name, bucket_date"
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("AssembleAllTermNamespaceBoxplots query: %w", err)
	}
	defer rows.Close()

	out := make(map[string]*NativePlot)
	plotsByTerm := make(map[string]map[string]NativePlotsData)
	for rows.Next() {
		var termName string
		var bucket time.Time
		var cpuP50, cpuP95, cpuP99, cpuMax float64
		var memP50, memP95, memP99, memMax float64
		if err := rows.Scan(&termName, &bucket,
			&cpuP50, &cpuP95, &cpuP99, &cpuMax,
			&memP50, &memP95, &memP99, &memMax); err != nil {
			return nil, fmt.Errorf("AssembleAllTermNamespaceBoxplots scan: %w", err)
		}
		if plotsByTerm[termName] == nil {
			plotsByTerm[termName] = map[string]NativePlotsData{}
		}
		plotsByTerm[termName][dailyBucketKey(bucket)] = plotDetailsFromDigest(
			cpuP50, cpuP95, cpuP99, cpuMax, memP50, memP95, memP99, memMax, 0, 0)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("AssembleAllTermNamespaceBoxplots rows: %w", err)
	}

	for termName, plotsData := range plotsByTerm {
		if len(plotsData) == 0 {
			continue
		}
		out[termName] = &NativePlot{DataPoints: len(plotsData), PlotsData: plotsData}
	}
	return out, nil
}

// AssembleAllTermBoxplotsBH computes business-hours plots for multiple terms in a single query.
// Identical to AssembleAllTermBoxplots except schedule_type = 'business_hours'.
func AssembleAllTermBoxplotsBH(ctx context.Context, pool *pgxpool.Pool, key ContainerKey, termNames []string, orgID string) (map[string]*NativePlot, error) {
	if pool == nil || len(termNames) == 0 {
		return map[string]*NativePlot{}, nil
	}
	windows, err := loadTermWindows(ctx, pool, orgID)
	if err != nil {
		return nil, fmt.Errorf("load term windows: %w", err)
	}

	end := time.Now().UTC().Truncate(24 * time.Hour)
	var unionParts []string
	args := []any{key.OrgID, key.ClusterUUID, key.Namespace, key.Workload, key.WorkloadType, key.ContainerName, end}
	argN := 8

	for _, termName := range termNames {
		tw, ok := windows[termName]
		if !ok {
			continue
		}
		start := end.Add(-time.Duration(tw.WindowHours) * time.Hour).Truncate(24 * time.Hour)
		unionParts = append(unionParts, fmt.Sprintf(`
			SELECT '%s' AS term_name,
				bucket_date,
				cpu_usage_p50_mc::float8,
				cpu_usage_p95_mc::float8,
				cpu_usage_p99_mc::float8,
				cpu_usage_max_mc::float8,
				memory_usage_p50_kib::float8,
				memory_usage_p95_kib::float8,
				memory_usage_p99_kib::float8,
				memory_usage_max_kib::float8,
				COALESCE(cpu_throttle_p95_mc, 0)::float8,
				COALESCE(cpu_throttle_max_mc, 0)::float8
			FROM daily_container_digests
			WHERE org_id = $1 AND cluster_uuid = $2
				AND namespace = $3 AND workload = $4
				AND workload_type = $5 AND container_name = $6
				AND bucket_date >= $%d AND bucket_date <= $7
				AND schedule_type = 'business_hours'`, termName, argN))
		args = append(args, start)
		argN++
	}

	if len(unionParts) == 0 {
		return map[string]*NativePlot{}, nil
	}

	query := strings.Join(unionParts, " UNION ALL ") + " ORDER BY term_name, bucket_date"
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("AssembleAllTermBoxplotsBH query: %w", err)
	}
	defer rows.Close()

	out := make(map[string]*NativePlot)
	plotsByTerm := make(map[string]map[string]NativePlotsData)
	for rows.Next() {
		var termName string
		var bucket time.Time
		var cpuP50, cpuP95, cpuP99, cpuMax float64
		var memP50, memP95, memP99, memMax float64
		var throttleP95, throttleMax float64
		if err := rows.Scan(&termName, &bucket,
			&cpuP50, &cpuP95, &cpuP99, &cpuMax,
			&memP50, &memP95, &memP99, &memMax,
			&throttleP95, &throttleMax); err != nil {
			return nil, fmt.Errorf("AssembleAllTermBoxplotsBH scan: %w", err)
		}
		if plotsByTerm[termName] == nil {
			plotsByTerm[termName] = map[string]NativePlotsData{}
		}
		plotsByTerm[termName][dailyBucketKey(bucket)] = plotDetailsFromDigest(
			cpuP50, cpuP95, cpuP99, cpuMax, memP50, memP95, memP99, memMax,
			throttleP95, throttleMax)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("AssembleAllTermBoxplotsBH rows: %w", err)
	}

	for termName, plotsData := range plotsByTerm {
		if len(plotsData) == 0 {
			continue
		}
		out[termName] = &NativePlot{DataPoints: len(plotsData), PlotsData: plotsData}
	}
	return out, nil
}

// AssembleAllTermNamespaceBoxplotsBH computes business-hours namespace plots for multiple terms.
// Identical to AssembleAllTermNamespaceBoxplots except schedule_type = 'business_hours'.
func AssembleAllTermNamespaceBoxplotsBH(ctx context.Context, pool *pgxpool.Pool, key NamespaceKey, termNames []string, orgID string) (map[string]*NativePlot, error) {
	if pool == nil || len(termNames) == 0 {
		return map[string]*NativePlot{}, nil
	}
	windows, err := loadTermWindows(ctx, pool, orgID)
	if err != nil {
		return nil, fmt.Errorf("load term windows: %w", err)
	}

	end := time.Now().UTC().Truncate(24 * time.Hour)
	var unionParts []string
	args := []any{key.OrgID, key.ClusterUUID, key.Namespace, end}
	argN := 5

	for _, termName := range termNames {
		tw, ok := windows[termName]
		if !ok {
			continue
		}
		start := end.Add(-time.Duration(tw.WindowHours) * time.Hour).Truncate(24 * time.Hour)
		unionParts = append(unionParts, fmt.Sprintf(`
			SELECT '%s' AS term_name,
				bucket_date,
				cpu_usage_p50_mc::float8,
				cpu_usage_p95_mc::float8,
				cpu_usage_p99_mc::float8,
				cpu_usage_max_mc::float8,
				memory_usage_p50_kib::float8,
				memory_usage_p95_kib::float8,
				memory_usage_p99_kib::float8,
				memory_usage_max_kib::float8
			FROM daily_namespace_digests
			WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3
				AND bucket_date >= $%d AND bucket_date <= $4
				AND schedule_type = 'business_hours'`, termName, argN))
		args = append(args, start)
		argN++
	}

	if len(unionParts) == 0 {
		return map[string]*NativePlot{}, nil
	}

	query := strings.Join(unionParts, " UNION ALL ") + " ORDER BY term_name, bucket_date"
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("AssembleAllTermNamespaceBoxplotsBH query: %w", err)
	}
	defer rows.Close()

	out := make(map[string]*NativePlot)
	plotsByTerm := make(map[string]map[string]NativePlotsData)
	for rows.Next() {
		var termName string
		var bucket time.Time
		var cpuP50, cpuP95, cpuP99, cpuMax float64
		var memP50, memP95, memP99, memMax float64
		if err := rows.Scan(&termName, &bucket,
			&cpuP50, &cpuP95, &cpuP99, &cpuMax,
			&memP50, &memP95, &memP99, &memMax); err != nil {
			return nil, fmt.Errorf("AssembleAllTermNamespaceBoxplotsBH scan: %w", err)
		}
		if plotsByTerm[termName] == nil {
			plotsByTerm[termName] = map[string]NativePlotsData{}
		}
		plotsByTerm[termName][dailyBucketKey(bucket)] = plotDetailsFromDigest(
			cpuP50, cpuP95, cpuP99, cpuMax, memP50, memP95, memP99, memMax, 0, 0)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("AssembleAllTermNamespaceBoxplotsBH rows: %w", err)
	}

	for termName, plotsData := range plotsByTerm {
		if len(plotsData) == 0 {
			continue
		}
		out[termName] = &NativePlot{DataPoints: len(plotsData), PlotsData: plotsData}
	}
	return out, nil
}

// NamespaceMonitoringEndTime returns the most recent bucket_date from
// daily_namespace_digests for the given namespace. Returns zero time if no data.
func NamespaceMonitoringEndTime(ctx context.Context, pool *pgxpool.Pool, key NamespaceKey) (time.Time, error) {
	var maxDate time.Time
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(bucket_date), '0001-01-01')
		FROM daily_namespace_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3`,
		key.OrgID, key.ClusterUUID, key.Namespace,
	).Scan(&maxDate)
	if err != nil {
		return time.Time{}, fmt.Errorf("NamespaceMonitoringEndTime: %w", err)
	}
	return maxDate, nil
}

// MonitoringEndTime returns the most recent bucket_date from daily_container_digests
// for the given container. Returns zero time if no data exists.
func MonitoringEndTime(ctx context.Context, pool *pgxpool.Pool, key ContainerKey) (time.Time, error) {
	var maxDate time.Time
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(bucket_date), '0001-01-01')
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3
		  AND workload = $4 AND container_name = $5`,
		key.OrgID, key.ClusterUUID, key.Namespace, key.Workload, key.ContainerName,
	).Scan(&maxDate)
	if err != nil {
		return time.Time{}, fmt.Errorf("MonitoringEndTime: %w", err)
	}
	return maxDate, nil
}
