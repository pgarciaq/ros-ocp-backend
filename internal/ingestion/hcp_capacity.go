package ingestion

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	libcsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	hcpcapacity "github.com/redhatinsights/ros-ocp-backend/librobne/hcp"
)

// W4 fleet-capacity snapshot ingestion (#392, ADR-0334 amendment).
//
// The CSV parsing and validation live in librobne/csv (shared with the
// robne CLI, #700): the 25-column operator contract, strict row
// validation with skip reasons, and the future-skew rule
// (HCPCapacitySkewCheck, applied here with wall clock — the CLI applies
// it with its --now clock). Scenarios are computed at ingest via
// librobne/hcp and persisted; headroom and status are derived at serve
// time. The dispatch contract keeps everything non-fatal: the caller
// marks Done, so a poisoned capacity file cannot gate container
// recommendations via manifest completeness (SLO contract).

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
func upsertHCPCapacityRow(ctx context.Context, pool *pgxpool.Pool, r libcsv.HCPCapacityRow, orgID, clusterUUID string) error {
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
func nullableQPS(r libcsv.HCPCapacityRow) interface{} {
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
	parsed, parseSkips, extras, err := libcsv.ParseHCPCapacityRows(r)
	if err != nil {
		return err
	}
	if len(extras) > 0 {
		logging.GetLogger().WithField("cluster_uuid", clusterUUID).Warnf(
			"ProcessHCPCapacityCSV: tolerating unrecognized extra column(s): %s", strings.Join(extras, ", "))
	}
	rows, skewDrops := libcsv.HCPCapacitySkewCheck(parsed, time.Now().UTC())
	skipped := len(parseSkips) + len(skewDrops)
	if skipped > 0 {
		metrics.IncCSVRowsSkipped("hcp-capacity", skipped)
		logging.GetLogger().Warnf("ProcessHCPCapacityCSV: skipped %d malformed, invalid, or future-dated rows", skipped)
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
