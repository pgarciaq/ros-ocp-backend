package model

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// W4 fleet-capacity history queries (#392). pgx-based (not gorm) for
// direct testability over the test pool; the Task 5 handlers wire
// database.GetPool() plus cluster RBAC. org_id scoping here is the tenant
// boundary; user-permission RBAC lands with the API surface.

// HCPCapacitySnapshot is one stored history row. Nullable inputs scan as
// pointers: NULL is the absence signal (unknown QPS, absent MCE
// reference), never zero. Headroom and scenario status are derived at
// serve time from the stored maxima, current count, completeness flags,
// and availability basis — not persisted.
type HCPCapacitySnapshot struct {
	ObservedAt                 time.Time
	MCESampledAt               *time.Time
	OperatorVersion            string
	OCPVersion                 string
	MCEVersion                 string
	NativeModelVersion         string
	CollectionIntervalMinutes  int64
	UploadCycleMinutes         int64
	InventoryComplete          bool
	CurrentHCCount             int64
	ReadyHCPCount              int64
	AverageReadyHCPQPS         *float64
	ReadyHCPQPSKnown           bool
	HostedAvailabilityBasis    string
	EligibleCPUMillicores      int64
	EligibleMemoryBytes        int64
	EligibleMaxPods            int64
	WorkerPlacementKnown       bool
	PressureState              string
	MCERequestMaxHCs           *int64
	MCELowQPSMaxHCs            *int64
	MCEAverageQPSMaxHCs        *int64
	MCEMediumQPSMaxHCs         *int64
	MCEHighQPSMaxHCs           *int64
	RequestMaxHCs              int64
	RequestLimitingResource    string
	LowQPSMaxHCs               int64
	LowQPSLimitingResource     string
	AverageQPSMaxHCs           int64
	AverageQPSLimitingResource string
	MediumQPSMaxHCs            int64
	MediumQPSLimitingResource  string
	HighQPSMaxHCs              int64
	HighQPSLimitingResource    string
}

// hcpCapacityColumnList is the SELECT list — the order is the Scan order
// in scanHCPCapacity.
const hcpCapacityColumnList = `observed_at, mce_sampled_at, operator_version, ocp_version, mce_version,
	native_model_version, collection_interval_minutes, upload_cycle_minutes,
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
	high_qps_max_hcs, high_qps_limiting_resource`

func scanHCPCapacity(row pgx.Row) (*HCPCapacitySnapshot, error) {
	s := &HCPCapacitySnapshot{}
	err := row.Scan(
		&s.ObservedAt, &s.MCESampledAt, &s.OperatorVersion, &s.OCPVersion, &s.MCEVersion,
		&s.NativeModelVersion, &s.CollectionIntervalMinutes, &s.UploadCycleMinutes,
		&s.InventoryComplete, &s.CurrentHCCount, &s.ReadyHCPCount,
		&s.AverageReadyHCPQPS, &s.ReadyHCPQPSKnown, &s.HostedAvailabilityBasis,
		&s.EligibleCPUMillicores, &s.EligibleMemoryBytes,
		&s.EligibleMaxPods, &s.WorkerPlacementKnown, &s.PressureState,
		&s.MCERequestMaxHCs, &s.MCELowQPSMaxHCs, &s.MCEAverageQPSMaxHCs,
		&s.MCEMediumQPSMaxHCs, &s.MCEHighQPSMaxHCs,
		&s.RequestMaxHCs, &s.RequestLimitingResource,
		&s.LowQPSMaxHCs, &s.LowQPSLimitingResource,
		&s.AverageQPSMaxHCs, &s.AverageQPSLimitingResource,
		&s.MediumQPSMaxHCs, &s.MediumQPSLimitingResource,
		&s.HighQPSMaxHCs, &s.HighQPSLimitingResource,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// GetLatestHCPCapacity returns the newest snapshot for one org+cluster, or
// nil when none exists (never another org's row — org_id is in the key).
func GetLatestHCPCapacity(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) (*HCPCapacitySnapshot, error) {
	return scanHCPCapacity(pool.QueryRow(ctx,
		`SELECT `+hcpCapacityColumnList+` FROM hcp_capacity_history
		WHERE org_id = $1 AND cluster_uuid = $2
		ORDER BY observed_at DESC LIMIT 1`, orgID, clusterUUID))
}

// GetHCPCapacityHistory returns newest-first history rows for one
// org+cluster plus the total count for pagination.
func GetHCPCapacityHistory(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, limit, offset int) ([]HCPCapacitySnapshot, int64, error) {
	var total int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM hcp_capacity_history WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := pool.Query(ctx,
		`SELECT `+hcpCapacityColumnList+` FROM hcp_capacity_history
		WHERE org_id = $1 AND cluster_uuid = $2
		ORDER BY observed_at DESC LIMIT $3 OFFSET $4`, orgID, clusterUUID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []HCPCapacitySnapshot{}
	for rows.Next() {
		s := &HCPCapacitySnapshot{}
		if err := rows.Scan(
			&s.ObservedAt, &s.MCESampledAt, &s.OperatorVersion, &s.OCPVersion, &s.MCEVersion,
			&s.NativeModelVersion, &s.CollectionIntervalMinutes, &s.UploadCycleMinutes,
			&s.InventoryComplete, &s.CurrentHCCount, &s.ReadyHCPCount,
			&s.AverageReadyHCPQPS, &s.ReadyHCPQPSKnown, &s.HostedAvailabilityBasis,
			&s.EligibleCPUMillicores, &s.EligibleMemoryBytes,
			&s.EligibleMaxPods, &s.WorkerPlacementKnown, &s.PressureState,
			&s.MCERequestMaxHCs, &s.MCELowQPSMaxHCs, &s.MCEAverageQPSMaxHCs,
			&s.MCEMediumQPSMaxHCs, &s.MCEHighQPSMaxHCs,
			&s.RequestMaxHCs, &s.RequestLimitingResource,
			&s.LowQPSMaxHCs, &s.LowQPSLimitingResource,
			&s.AverageQPSMaxHCs, &s.AverageQPSLimitingResource,
			&s.MediumQPSMaxHCs, &s.MediumQPSLimitingResource,
			&s.HighQPSMaxHCs, &s.HighQPSLimitingResource,
		); err != nil {
			return nil, 0, err
		}
		out = append(out, *s)
	}
	return out, total, rows.Err()
}
