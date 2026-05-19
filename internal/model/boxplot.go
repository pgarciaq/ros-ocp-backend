package model

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BoxPlotDetails holds the five-number summary for a single metric in a bucket.
type BoxPlotDetails struct {
	Min    float64 `json:"min"`
	Q1     float64 `json:"q1"`
	Median float64 `json:"median"`
	Q3     float64 `json:"q3"`
	Max    float64 `json:"max"`
	Format string  `json:"format"`
}

// NativePlotsData holds CPU and memory boxplot data for a single time bucket.
type NativePlotsData struct {
	CPUUsage    *BoxPlotDetails `json:"cpuUsage,omitempty"`
	MemoryUsage *BoxPlotDetails `json:"memoryUsage,omitempty"`
}

// NativePlot is the top-level plots structure matching the Kruize JSON shape.
type NativePlot struct {
	DataPoints int                        `json:"datapoints"`
	PlotsData  map[string]NativePlotsData `json:"plots_data"`
}

// ContainerKey identifies a unique container for boxplot queries.
type ContainerKey struct {
	OrgID         string
	ClusterUUID   string
	Namespace     string
	Workload      string
	ContainerName string
}

// BucketGranularity constrains the SQL bucketing to known-safe expressions.
type BucketGranularity int

const (
	BucketGranularity6Hour BucketGranularity = iota
	BucketGranularityDaily
)

func (bg BucketGranularity) sql() string {
	switch bg {
	case BucketGranularity6Hour:
		return "to_timestamp(floor(extract(epoch from sample_time) / 21600) * 21600) AT TIME ZONE 'UTC'"
	default:
		return "date_trunc('day', sample_time AT TIME ZONE 'UTC')"
	}
}

// TermWindow defines the time window and bucketing for a recommendation term.
type TermWindow struct {
	Name        string
	WindowHours int
	Bucket      BucketGranularity
	BucketKey   string // Go time format for the bucket key in plots_data
}

// defaultTermWindows are the hardcoded defaults when no org overrides exist.
var defaultTermWindows = map[string]TermWindow{
	"short_term": {
		Name:        "short_term",
		WindowHours: 24,
		Bucket:      BucketGranularity6Hour,
		BucketKey:   "2006-01-02T15:04:05.000Z",
	},
	"medium_term": {
		Name:        "medium_term",
		WindowHours: 7 * 24,
		Bucket:      BucketGranularityDaily,
		BucketKey:   "2006-01-02T15:04:05.000Z",
	},
	"long_term": {
		Name:        "long_term",
		WindowHours: 15 * 24,
		Bucket:      BucketGranularityDaily,
		BucketKey:   "2006-01-02T15:04:05.000Z",
	},
}

// termKeyNames maps term ordinals to API term key suffixes.
var termKeyNames = [3]string{"short", "medium", "long"}

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
		windowHours := c.WindowDays * 24
		bucket := BucketGranularityDaily
		if c.WindowDays <= 1 {
			bucket = BucketGranularity6Hour
		}
		result[name] = TermWindow{
			Name:        name,
			WindowHours: windowHours,
			Bucket:      bucket,
			BucketKey:   "2006-01-02T15:04:05.000Z",
		}
	}
	return result, nil
}

// AssembleBoxplots queries container_usage_samples and computes exact
// five-number summaries per time bucket using percentile_cont().
// orgID is used to load per-org term window overrides from the database.
func AssembleBoxplots(ctx context.Context, pool *pgxpool.Pool, key ContainerKey, termName string, orgID string) (*NativePlot, error) {
	windows, err := loadTermWindows(ctx, pool, orgID)
	if err != nil {
		return nil, fmt.Errorf("load term windows: %w", err)
	}
	tw, ok := windows[termName]
	if !ok {
		return nil, fmt.Errorf("unknown term: %s", termName)
	}

	end := time.Now().UTC()
	start := end.Add(-time.Duration(tw.WindowHours) * time.Hour)

	query := fmt.Sprintf(`
		SELECT
			%s AS bucket,
			MIN(cpu_usage_mc)::float8,
			percentile_cont(0.25) WITHIN GROUP (ORDER BY cpu_usage_mc)::float8,
			percentile_cont(0.5)  WITHIN GROUP (ORDER BY cpu_usage_mc)::float8,
			percentile_cont(0.75) WITHIN GROUP (ORDER BY cpu_usage_mc)::float8,
			MAX(cpu_usage_mc)::float8,
			MIN(mem_usage_kib)::float8,
			percentile_cont(0.25) WITHIN GROUP (ORDER BY mem_usage_kib)::float8,
			percentile_cont(0.5)  WITHIN GROUP (ORDER BY mem_usage_kib)::float8,
			percentile_cont(0.75) WITHIN GROUP (ORDER BY mem_usage_kib)::float8,
			MAX(mem_usage_kib)::float8
		FROM container_usage_samples
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3
		  AND workload = $4 AND container_name = $5
		  AND sample_time >= $6 AND sample_time < $7
		GROUP BY bucket
		ORDER BY bucket`,
		tw.Bucket.sql())

	rows, err := pool.Query(ctx, query,
		key.OrgID, key.ClusterUUID, key.Namespace, key.Workload, key.ContainerName,
		start, end)
	if err != nil {
		return nil, fmt.Errorf("AssembleBoxplots query: %w", err)
	}
	defer rows.Close()

	plotsData := map[string]NativePlotsData{}
	for rows.Next() {
		var bucket time.Time
		var cpuMin, cpuQ1, cpuMed, cpuQ3, cpuMax float64
		var memMin, memQ1, memMed, memQ3, memMax float64

		if err := rows.Scan(&bucket,
			&cpuMin, &cpuQ1, &cpuMed, &cpuQ3, &cpuMax,
			&memMin, &memQ1, &memMed, &memQ3, &memMax); err != nil {
			return nil, fmt.Errorf("AssembleBoxplots scan: %w", err)
		}

		bucketKey := bucket.UTC().Format(tw.BucketKey)
		plotsData[bucketKey] = NativePlotsData{
			CPUUsage: &BoxPlotDetails{
				Min: cpuMin / 1000.0, Q1: cpuQ1 / 1000.0, Median: cpuMed / 1000.0,
				Q3: cpuQ3 / 1000.0, Max: cpuMax / 1000.0, Format: "cores",
			},
			MemoryUsage: &BoxPlotDetails{
				Min: memMin / 1024.0, Q1: memQ1 / 1024.0, Median: memMed / 1024.0,
				Q3: memQ3 / 1024.0, Max: memMax / 1024.0, Format: "MiB",
			},
		}
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

// NamespaceKey identifies a unique namespace for boxplot queries.
type NamespaceKey struct {
	OrgID       string
	ClusterUUID string
	Namespace   string
}

// AssembleNamespaceBoxplots queries namespace_usage_samples and computes exact
// five-number summaries per time bucket using percentile_cont().
// Mirrors AssembleBoxplots but with namespace-level granularity (no workload/container).
// orgID is used to load per-org term window overrides from the database.
func AssembleNamespaceBoxplots(ctx context.Context, pool *pgxpool.Pool, key NamespaceKey, termName string, orgID string) (*NativePlot, error) {
	windows, err := loadTermWindows(ctx, pool, orgID)
	if err != nil {
		return nil, fmt.Errorf("load term windows: %w", err)
	}
	tw, ok := windows[termName]
	if !ok {
		return nil, fmt.Errorf("unknown term: %s", termName)
	}

	end := time.Now().UTC()
	start := end.Add(-time.Duration(tw.WindowHours) * time.Hour)

	query := fmt.Sprintf(`
		SELECT
			%s AS bucket,
			MIN(cpu_usage_mc)::float8,
			percentile_cont(0.25) WITHIN GROUP (ORDER BY cpu_usage_mc)::float8,
			percentile_cont(0.5)  WITHIN GROUP (ORDER BY cpu_usage_mc)::float8,
			percentile_cont(0.75) WITHIN GROUP (ORDER BY cpu_usage_mc)::float8,
			MAX(cpu_usage_mc)::float8,
			MIN(mem_usage_kib)::float8,
			percentile_cont(0.25) WITHIN GROUP (ORDER BY mem_usage_kib)::float8,
			percentile_cont(0.5)  WITHIN GROUP (ORDER BY mem_usage_kib)::float8,
			percentile_cont(0.75) WITHIN GROUP (ORDER BY mem_usage_kib)::float8,
			MAX(mem_usage_kib)::float8
		FROM namespace_usage_samples
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3
		  AND sample_time >= $4 AND sample_time < $5
		GROUP BY bucket
		ORDER BY bucket`,
		tw.Bucket.sql())

	rows, err := pool.Query(ctx, query,
		key.OrgID, key.ClusterUUID, key.Namespace,
		start, end)
	if err != nil {
		return nil, fmt.Errorf("AssembleNamespaceBoxplots query: %w", err)
	}
	defer rows.Close()

	plotsData := map[string]NativePlotsData{}
	for rows.Next() {
		var bucket time.Time
		var cpuMin, cpuQ1, cpuMed, cpuQ3, cpuMax float64
		var memMin, memQ1, memMed, memQ3, memMax float64

		if err := rows.Scan(&bucket,
			&cpuMin, &cpuQ1, &cpuMed, &cpuQ3, &cpuMax,
			&memMin, &memQ1, &memMed, &memQ3, &memMax); err != nil {
			return nil, fmt.Errorf("AssembleNamespaceBoxplots scan: %w", err)
		}

		bucketKey := bucket.UTC().Format(tw.BucketKey)
		plotsData[bucketKey] = NativePlotsData{
			CPUUsage: &BoxPlotDetails{
				Min: cpuMin / 1000.0, Q1: cpuQ1 / 1000.0, Median: cpuMed / 1000.0,
				Q3: cpuQ3 / 1000.0, Max: cpuMax / 1000.0, Format: "cores",
			},
			MemoryUsage: &BoxPlotDetails{
				Min: memMin / 1024.0, Q1: memQ1 / 1024.0, Median: memMed / 1024.0,
				Q3: memQ3 / 1024.0, Max: memMax / 1024.0, Format: "MiB",
			},
		}
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
