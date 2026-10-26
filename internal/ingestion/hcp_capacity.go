package ingestion

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	hcpcapacity "github.com/redhatinsights/ros-ocp-backend/librobne/hcp"
)

// W4 fleet-capacity snapshot ingestion (#392, ADR-0334 amendment).
//
// The 25-column header is the operator-side contract (KMO
// TestHCPCapacityRow_Contract pins the same list); the model version is
// validated against librobne/hcp.ModelVersion at parse so the operator
// and the math cannot silently drift. Row-level problems (bad values,
// future skew, cadence out of bounds) skip with counters — never
// zero-filled. A header mismatch or a CSV syntax error aborts the parse
// and returns an error, but the dispatch contract keeps that non-fatal:
// the caller marks Done (not Failed), so a poisoned capacity file cannot
// gate container recommendations via manifest completeness (SLO
// contract). Scenarios are computed at ingest via librobne/hcp and
// persisted; headroom and status are derived at serve time from the
// stored maxima, current count, and completeness flags.

// hcpCapacitySkewTolerance bounds the operator-supplied observed_at: a
// future-dated row cannot keep `latest` permanently fresh.
const hcpCapacitySkewTolerance = 5 * time.Minute

// hcpCapacityMaxCadenceMinutes bounds the cadence fields the serve-side
// staleness clock (2× max cadence) depends on: a zero or absurd value
// would make the window degenerate and mark every row permanently stale.
const hcpCapacityMaxCadenceMinutes = 60 * 24 * 60 // 60 days

var hcpCapacityHeader = []string{
	"observed_at", "mce_sampled_at", "operator_version", "ocp_version", "mce_version",
	"native_model_version", "collection_interval_minutes", "upload_cycle_minutes",
	"inventory_complete", "current_hc_count", "ready_hcp_count",
	"average_ready_hcp_qps", "ready_hcp_qps_known", "hosted_availability_basis",
	"eligible_worker_cpu_allocatable_millicores", "eligible_worker_memory_allocatable_bytes",
	"eligible_worker_max_pods", "worker_placement_known", "pressure_state",
	"mce_request_max_hcs", "mce_low_qps_max_hcs", "mce_average_qps_max_hcs",
	"mce_medium_qps_max_hcs", "mce_high_qps_max_hcs",
	// Appended (contract position 25): compact/shared-infrastructure
	// topology — management control-plane nodes doubling as workers
	// (ADR-0334 §3.2.5 caveat).
	"shared_infrastructure",
}

// HCPCapacityRow is one parsed snapshot row. cluster_uuid is deliberately
// absent: it arrives as trusted Kafka/report metadata, never CSV-supplied.
type HCPCapacityRow struct {
	ObservedAt                time.Time
	MCESampledAt              *time.Time
	OperatorVersion           string
	OCPVersion                string
	MCEVersion                string
	NativeModelVersion        string
	CollectionIntervalMinutes int64
	UploadCycleMinutes        int64
	InventoryComplete         bool
	CurrentHCCount            int64
	ReadyHCPCount             int64
	AverageReadyHCPQPS        float64
	ReadyHCPQPSKnown          bool
	HostedAvailabilityBasis   string
	EligibleCPUMillicores     int64
	EligibleMemoryBytes       int64
	EligibleMaxPods           int64
	WorkerPlacementKnown      bool
	PressureState             string
	SharedInfrastructure      bool
	MCERequestMaxHCs          *int64
	MCELowQPSMaxHCs           *int64
	MCEAverageQPSMaxHCs       *int64
	MCEMediumQPSMaxHCs        *int64
	MCEHighQPSMaxHCs          *int64
}

func parseStrictBool(s string) (bool, error) {
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("not a strict boolean: %q", s)
}

func parseOptionalInt64(s string) (*int64, error) {
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// parseHCPCapacityRecord validates one 24-field record; ok=false means
// skip-with-counter.
func parseHCPCapacityRecord(rec []string, now time.Time) (HCPCapacityRow, bool) {
	var r HCPCapacityRow
	if len(rec) != len(hcpCapacityHeader) {
		return r, false
	}
	var err error
	if r.ObservedAt, err = time.Parse(time.RFC3339, rec[0]); err != nil {
		return r, false
	}
	if r.ObservedAt.After(now.Add(hcpCapacitySkewTolerance)) {
		return r, false
	}
	if rec[1] != "" {
		t, err := time.Parse(time.RFC3339, rec[1])
		if err != nil {
			return r, false
		}
		r.MCESampledAt = &t
	}
	r.OperatorVersion = rec[2]
	r.OCPVersion = rec[3]
	r.MCEVersion = rec[4]
	if rec[5] != hcpcapacity.ModelVersion {
		return r, false
	}
	r.NativeModelVersion = rec[5]
	if r.CollectionIntervalMinutes, err = strconv.ParseInt(rec[6], 10, 64); err != nil {
		return r, false
	}
	if r.UploadCycleMinutes, err = strconv.ParseInt(rec[7], 10, 64); err != nil {
		return r, false
	}
	// Cadence bounds: the serve-side staleness window is 2× max(cadence);
	// zero or absurd values would degenerate it (every row permanently
	// stale, or a negative window after Duration overflow).
	if r.CollectionIntervalMinutes <= 0 || r.CollectionIntervalMinutes > hcpCapacityMaxCadenceMinutes ||
		r.UploadCycleMinutes <= 0 || r.UploadCycleMinutes > hcpCapacityMaxCadenceMinutes {
		return r, false
	}
	if r.InventoryComplete, err = parseStrictBool(rec[8]); err != nil {
		return r, false
	}
	if r.CurrentHCCount, err = strconv.ParseInt(rec[9], 10, 64); err != nil {
		return r, false
	}
	if r.ReadyHCPCount, err = strconv.ParseInt(rec[10], 10, 64); err != nil {
		return r, false
	}
	if r.ReadyHCPQPSKnown, err = parseStrictBool(rec[12]); err != nil {
		return r, false
	}
	if r.ReadyHCPQPSKnown {
		q, err := strconv.ParseFloat(rec[11], 64)
		if err != nil || math.IsNaN(q) || math.IsInf(q, 0) || q < 0 {
			return r, false
		}
		r.AverageReadyHCPQPS = q
	} else if rec[11] != "" {
		return r, false
	}
	r.HostedAvailabilityBasis = rec[13]
	switch r.HostedAvailabilityBasis {
	case "", hcpcapacity.BasisHighlyAvailable, hcpcapacity.BasisMixed, hcpcapacity.BasisSingleReplica, hcpcapacity.BasisUnknown:
	default:
		return r, false
	}
	if r.EligibleCPUMillicores, err = strconv.ParseInt(rec[14], 10, 64); err != nil {
		return r, false
	}
	if r.EligibleMemoryBytes, err = strconv.ParseInt(rec[15], 10, 64); err != nil {
		return r, false
	}
	if r.EligibleMaxPods, err = strconv.ParseInt(rec[16], 10, 64); err != nil {
		return r, false
	}
	if r.WorkerPlacementKnown, err = parseStrictBool(rec[17]); err != nil {
		return r, false
	}
	switch rec[18] {
	case "pressured", "unknown":
		r.PressureState = rec[18]
	default:
		return r, false
	}
	if r.SharedInfrastructure, err = parseStrictBool(rec[24]); err != nil {
		return r, false
	}
	if r.MCERequestMaxHCs, err = parseOptionalInt64(rec[19]); err != nil {
		return r, false
	}
	if r.MCELowQPSMaxHCs, err = parseOptionalInt64(rec[20]); err != nil {
		return r, false
	}
	if r.MCEAverageQPSMaxHCs, err = parseOptionalInt64(rec[21]); err != nil {
		return r, false
	}
	if r.MCEMediumQPSMaxHCs, err = parseOptionalInt64(rec[22]); err != nil {
		return r, false
	}
	if r.MCEHighQPSMaxHCs, err = parseOptionalInt64(rec[23]); err != nil {
		return r, false
	}
	return r, true
}

// parseHCPCapacityRows parses the snapshot CSV. Header mismatch aborts the
// file (the caller still marks Done); row-level problems skip with
// counters.
func parseHCPCapacityRows(r io.Reader, now time.Time) ([]HCPCapacityRow, int, error) {
	reader := csv.NewReader(r)
	// Variable field count: a short or long row is a row-level problem
	// (skip with counter), not a file abort — the length check in
	// parseHCPCapacityRecord governs.
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, 0, fmt.Errorf("hcp-capacity: reading header: %w", err)
	}
	if !reflect.DeepEqual(header, hcpCapacityHeader) {
		return nil, 0, fmt.Errorf("hcp-capacity: missing or unexpected columns (header must match the 24-column operator contract)")
	}
	var rows []HCPCapacityRow
	skipped := 0
	for {
		rec, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("hcp-capacity: reading row: %w", err)
		}
		row, ok := parseHCPCapacityRecord(rec, now)
		if !ok {
			skipped++
			continue
		}
		rows = append(rows, row)
	}
	return rows, skipped, nil
}

// scenarioCols is the (max, limiting) pair stored per scenario.
type scenarioCols struct {
	maxHCs int64
	limit  string
}

func scenarioColumns(res hcpcapacity.CapacityResult) map[string]scenarioCols {
	out := make(map[string]scenarioCols, len(res.Scenarios))
	for _, s := range res.Scenarios {
		out[s.Name] = scenarioCols{maxHCs: s.MaxHCs, limit: s.LimitingResource}
	}
	return out
}

// upsertHCPCapacityRow computes the five native scenarios via librobne/hcp
// and persists inputs + scenarios. A CalculateCapacity validation error
// (internally inconsistent row) is returned for the caller to skip with a
// counter — never a file abort.
func upsertHCPCapacityRow(ctx context.Context, pool *pgxpool.Pool, r HCPCapacityRow, orgID, clusterUUID string) error {
	res, err := hcpcapacity.CalculateCapacity(hcpcapacity.CapacityInputs{
		Workers: hcpcapacity.WorkerCapacity{
			CPUAllocatableMillicores: r.EligibleCPUMillicores,
			MemoryAllocatableBytes:   r.EligibleMemoryBytes,
			MaxPods:                  r.EligibleMaxPods,
			PlacementKnown:           r.WorkerPlacementKnown,
		},
		CurrentHCCount:          r.CurrentHCCount,
		InventoryComplete:       r.InventoryComplete,
		ReadyHCPCount:           r.ReadyHCPCount,
		AverageReadyHCPQPS:      r.AverageReadyHCPQPS,
		ReadyHCPQPSKnown:        r.ReadyHCPQPSKnown,
		HostedAvailabilityBasis: r.HostedAvailabilityBasis,
	})
	if err != nil {
		return fmt.Errorf("hcp-capacity: scenario calculation: %w", err)
	}
	sc := scenarioColumns(res)

	const upsertSQL = `
		INSERT INTO hcp_capacity_history (
			observed_at, org_id, cluster_uuid, mce_sampled_at,
			operator_version, ocp_version, mce_version, native_model_version,
			collection_interval_minutes, upload_cycle_minutes,
			inventory_complete, current_hc_count, ready_hcp_count,
			average_ready_hcp_qps, ready_hcp_qps_known, hosted_availability_basis,
			eligible_worker_cpu_allocatable_millicores, eligible_worker_memory_allocatable_bytes,
			eligible_worker_max_pods, worker_placement_known, pressure_state,
			mce_request_max_hcs, mce_low_qps_max_hcs, mce_average_qps_max_hcs,
			mce_medium_qps_max_hcs, mce_high_qps_max_hcs,
			request_max_hcs, request_limiting_resource,
			low_qps_max_hcs, low_qps_limiting_resource,
			average_qps_max_hcs, average_qps_limiting_resource,
			medium_qps_max_hcs, medium_qps_limiting_resource,
			high_qps_max_hcs, high_qps_limiting_resource,
			shared_infrastructure
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37)
		ON CONFLICT (org_id, cluster_uuid, observed_at) DO UPDATE SET
			mce_sampled_at = EXCLUDED.mce_sampled_at,
			operator_version = EXCLUDED.operator_version,
			ocp_version = EXCLUDED.ocp_version,
			mce_version = EXCLUDED.mce_version,
			native_model_version = EXCLUDED.native_model_version,
			collection_interval_minutes = EXCLUDED.collection_interval_minutes,
			upload_cycle_minutes = EXCLUDED.upload_cycle_minutes,
			inventory_complete = EXCLUDED.inventory_complete,
			current_hc_count = EXCLUDED.current_hc_count,
			ready_hcp_count = EXCLUDED.ready_hcp_count,
			average_ready_hcp_qps = EXCLUDED.average_ready_hcp_qps,
			ready_hcp_qps_known = EXCLUDED.ready_hcp_qps_known,
			hosted_availability_basis = EXCLUDED.hosted_availability_basis,
			eligible_worker_cpu_allocatable_millicores = EXCLUDED.eligible_worker_cpu_allocatable_millicores,
			eligible_worker_memory_allocatable_bytes = EXCLUDED.eligible_worker_memory_allocatable_bytes,
			eligible_worker_max_pods = EXCLUDED.eligible_worker_max_pods,
			worker_placement_known = EXCLUDED.worker_placement_known,
			pressure_state = EXCLUDED.pressure_state,
			mce_request_max_hcs = EXCLUDED.mce_request_max_hcs,
			mce_low_qps_max_hcs = EXCLUDED.mce_low_qps_max_hcs,
			mce_average_qps_max_hcs = EXCLUDED.mce_average_qps_max_hcs,
			mce_medium_qps_max_hcs = EXCLUDED.mce_medium_qps_max_hcs,
			mce_high_qps_max_hcs = EXCLUDED.mce_high_qps_max_hcs,
			request_max_hcs = EXCLUDED.request_max_hcs,
			request_limiting_resource = EXCLUDED.request_limiting_resource,
			low_qps_max_hcs = EXCLUDED.low_qps_max_hcs,
			low_qps_limiting_resource = EXCLUDED.low_qps_limiting_resource,
			average_qps_max_hcs = EXCLUDED.average_qps_max_hcs,
			average_qps_limiting_resource = EXCLUDED.average_qps_limiting_resource,
			medium_qps_max_hcs = EXCLUDED.medium_qps_max_hcs,
			medium_qps_limiting_resource = EXCLUDED.medium_qps_limiting_resource,
			high_qps_max_hcs = EXCLUDED.high_qps_max_hcs,
			high_qps_limiting_resource = EXCLUDED.high_qps_limiting_resource,
			shared_infrastructure = EXCLUDED.shared_infrastructure`

	// Scenario order matches the stable librobne contract; lookup is by
	// name so a reordered result cannot scramble columns silently.
	req := sc[hcpcapacity.ScenarioRequest]
	low := sc[hcpcapacity.ScenarioLowQPS]
	avg := sc[hcpcapacity.ScenarioAverageQPS]
	med := sc[hcpcapacity.ScenarioMediumQPS]
	high := sc[hcpcapacity.ScenarioHighQPS]

	_, err = pool.Exec(ctx, upsertSQL,
		r.ObservedAt.UTC(), orgID, clusterUUID, r.MCESampledAt,
		r.OperatorVersion, r.OCPVersion, r.MCEVersion, r.NativeModelVersion,
		r.CollectionIntervalMinutes, r.UploadCycleMinutes,
		r.InventoryComplete, r.CurrentHCCount, r.ReadyHCPCount,
		nullableQPS(r), r.ReadyHCPQPSKnown, r.HostedAvailabilityBasis,
		r.EligibleCPUMillicores, r.EligibleMemoryBytes,
		r.EligibleMaxPods, r.WorkerPlacementKnown, r.PressureState,
		r.MCERequestMaxHCs, r.MCELowQPSMaxHCs, r.MCEAverageQPSMaxHCs,
		r.MCEMediumQPSMaxHCs, r.MCEHighQPSMaxHCs,
		req.maxHCs, req.limit, low.maxHCs, low.limit,
		avg.maxHCs, avg.limit, med.maxHCs, med.limit,
		high.maxHCs, high.limit,
		r.SharedInfrastructure,
	)
	if err != nil {
		return fmt.Errorf("hcp-capacity: upserting snapshot %s/%s/%s: %w", orgID, clusterUUID, r.ObservedAt.Format(time.RFC3339), err)
	}
	return nil
}

// nullableQPS renders the average as NULL when unknown — absence is the
// unavailability signal, never zero.
func nullableQPS(r HCPCapacityRow) interface{} {
	if !r.ReadyHCPQPSKnown {
		return nil
	}
	return r.AverageReadyHCPQPS
}

// EnsureHCPCapacityPartitions pre-creates monthly partitions covering the
// previous, current, and next two months (a window ending in the prior
// month can be emitted just after midnight on the 1st). Idempotent via
// catalog check.
func EnsureHCPCapacityPartitions(ctx context.Context, pool *pgxpool.Pool, monthStart time.Time) error {
	ms := time.Date(monthStart.Year(), monthStart.Month(), 1, 0, 0, 0, 0, time.UTC)
	for i := -1; i <= 2; i++ {
		start := ms.AddDate(0, i, 0)
		end := start.AddDate(0, 1, 0)
		part := "hcp_capacity_history_" + start.Format("200601")
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_class WHERE relname = $1)`, part).Scan(&exists); err != nil {
			return fmt.Errorf("hcp-capacity: checking partition %s: %w", part, err)
		}
		if exists {
			continue
		}
		q := `CREATE TABLE IF NOT EXISTS "` + part + `" PARTITION OF hcp_capacity_history` +
			` FOR VALUES FROM ('` + start.Format("2006-01-02") + `') TO ('` + end.Format("2006-01-02") + `')`
		if _, err := pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("hcp-capacity: creating partition %s: %w", part, err)
		}
	}
	return nil
}

// ProcessHCPCapacityCSV is the top-level W4 snapshot ingestion entry point
// (#392). Never permanently fails the manifest on bad data: malformed rows
// are skipped with counters; a CalculateCapacity validation failure on an
// internally inconsistent row skips that row, not the file.
func ProcessHCPCapacityCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, skipped, err := parseHCPCapacityRows(r, time.Now().UTC())
	if err != nil {
		return err
	}
	if skipped > 0 {
		metrics.IncCSVRowsSkipped("hcp-capacity", skipped)
		logging.GetLogger().Warnf("ProcessHCPCapacityCSV: skipped %d malformed or invalid rows", skipped)
	}
	inserted := 0
	for _, row := range rows {
		if err := EnsureHCPCapacityPartitions(ctx, pool, row.ObservedAt); err != nil {
			return err
		}
		if err := upsertHCPCapacityRow(ctx, pool, row, orgID, clusterUUID); err != nil {
			metrics.IncCSVRowsSkipped("hcp-capacity", 1)
			logging.GetLogger().WithField("cluster_uuid", clusterUUID).Warnf("ProcessHCPCapacityCSV: skipping inconsistent row: %v", err)
			continue
		}
		inserted++
	}
	if inserted == 0 {
		logging.GetLogger().WithField("cluster_uuid", clusterUUID).Info("ProcessHCPCapacityCSV: no capacity rows ingested")
		return nil
	}
	logging.GetLogger().WithField("cluster_uuid", clusterUUID).Infof("ProcessHCPCapacityCSV: upserted %d capacity snapshots", inserted)
	return nil
}
