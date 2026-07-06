package model

import (
	"database/sql"
	"fmt"
)

// scanNativeContainerRows scans all rows from a *sql.Rows into NativeRecommendationRow
// structs using positional scanning. Column order must match nativeDetailSelect + page sort.
// This bypasses GORM's reflection-based scanIntoStruct, eliminating ~40MB of per-request
// allocations from reflect.New and field-name lookups (PROF-2).
func scanNativeContainerRows(sqlRows *sql.Rows, capacity int) ([]NativeRecommendationRow, error) {
	rows := make([]NativeRecommendationRow, 0, capacity)
	for sqlRows.Next() {
		var r NativeRecommendationRow
		err := sqlRows.Scan(
			&r.OrgID, &r.ClusterUUID, &r.Namespace, &r.Workload, &r.WorkloadType,
			&r.ContainerName, &r.Term, &r.Engine,
			&r.RecCPURequestMC, &r.RecCPULimitMC,
			&r.RecMemRequestKiB, &r.RecMemLimitKiB,
			&r.CurrentCPURequestMC, &r.CurrentCPULimitMC,
			&r.CurrentMemRequestKiB, &r.CurrentMemLimitKiB,
			&r.VariationCPURequestPct, &r.VariationCPULimitPct,
			&r.VariationMemRequestPct, &r.VariationMemLimitPct,
			&r.NotificationCodes, &r.ConfidenceLevel, &r.Stale,
			&r.PodCountMin, &r.PodCountMax, &r.PodCountAvg,
			&r.DesiredReplicas, &r.AvailableReplicas,
			&r.RecommendedReplicas, &r.ReplicaConfidence, &r.ReplicaExplanation,
			&r.EstimatedSavingsCents,
			&r.EstimatedCPUSavingsCents, &r.EstimatedMemSavingsCents,
			&r.IdleState, &r.IdleSince, &r.IdleDurationDays,
			&r.PeakCPUMillicores, &r.PeakMemoryBytes, &r.EstimatedWasteCents,
			&r.MonitoringEndTime,
			&r.ExplDataDays, &r.ExplDecayHalfLifeHours,
			&r.ExplCPUCostPctMC, &r.ExplCPUPerfPctMC,
			&r.ExplCPUUsageP95MC, &r.ExplCPUUsageP50MC, &r.ExplCPUUsageMeanMC,
			&r.ExplCPUAdaptiveMarginBP, &r.ExplCPUTrendSlope,
			&r.ExplMemCostPctKiB, &r.ExplMemPerfPctKiB,
			&r.ExplMemUsageP95KiB, &r.ExplMemUsageP50KiB, &r.ExplMemUsageMeanKiB,
			&r.ExplMemAdaptiveMarginBP, &r.ExplMemTrendSlope,
			&r.ExplOOMCountSum, &r.ExplOOMBumpApplied, &r.ExplCPUFloorApplied, &r.ExplMemFloorApplied, &r.ExplIsIdle,
			&r.ExplGPUSMActiveAvgBP, &r.ExplGPUTensorActiveAvgBP, &r.ExplGPUDRAMActiveAvgBP,
			&r.ExplGPUFBUsageMaxMiB, &r.ExplGPUFBP98MiB,
			&r.ExplGPURecommendedProfile, &r.ExplGPUCurrentProfile,
			&r.ExplGPUHasProfilingData, &r.ExplGPUMemoryBound,
			&r.UpdatedAt,
			&r.SourceID, &r.ClusterAlias, &r.LastReported,
			&r.AnalyticsIncomplete, &r.AnalyticsIncompleteAt,
			&r.IngestHooksFailed, &r.IngestHooksFailedAt,
			&r.PageSortText,
		)
		if err != nil {
			return nil, fmt.Errorf("scanNativeContainerRows: %w", err)
		}
		rows = append(rows, r)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("scanNativeContainerRows iteration: %w", err)
	}
	return rows, nil
}

// scanNativeContainerRowsNoSort scans rows matching nativeDetailSelect WITHOUT
// the trailing page.ros_container_page_sort column. Used for detail (single-record) queries.
func scanNativeContainerRowsNoSort(sqlRows *sql.Rows, capacity int) ([]NativeRecommendationRow, error) {
	rows := make([]NativeRecommendationRow, 0, capacity)
	for sqlRows.Next() {
		var r NativeRecommendationRow
		err := sqlRows.Scan(
			&r.OrgID, &r.ClusterUUID, &r.Namespace, &r.Workload, &r.WorkloadType,
			&r.ContainerName, &r.Term, &r.Engine,
			&r.RecCPURequestMC, &r.RecCPULimitMC,
			&r.RecMemRequestKiB, &r.RecMemLimitKiB,
			&r.CurrentCPURequestMC, &r.CurrentCPULimitMC,
			&r.CurrentMemRequestKiB, &r.CurrentMemLimitKiB,
			&r.VariationCPURequestPct, &r.VariationCPULimitPct,
			&r.VariationMemRequestPct, &r.VariationMemLimitPct,
			&r.NotificationCodes, &r.ConfidenceLevel, &r.Stale,
			&r.PodCountMin, &r.PodCountMax, &r.PodCountAvg,
			&r.DesiredReplicas, &r.AvailableReplicas,
			&r.RecommendedReplicas, &r.ReplicaConfidence, &r.ReplicaExplanation,
			&r.EstimatedSavingsCents,
			&r.EstimatedCPUSavingsCents, &r.EstimatedMemSavingsCents,
			&r.IdleState, &r.IdleSince, &r.IdleDurationDays,
			&r.PeakCPUMillicores, &r.PeakMemoryBytes, &r.EstimatedWasteCents,
			&r.MonitoringEndTime,
			&r.ExplDataDays, &r.ExplDecayHalfLifeHours,
			&r.ExplCPUCostPctMC, &r.ExplCPUPerfPctMC,
			&r.ExplCPUUsageP95MC, &r.ExplCPUUsageP50MC, &r.ExplCPUUsageMeanMC,
			&r.ExplCPUAdaptiveMarginBP, &r.ExplCPUTrendSlope,
			&r.ExplMemCostPctKiB, &r.ExplMemPerfPctKiB,
			&r.ExplMemUsageP95KiB, &r.ExplMemUsageP50KiB, &r.ExplMemUsageMeanKiB,
			&r.ExplMemAdaptiveMarginBP, &r.ExplMemTrendSlope,
			&r.ExplOOMCountSum, &r.ExplOOMBumpApplied, &r.ExplCPUFloorApplied, &r.ExplMemFloorApplied, &r.ExplIsIdle,
			&r.ExplGPUSMActiveAvgBP, &r.ExplGPUTensorActiveAvgBP, &r.ExplGPUDRAMActiveAvgBP,
			&r.ExplGPUFBUsageMaxMiB, &r.ExplGPUFBP98MiB,
			&r.ExplGPURecommendedProfile, &r.ExplGPUCurrentProfile,
			&r.ExplGPUHasProfilingData, &r.ExplGPUMemoryBound,
			&r.UpdatedAt,
			&r.SourceID, &r.ClusterAlias, &r.LastReported,
			&r.AnalyticsIncomplete, &r.AnalyticsIncompleteAt,
			&r.IngestHooksFailed, &r.IngestHooksFailedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanNativeContainerRowsNoSort: %w", err)
		}
		rows = append(rows, r)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("scanNativeContainerRowsNoSort iteration: %w", err)
	}
	return rows, nil
}

// scanNativeNamespaceRows scans rows from nativeNSSelect + page sort column.
func scanNativeNamespaceRows(sqlRows *sql.Rows, capacity int) ([]NativeNamespaceRow, error) {
	rows := make([]NativeNamespaceRow, 0, capacity)
	for sqlRows.Next() {
		var r NativeNamespaceRow
		err := sqlRows.Scan(
			&r.OrgID, &r.ClusterUUID, &r.NamespaceName, &r.Term, &r.Engine,
			&r.RecCPURequestMC, &r.RecCPULimitMC,
			&r.RecMemRequestKiB, &r.RecMemLimitKiB,
			&r.CurrentCPURequestMC, &r.CurrentCPULimitMC,
			&r.CurrentMemRequestKiB, &r.CurrentMemLimitKiB,
			&r.VariationCPURequestPct, &r.VariationCPULimitPct,
			&r.VariationMemRequestPct, &r.VariationMemLimitPct,
			&r.NotificationCodes, &r.ConfidenceLevel, &r.Stale, &r.IdleState,
			&r.IdleSince, &r.IdleDurationDays, &r.EstimatedWasteCents,
			&r.EstimatedSavingsCents, &r.EstimatedCPUSavingsCents, &r.EstimatedMemSavingsCents,
			&r.MonitoringEndTime, &r.UpdatedAt,
			&r.ExplDataDays, &r.ExplDecayHalfLifeHours,
			&r.ExplCPUCostPctMC, &r.ExplCPUPerfPctMC,
			&r.ExplCPUUsageP95MC, &r.ExplCPUUsageP50MC, &r.ExplCPUUsageMeanMC,
			&r.ExplCPUAdaptiveMarginBP, &r.ExplCPUTrendSlope,
			&r.ExplMemCostPctKiB, &r.ExplMemPerfPctKiB,
			&r.ExplMemUsageP95KiB, &r.ExplMemUsageP50KiB, &r.ExplMemUsageMeanKiB,
			&r.ExplMemAdaptiveMarginBP, &r.ExplMemTrendSlope,
			&r.ExplOOMCountSum, &r.ExplOOMBumpApplied, &r.ExplCPUFloorApplied, &r.ExplMemFloorApplied, &r.ExplIsIdle,
			&r.SourceID, &r.ClusterAlias, &r.LastReported,
			&r.PageSortText,
		)
		if err != nil {
			return nil, fmt.Errorf("scanNativeNamespaceRows: %w", err)
		}
		rows = append(rows, r)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("scanNativeNamespaceRows iteration: %w", err)
	}
	return rows, nil
}

// scanNativeNamespaceRowsNoSort scans rows matching nativeNSSelect without page sort.
func scanNativeNamespaceRowsNoSort(sqlRows *sql.Rows, capacity int) ([]NativeNamespaceRow, error) {
	rows := make([]NativeNamespaceRow, 0, capacity)
	for sqlRows.Next() {
		var r NativeNamespaceRow
		err := sqlRows.Scan(
			&r.OrgID, &r.ClusterUUID, &r.NamespaceName, &r.Term, &r.Engine,
			&r.RecCPURequestMC, &r.RecCPULimitMC,
			&r.RecMemRequestKiB, &r.RecMemLimitKiB,
			&r.CurrentCPURequestMC, &r.CurrentCPULimitMC,
			&r.CurrentMemRequestKiB, &r.CurrentMemLimitKiB,
			&r.VariationCPURequestPct, &r.VariationCPULimitPct,
			&r.VariationMemRequestPct, &r.VariationMemLimitPct,
			&r.NotificationCodes, &r.ConfidenceLevel, &r.Stale, &r.IdleState,
			&r.IdleSince, &r.IdleDurationDays, &r.EstimatedWasteCents,
			&r.EstimatedSavingsCents, &r.EstimatedCPUSavingsCents, &r.EstimatedMemSavingsCents,
			&r.MonitoringEndTime, &r.UpdatedAt,
			&r.ExplDataDays, &r.ExplDecayHalfLifeHours,
			&r.ExplCPUCostPctMC, &r.ExplCPUPerfPctMC,
			&r.ExplCPUUsageP95MC, &r.ExplCPUUsageP50MC, &r.ExplCPUUsageMeanMC,
			&r.ExplCPUAdaptiveMarginBP, &r.ExplCPUTrendSlope,
			&r.ExplMemCostPctKiB, &r.ExplMemPerfPctKiB,
			&r.ExplMemUsageP95KiB, &r.ExplMemUsageP50KiB, &r.ExplMemUsageMeanKiB,
			&r.ExplMemAdaptiveMarginBP, &r.ExplMemTrendSlope,
			&r.ExplOOMCountSum, &r.ExplOOMBumpApplied, &r.ExplCPUFloorApplied, &r.ExplMemFloorApplied, &r.ExplIsIdle,
			&r.SourceID, &r.ClusterAlias, &r.LastReported,
		)
		if err != nil {
			return nil, fmt.Errorf("scanNativeNamespaceRowsNoSort: %w", err)
		}
		rows = append(rows, r)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("scanNativeNamespaceRowsNoSort iteration: %w", err)
	}
	return rows, nil
}
