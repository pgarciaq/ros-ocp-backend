package model

import (
	"database/sql"
	"fmt"
)

// Positional quality scans (#523). Column order in each function must match
// the corresponding Select clause. This bypasses GORM's reflection-based
// Find, following native_pgx_scan.go.
//
// Nullability: adoption_detected is nullable with DEFAULT false, so it scans
// via sql.NullBool (GORM-zero semantics) rather than failing on NULL like a
// direct bool scan would. Pointer fields take NULL as nil natively.
// cluster_alias is NOT NULL with an inner join, so plain string is safe.

func scanQualityRows(sqlRows *sql.Rows, capacity int) ([]QualityRow, error) {
	rows := make([]QualityRow, 0, capacity)
	for sqlRows.Next() {
		var r QualityRow
		var adopted sql.NullBool
		err := sqlRows.Scan(
			&r.MeasuredAt, &r.ClusterUUID, &r.ClusterAlias,
			&r.Namespace, &r.Workload, &r.ContainerName, &r.Engine,
			&r.StabilityPct, &adopted,
			&r.OOMEventsAfterRec, &r.RecommendationAgeHrs,
		)
		if err != nil {
			return nil, fmt.Errorf("scanQualityRows: %w", err)
		}
		r.AdoptionDetected = adopted.Bool
		rows = append(rows, r)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("scanQualityRows iteration: %w", err)
	}
	return rows, nil
}

func scanPVCQualityRows(sqlRows *sql.Rows, capacity int) ([]PVCQualityRow, error) {
	rows := make([]PVCQualityRow, 0, capacity)
	for sqlRows.Next() {
		var r PVCQualityRow
		var adopted sql.NullBool
		err := sqlRows.Scan(
			&r.MeasuredAt, &r.ClusterUUID, &r.ClusterAlias,
			&r.Namespace, &r.PVCName, &r.Engine,
			&r.StabilityPct, &adopted,
			&r.DaysAboveThreshold, &r.RecommendationAgeHrs,
		)
		if err != nil {
			return nil, fmt.Errorf("scanPVCQualityRows: %w", err)
		}
		r.AdoptionDetected = adopted.Bool
		rows = append(rows, r)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("scanPVCQualityRows iteration: %w", err)
	}
	return rows, nil
}

func scanVMQualityRows(sqlRows *sql.Rows, capacity int) ([]VMQualityRow, error) {
	rows := make([]VMQualityRow, 0, capacity)
	for sqlRows.Next() {
		var r VMQualityRow
		var adopted sql.NullBool
		err := sqlRows.Scan(
			&r.MeasuredAt, &r.ClusterUUID, &r.ClusterAlias,
			&r.Namespace, &r.VMName, &r.Engine,
			&r.StabilityPct, &adopted,
			&r.SaturationDays, &r.RecommendationAgeHrs,
		)
		if err != nil {
			return nil, fmt.Errorf("scanVMQualityRows: %w", err)
		}
		r.AdoptionDetected = adopted.Bool
		rows = append(rows, r)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("scanVMQualityRows iteration: %w", err)
	}
	return rows, nil
}

func scanSnapshotQualityRows(sqlRows *sql.Rows, capacity int) ([]SnapshotQualityRow, error) {
	rows := make([]SnapshotQualityRow, 0, capacity)
	for sqlRows.Next() {
		var r SnapshotQualityRow
		var adopted sql.NullBool
		err := sqlRows.Scan(
			&r.MeasuredAt, &r.ClusterUUID, &r.ClusterAlias,
			&r.SnapshotName,
			&adopted, &r.RecommendationAgeHrs,
		)
		if err != nil {
			return nil, fmt.Errorf("scanSnapshotQualityRows: %w", err)
		}
		r.AdoptionDetected = adopted.Bool
		rows = append(rows, r)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("scanSnapshotQualityRows iteration: %w", err)
	}
	return rows, nil
}

func scanGPUMIGQualityRows(sqlRows *sql.Rows, capacity int) ([]GPUMIGQualityRow, error) {
	rows := make([]GPUMIGQualityRow, 0, capacity)
	for sqlRows.Next() {
		var r GPUMIGQualityRow
		var adopted sql.NullBool
		err := sqlRows.Scan(
			&r.MeasuredAt, &r.ClusterUUID, &r.ClusterAlias,
			&r.Namespace, &r.Workload, &r.ContainerName, &r.Engine,
			&r.StabilityPct, &adopted,
			&r.ContentionDays, &r.RecommendationAgeHrs,
		)
		if err != nil {
			return nil, fmt.Errorf("scanGPUMIGQualityRows: %w", err)
		}
		r.AdoptionDetected = adopted.Bool
		rows = append(rows, r)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("scanGPUMIGQualityRows iteration: %w", err)
	}
	return rows, nil
}
