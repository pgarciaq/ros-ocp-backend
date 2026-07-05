package api

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

var pvcRecCSVHeader = []string{
	"cluster_uuid", "namespace", "persistentvolumeclaim", "mounted_by", "vm_name",
	"persistentvolume", "storageclass", "recommendation_type", "usage_ratio",
	"capacity_bytes", "usage_bytes_max", "recommended_bytes", "days_to_full",
	"growth_bytes_per_day", "estimated_monthly_savings_value", "estimated_monthly_savings_units",
	"confidence_level", "idle_since", "idle_duration_days", "data_days", "term",
	"resize_note", "notification_codes", "count",
}

func generatePVCRecCSV(_ context.Context, w io.Writer, data []PVCRecommendationResponse) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(pvcRecCSVHeader); err != nil {
		return err
	}
	for _, r := range data {
		savingsVal, savingsUnits := "", ""
		if r.EstimatedMonthlySavings != nil {
			savingsVal = r.EstimatedMonthlySavings.Value
			savingsUnits = r.EstimatedMonthlySavings.Units
		}
		recommendedBytes := ""
		if r.RecommendedBytes != nil {
			recommendedBytes = strconv.FormatInt(*r.RecommendedBytes, 10)
		}
		daysToFull := ""
		if r.DaysToFull != nil {
			daysToFull = strconv.Itoa(*r.DaysToFull)
		}
		growthBytesPerDay := ""
		if r.GrowthBytesPerDay != nil {
			growthBytesPerDay = strconv.FormatInt(*r.GrowthBytesPerDay, 10)
		}
		idleSince := ""
		if r.IdleSince != nil {
			idleSince = *r.IdleSince
		}
		idleDurationDays := ""
		if r.IdleDurationDays != nil {
			idleDurationDays = strconv.Itoa(*r.IdleDurationDays)
		}
		if err := writer.Write(sanitizeCSVRow([]string{
			r.ClusterUUID, r.Namespace, r.PersistentVolumeClaim, r.MountedBy, r.VMName,
			r.PersistentVolume, r.StorageClass, r.RecommendationType, fmt.Sprintf("%g", r.UsageRatio),
			strconv.FormatInt(r.CapacityBytes, 10), strconv.FormatInt(r.UsageBytesMax, 10),
			recommendedBytes, daysToFull, growthBytesPerDay,
			savingsVal, savingsUnits,
			fmt.Sprintf("%g", r.ConfidenceLevel),
			idleSince, idleDurationDays, strconv.Itoa(r.DataDays), r.Term,
			r.ResizeNote, notificationMapCodesStr(r.Notifications), strconv.Itoa(r.Count),
		})); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

var quotaRecCSVHeader = []string{
	"cluster_uuid", "namespace", "quota_name", "recommendation_type", "risk_level",
	"quota_hard_cpu_request_millicores", "quota_hard_cpu_limit_millicores",
	"quota_hard_memory_request_bytes", "quota_hard_memory_limit_bytes",
	"quota_hard_storage_request_bytes", "quota_hard_pods",
	"quota_used_cpu_request_millicores", "quota_used_cpu_limit_millicores",
	"quota_used_memory_request_bytes", "quota_used_memory_limit_bytes",
	"quota_used_storage_request_bytes", "quota_used_pods",
	"quota_recommended_cpu_request_millicores", "quota_recommended_cpu_limit_millicores",
	"quota_recommended_memory_request_bytes", "quota_recommended_memory_limit_bytes",
	"quota_recommended_storage_request_bytes", "quota_recommended_pods",
	"utilization_cpu_request_percent", "utilization_cpu_limit_percent",
	"utilization_memory_request_percent", "utilization_memory_limit_percent",
	"utilization_storage_request_percent", "utilization_pods_percent",
	"capacity_freed_cpu_millicores", "capacity_freed_memory_bytes",
	"capacity_freed_storage_request_bytes", "capacity_freed_pods",
	"estimated_savings_value", "estimated_savings_units", "last_observed_at",
	"notification_codes", "count",
}

func generateQuotaRecCSV(_ context.Context, w io.Writer, data []QuotaRecommendationListItem) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(quotaRecCSVHeader); err != nil {
		return err
	}
	for _, r := range data {
		savingsVal, savingsUnits := "", ""
		if r.EstimatedSavings != nil {
			savingsVal = r.EstimatedSavings.Value
			savingsUnits = r.EstimatedSavings.Units
		}
		if err := writer.Write(sanitizeCSVRow([]string{
			r.ClusterUUID, r.Namespace, r.QuotaName, r.RecommendationType, r.RiskLevel,
			quotaResourceCSV(r.QuotaHard, quotaFieldCPUReq),
			quotaResourceCSV(r.QuotaHard, quotaFieldCPULim),
			quotaResourceCSV(r.QuotaHard, quotaFieldMemReq),
			quotaResourceCSV(r.QuotaHard, quotaFieldMemLim),
			quotaResourceCSV(r.QuotaHard, quotaFieldStorage),
			quotaResourceCSV(r.QuotaHard, quotaFieldPods),
			quotaResourceCSV(r.QuotaUsed, quotaFieldCPUReq),
			quotaResourceCSV(r.QuotaUsed, quotaFieldCPULim),
			quotaResourceCSV(r.QuotaUsed, quotaFieldMemReq),
			quotaResourceCSV(r.QuotaUsed, quotaFieldMemLim),
			quotaResourceCSV(r.QuotaUsed, quotaFieldStorage),
			quotaResourceCSV(r.QuotaUsed, quotaFieldPods),
			quotaResourceCSV(r.QuotaRecommended, quotaFieldCPUReq),
			quotaResourceCSV(r.QuotaRecommended, quotaFieldCPULim),
			quotaResourceCSV(r.QuotaRecommended, quotaFieldMemReq),
			quotaResourceCSV(r.QuotaRecommended, quotaFieldMemLim),
			quotaResourceCSV(r.QuotaRecommended, quotaFieldStorage),
			quotaResourceCSV(r.QuotaRecommended, quotaFieldPods),
			quotaUtilCSV(r.Utilization, quotaUtilCPUReq),
			quotaUtilCSV(r.Utilization, quotaUtilCPULim),
			quotaUtilCSV(r.Utilization, quotaUtilMemReq),
			quotaUtilCSV(r.Utilization, quotaUtilMemLim),
			quotaUtilCSV(r.Utilization, quotaUtilStorage),
			quotaUtilCSV(r.Utilization, quotaUtilPods),
			quotaCapacityFreedCSV(r.CapacityFreed, quotaFreedCPU),
			quotaCapacityFreedCSV(r.CapacityFreed, quotaFreedMem),
			quotaCapacityFreedCSV(r.CapacityFreed, quotaFreedStorage),
			quotaCapacityFreedCSV(r.CapacityFreed, quotaFreedPods),
			savingsVal, savingsUnits, r.LastObservedAt,
			notificationMapCodesStr(r.Notifications), strconv.Itoa(r.Count),
		})); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

type quotaCSVField int

const (
	quotaFieldCPUReq quotaCSVField = iota
	quotaFieldCPULim
	quotaFieldMemReq
	quotaFieldMemLim
	quotaFieldStorage
	quotaFieldPods
)

func quotaResourceCSV(v *QuotaResourceValues, field quotaCSVField) string {
	if v == nil {
		return ""
	}
	switch field {
	case quotaFieldCPUReq:
		return optionalInt64CSV(v.CPURequestMillicores)
	case quotaFieldCPULim:
		return optionalInt64CSV(v.CPULimitMillicores)
	case quotaFieldMemReq:
		return optionalInt64CSV(v.MemoryRequestBytes)
	case quotaFieldMemLim:
		return optionalInt64CSV(v.MemoryLimitBytes)
	case quotaFieldStorage:
		return optionalInt64CSV(v.StorageRequestBytes)
	case quotaFieldPods:
		return optionalInt64CSV(v.Pods)
	default:
		return ""
	}
}

type quotaUtilField int

const (
	quotaUtilCPUReq quotaUtilField = iota
	quotaUtilCPULim
	quotaUtilMemReq
	quotaUtilMemLim
	quotaUtilStorage
	quotaUtilPods
)

func quotaUtilCSV(v *QuotaUtilizationPercents, field quotaUtilField) string {
	if v == nil {
		return ""
	}
	switch field {
	case quotaUtilCPUReq:
		return optionalFloat64CSV(v.CPURequestPercent)
	case quotaUtilCPULim:
		return optionalFloat64CSV(v.CPULimitPercent)
	case quotaUtilMemReq:
		return optionalFloat64CSV(v.MemoryRequestPercent)
	case quotaUtilMemLim:
		return optionalFloat64CSV(v.MemoryLimitPercent)
	case quotaUtilStorage:
		return optionalFloat64CSV(v.StorageRequestPercent)
	case quotaUtilPods:
		return optionalFloat64CSV(v.PodsPercent)
	default:
		return ""
	}
}

type quotaFreedField int

const (
	quotaFreedCPU quotaFreedField = iota
	quotaFreedMem
	quotaFreedStorage
	quotaFreedPods
)

func quotaCapacityFreedCSV(v *QuotaCapacityFreedResponse, field quotaFreedField) string {
	if v == nil {
		return ""
	}
	switch field {
	case quotaFreedCPU:
		return strconv.FormatInt(v.CPUMillicores, 10)
	case quotaFreedMem:
		return strconv.FormatInt(v.MemoryBytes, 10)
	case quotaFreedStorage:
		return strconv.FormatInt(v.StorageRequestBytes, 10)
	case quotaFreedPods:
		return strconv.FormatInt(v.PodsFreed, 10)
	default:
		return ""
	}
}

var clusterQuotaRecCSVHeader = []string{
	"cluster_uuid", "cluster_quota_name", "recommendation_type", "risk_level",
	"quota_hard_cpu_request_millicores", "quota_hard_cpu_limit_millicores",
	"quota_hard_memory_request_bytes", "quota_hard_memory_limit_bytes",
	"quota_hard_storage_request_bytes", "quota_hard_pods",
	"quota_used_cpu_request_millicores", "quota_used_cpu_limit_millicores",
	"quota_used_memory_request_bytes", "quota_used_memory_limit_bytes",
	"quota_used_storage_request_bytes", "quota_used_pods",
	"quota_recommended_cpu_request_millicores", "quota_recommended_cpu_limit_millicores",
	"quota_recommended_memory_request_bytes", "quota_recommended_memory_limit_bytes",
	"quota_recommended_storage_request_bytes", "quota_recommended_pods",
	"utilization_cpu_request_percent", "utilization_memory_request_percent",
	"utilization_storage_request_percent", "utilization_pods_percent",
	"capacity_freed_cpu_cores", "capacity_freed_memory_bytes",
	"capacity_freed_storage_request_bytes", "capacity_freed_pods",
	"estimated_savings_value", "estimated_savings_units", "namespaces",
	"notification_codes", "count",
}

func generateClusterQuotaRecCSV(_ context.Context, w io.Writer, data []ClusterQuotaRecommendationListItem) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(clusterQuotaRecCSVHeader); err != nil {
		return err
	}
	for _, r := range data {
		savingsVal, savingsUnits := "", ""
		if r.EstimatedSavings != nil {
			savingsVal = r.EstimatedSavings.Value
			savingsUnits = r.EstimatedSavings.Units
		}
		if err := writer.Write(sanitizeCSVRow([]string{
			r.ClusterUUID, r.ClusterQuotaName, r.RecommendationType, r.RiskLevel,
			clusterQuotaResourceCSV(r.QuotaHard, quotaFieldCPUReq),
			clusterQuotaResourceCSV(r.QuotaHard, quotaFieldCPULim),
			clusterQuotaResourceCSV(r.QuotaHard, quotaFieldMemReq),
			clusterQuotaResourceCSV(r.QuotaHard, quotaFieldMemLim),
			clusterQuotaResourceCSV(r.QuotaHard, quotaFieldStorage),
			clusterQuotaResourceCSV(r.QuotaHard, quotaFieldPods),
			clusterQuotaResourceCSV(r.QuotaUsed, quotaFieldCPUReq),
			clusterQuotaResourceCSV(r.QuotaUsed, quotaFieldCPULim),
			clusterQuotaResourceCSV(r.QuotaUsed, quotaFieldMemReq),
			clusterQuotaResourceCSV(r.QuotaUsed, quotaFieldMemLim),
			clusterQuotaResourceCSV(r.QuotaUsed, quotaFieldStorage),
			clusterQuotaResourceCSV(r.QuotaUsed, quotaFieldPods),
			clusterQuotaResourceCSV(r.QuotaRecommended, quotaFieldCPUReq),
			clusterQuotaResourceCSV(r.QuotaRecommended, quotaFieldCPULim),
			clusterQuotaResourceCSV(r.QuotaRecommended, quotaFieldMemReq),
			clusterQuotaResourceCSV(r.QuotaRecommended, quotaFieldMemLim),
			clusterQuotaResourceCSV(r.QuotaRecommended, quotaFieldStorage),
			clusterQuotaResourceCSV(r.QuotaRecommended, quotaFieldPods),
			clusterQuotaUtilCSV(r.Utilization, clusterQuotaUtilCPUReq),
			clusterQuotaUtilCSV(r.Utilization, clusterQuotaUtilMemReq),
			clusterQuotaUtilCSV(r.Utilization, clusterQuotaUtilStorage),
			clusterQuotaUtilCSV(r.Utilization, clusterQuotaUtilPods),
			clusterQuotaCapacityFreedCSV(r.CapacityFreed, clusterQuotaFreedCPU),
			clusterQuotaCapacityFreedCSV(r.CapacityFreed, clusterQuotaFreedMem),
			clusterQuotaCapacityFreedCSV(r.CapacityFreed, clusterQuotaFreedStorage),
			clusterQuotaCapacityFreedCSV(r.CapacityFreed, clusterQuotaFreedPods),
			savingsVal, savingsUnits, strings.Join(r.Namespaces, ";"),
			notificationMapCodesStr(r.Notifications), strconv.Itoa(r.Count),
		})); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func clusterQuotaResourceCSV(v *ClusterQuotaResourceValues, field quotaCSVField) string {
	if v == nil {
		return ""
	}
	switch field {
	case quotaFieldCPUReq:
		return optionalInt64CSV(v.CPURequestMillicores)
	case quotaFieldCPULim:
		return optionalInt64CSV(v.CPULimitMillicores)
	case quotaFieldMemReq:
		return optionalInt64CSV(v.MemoryRequestBytes)
	case quotaFieldMemLim:
		return optionalInt64CSV(v.MemoryLimitBytes)
	case quotaFieldStorage:
		return optionalInt64CSV(v.StorageRequestBytes)
	case quotaFieldPods:
		return optionalInt64CSV(v.Pods)
	default:
		return ""
	}
}

type clusterQuotaUtilField int

const (
	clusterQuotaUtilCPUReq clusterQuotaUtilField = iota
	clusterQuotaUtilMemReq
	clusterQuotaUtilStorage
	clusterQuotaUtilPods
)

func clusterQuotaUtilCSV(v *ClusterQuotaUtilizationPercents, field clusterQuotaUtilField) string {
	if v == nil {
		return ""
	}
	switch field {
	case clusterQuotaUtilCPUReq:
		return optionalFloat64CSV(v.CPURequestPercent)
	case clusterQuotaUtilMemReq:
		return optionalFloat64CSV(v.MemoryRequestPercent)
	case clusterQuotaUtilStorage:
		return optionalFloat64CSV(v.StorageRequestPercent)
	case clusterQuotaUtilPods:
		return optionalFloat64CSV(v.PodsPercent)
	default:
		return ""
	}
}

type clusterQuotaFreedField int

const (
	clusterQuotaFreedCPU clusterQuotaFreedField = iota
	clusterQuotaFreedMem
	clusterQuotaFreedStorage
	clusterQuotaFreedPods
)

func clusterQuotaCapacityFreedCSV(v *ClusterQuotaCapacityFreedResponse, field clusterQuotaFreedField) string {
	if v == nil {
		return ""
	}
	switch field {
	case clusterQuotaFreedCPU:
		return strconv.FormatInt(v.CPUCoresFreed, 10)
	case clusterQuotaFreedMem:
		return strconv.FormatInt(v.MemoryBytes, 10)
	case clusterQuotaFreedStorage:
		return strconv.FormatInt(v.StorageRequestBytes, 10)
	case clusterQuotaFreedPods:
		return strconv.FormatInt(v.PodsFreed, 10)
	default:
		return ""
	}
}

func generateFleetSavingsSummaryCSV(_ context.Context, w io.Writer, resp FleetSavingsSummaryResponse) error {
	writer := csv.NewWriter(w)
	header := []string{
		"cluster_uuid", "cluster_alias", "estimated_monthly_savings_value", "estimated_monthly_savings_units", "has_cost_data",
	}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, row := range resp.ByCluster {
		if err := writer.Write(sanitizeCSVRow([]string{
			row.ClusterUUID, row.ClusterAlias,
			row.EstimatedMonthlySavings.Value, row.EstimatedMonthlySavings.Units,
			strconv.FormatBool(row.HasCostData),
		})); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

var gpuMIGCSVHeader = []string{
	"cluster_uuid", "namespace", "workload", "container", "node_name", "gpu_model",
	"term", "recommended_gpu_profile", "current_gpu_profile", "gpu_classification", "confidence", "gpu_idle_state",
}

func generateGPUMIGCSV(_ context.Context, w io.Writer, data []model.GPUMIGRecommendationEntry) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(gpuMIGCSVHeader); err != nil {
		return err
	}
	for _, r := range data {
		if err := writer.Write(sanitizeCSVRow([]string{
			r.ClusterUUID, r.Namespace, r.Workload, r.Container, r.NodeName, r.GPUModel,
			r.Term, r.RecommendedGPUProfile, r.CurrentGPUProfile, r.Classification,
			fmt.Sprintf("%g", r.Confidence), r.GPUIdleState,
		})); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

var nodeGPURecCSVHeader = []string{
	"node_name", "cluster_uuid", "term", "recommendation_type", "gpu_model",
	"recommended_replicas", "confidence", "savings_per_gpu_value", "savings_per_gpu_units",
	"total_node_savings_value", "total_node_savings_units",
	"candidate_containers", "impacted_containers", "notification_codes",
}

func generateNodeGPURecCSV(_ context.Context, w io.Writer, data []model.NodeGPURecommendation) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(nodeGPURecCSVHeader); err != nil {
		return err
	}
	for _, r := range data {
		savingsPerGPUVal, savingsPerGPUUnits := "", ""
		totalSavingsVal, totalSavingsUnits := "", ""
		if r.SavingsPerGPU != nil {
			savingsPerGPUVal = r.SavingsPerGPU.Value
			savingsPerGPUUnits = r.SavingsPerGPU.Units
		}
		if r.EstimatedMonthlySavings != nil {
			totalSavingsVal = r.EstimatedMonthlySavings.Value
			totalSavingsUnits = r.EstimatedMonthlySavings.Units
		}
		if err := writer.Write(sanitizeCSVRow([]string{
			r.NodeName, r.ClusterUUID, r.Term, r.RecommendationType, r.GPUModel,
			strconv.Itoa(r.RecommendedReplicas), fmt.Sprintf("%g", r.Confidence),
			savingsPerGPUVal, savingsPerGPUUnits, totalSavingsVal, totalSavingsUnits,
			nodeContainerRefsStr(r.CandidateContainers),
			nodeContainerRefsStr(r.ImpactedContainers),
			int16SliceStr(r.NotificationCodes),
		})); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

var machineSetRecCSVHeader = []string{
	"machineset_name", "cluster_uuid", "cluster_alias", "instance_type", "term",
	"current_node_count", "recommended_node_count", "excess_nodes",
	"total_monthly_savings_value", "total_monthly_savings_units",
	"avg_cpu_utilization", "avg_memory_utilization", "nodes",
}

func generateMachineSetRecCSV(_ context.Context, w io.Writer, term string, data []model.MachineSetRecommendation) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(machineSetRecCSVHeader); err != nil {
		return err
	}
	for _, r := range data {
		nodes := strings.Join(r.Nodes, ";")
		savingsVal, savingsUnits := "", ""
		if r.TotalMonthlySavings != nil {
			savingsVal = r.TotalMonthlySavings.Value
			savingsUnits = r.TotalMonthlySavings.Units
		}
		if err := writer.Write(sanitizeCSVRow([]string{
			r.MachineSetName,
			r.ClusterUUID,
			r.ClusterAlias,
			r.InstanceType,
			term,
			strconv.Itoa(r.CurrentNodeCount),
			strconv.Itoa(r.RecommendedNodeCount),
			strconv.Itoa(r.ExcessNodes),
			savingsVal,
			savingsUnits,
			fmt.Sprintf("%g", r.AvgCPUUtilization),
			fmt.Sprintf("%g", r.AvgMemoryUtilization),
			nodes,
		})); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

var snapshotRecCSVHeader = []string{
	"cluster_uuid", "namespace", "snapshot_name", "source_pvc_name",
	"classification", "age_days", "restore_size_bytes",
	"estimated_monthly_cost_value", "estimated_monthly_cost_units",
	"source_pvc_exists", "last_restored_at",
	"notification_codes", "created_at", "last_reported", "count",
}

func generateSnapshotRecCSV(_ context.Context, w io.Writer, data []SnapshotRecommendationResponse) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(snapshotRecCSVHeader); err != nil {
		return err
	}
	for _, r := range data {
		costVal, costUnits := "", ""
		if r.EstimatedMonthlyCost != nil {
			costVal = r.EstimatedMonthlyCost.Value
			costUnits = r.EstimatedMonthlyCost.Units
		}
		if err := writer.Write(sanitizeCSVRow([]string{
			r.ClusterUUID, r.Namespace, r.SnapshotName, r.SourcePVCName,
			r.RecommendationType, strconv.Itoa(r.AgeDays), strconv.FormatInt(r.RestoreSizeBytes, 10),
			costVal, costUnits,
			strconv.FormatBool(r.SourcePVCExists), "",
			notificationMapCodesStr(r.Notifications), r.CreationTimestamp, r.LastReported,
			strconv.Itoa(r.Count),
		})); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}
