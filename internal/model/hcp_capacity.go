package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// W4 fleet-capacity history queries (#392). pgx-based (not gorm) for
// direct testability over the test pool; the API handlers wire
// database.GetPool() plus cluster RBAC (filterClustersByRBAC over
// getClustersForOrg — the established pool-surface authorization, same
// permission semantics as rbac.AddRBACFilter's cluster grants; the gorm
// helper itself is table-qualified to recommendation_sets and would
// false-negative capacity-only clusters). org_id scoping here is the
// tenant boundary.

// HCPCapacitySnapshot is one stored history row. Nullable inputs scan as
// pointers: NULL is the absence signal (unknown QPS, absent MCE
// reference), never zero. Headroom and scenario status are derived at
// serve time from the stored maxima, current count, completeness flags,
// and availability basis — not persisted.
type HCPCapacitySnapshot struct {
	ClusterUUID                string
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
	SharedInfrastructure       bool
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
// in scanHCPCapacityInto.
const hcpCapacityColumnList = `cluster_uuid, observed_at, mce_sampled_at, operator_version, ocp_version, mce_version,
	native_model_version, collection_interval_minutes, upload_cycle_minutes,
	inventory_complete, current_hc_count, ready_hcp_count,
	average_ready_hcp_qps, ready_hcp_qps_known, hosted_availability_basis,
	eligible_worker_cpu_allocatable_millicores, eligible_worker_memory_allocatable_bytes,
	eligible_worker_max_pods, worker_placement_known, pressure_state, shared_infrastructure,
	mce_request_max_hcs, mce_low_qps_max_hcs, mce_average_qps_max_hcs,
	mce_medium_qps_max_hcs, mce_high_qps_max_hcs,
	request_max_hcs, request_limiting_resource,
	low_qps_max_hcs, low_qps_limiting_resource,
	average_qps_max_hcs, average_qps_limiting_resource,
	medium_qps_max_hcs, medium_qps_limiting_resource,
	high_qps_max_hcs, high_qps_limiting_resource`

// scanHCPCapacityInto fills one snapshot; pgx.Row and pgx.Rows both
// satisfy this Scan shape.
func scanHCPCapacityInto(row interface{ Scan(dest ...any) error }, s *HCPCapacitySnapshot) error {
	return row.Scan(
		&s.ClusterUUID, &s.ObservedAt, &s.MCESampledAt, &s.OperatorVersion, &s.OCPVersion, &s.MCEVersion,
		&s.NativeModelVersion, &s.CollectionIntervalMinutes, &s.UploadCycleMinutes,
		&s.InventoryComplete, &s.CurrentHCCount, &s.ReadyHCPCount,
		&s.AverageReadyHCPQPS, &s.ReadyHCPQPSKnown, &s.HostedAvailabilityBasis,
		&s.EligibleCPUMillicores, &s.EligibleMemoryBytes,
		&s.EligibleMaxPods, &s.WorkerPlacementKnown, &s.PressureState, &s.SharedInfrastructure,
		&s.MCERequestMaxHCs, &s.MCELowQPSMaxHCs, &s.MCEAverageQPSMaxHCs,
		&s.MCEMediumQPSMaxHCs, &s.MCEHighQPSMaxHCs,
		&s.RequestMaxHCs, &s.RequestLimitingResource,
		&s.LowQPSMaxHCs, &s.LowQPSLimitingResource,
		&s.AverageQPSMaxHCs, &s.AverageQPSLimitingResource,
		&s.MediumQPSMaxHCs, &s.MediumQPSLimitingResource,
		&s.HighQPSMaxHCs, &s.HighQPSLimitingResource,
	)
}

// GetLatestHCPCapacity returns the newest snapshot for one org+cluster, or
// nil when none exists (never another org's row — org_id is in the key).
func GetLatestHCPCapacity(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) (*HCPCapacitySnapshot, error) {
	s := &HCPCapacitySnapshot{}
	err := scanHCPCapacityInto(pool.QueryRow(ctx,
		`SELECT `+hcpCapacityColumnList+` FROM hcp_capacity_history
		WHERE org_id = $1 AND cluster_uuid = $2
		ORDER BY observed_at DESC LIMIT 1`, orgID, clusterUUID), s)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// ListLatestHCPCapacity returns the newest snapshot per cluster for the
// given clusters — exactly one row per cluster (DISTINCT ON with the
// observed_at DESC ordering; the (org_id, cluster_uuid, observed_at) key
// makes same-cluster ties impossible, so same-timestamp collisions across
// clusters cannot fan out or collapse). The bind is uuid[] (not a text
// cast) so the (org_id, cluster_uuid, observed_at DESC) index
// discriminates clusters.
func ListLatestHCPCapacity(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUIDs []string) ([]HCPCapacitySnapshot, error) {
	ids := make([]uuid.UUID, 0, len(clusterUUIDs))
	for _, s := range clusterUUIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("hcp-capacity: invalid cluster uuid %q: %w", s, err)
		}
		ids = append(ids, id)
	}
	rows, err := pool.Query(ctx,
		`SELECT DISTINCT ON (cluster_uuid) `+hcpCapacityColumnList+` FROM hcp_capacity_history
		WHERE org_id = $1 AND cluster_uuid = ANY($2)
		ORDER BY cluster_uuid, observed_at DESC`, orgID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HCPCapacitySnapshot{}
	for rows.Next() {
		s := &HCPCapacitySnapshot{}
		if err := scanHCPCapacityInto(rows, s); err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// GetHCPCapacitySince returns ascending snapshots at or after `since` for
// one org+cluster — the freshness-window read behind the MCE-may-be-stale
// caution.
func GetHCPCapacitySince(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, since time.Time) ([]HCPCapacitySnapshot, error) {
	rows, err := pool.Query(ctx,
		`SELECT `+hcpCapacityColumnList+` FROM hcp_capacity_history
		WHERE org_id = $1 AND cluster_uuid = $2 AND observed_at >= $3
		ORDER BY observed_at ASC`, orgID, clusterUUID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HCPCapacitySnapshot{}
	for rows.Next() {
		s := &HCPCapacitySnapshot{}
		if err := scanHCPCapacityInto(rows, s); err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
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
		if err := scanHCPCapacityInto(rows, s); err != nil {
			return nil, 0, err
		}
		out = append(out, *s)
	}
	return out, total, rows.Err()
}
