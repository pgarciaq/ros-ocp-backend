package api

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/vm"
)

var vmRecCSVHeader = []string{
	"vm_name", "namespace", "cluster_uuid", "guest_os",
	"current_vcpu", "current_memory_gib", "current_disk_gib",
	"recommended_vcpu", "recommended_memory_gib", "recommended_disk_gib",
	"recommended_instance_type", "recommended_series", "confidence", "term", "engine",
	"category",
	"is_idle", "is_abandoned", "is_oversized", "is_network_bound",
	"is_power_off_candidate", "is_redundant_placement", "has_shared_storage", "numa_oversized",
	"guest_agent_detected",
	"gpu_count", "gpu_model", "gpu_classification", "recommended_gpu_action",
	"io_pattern", "days_until_full", "growth_gib_per_day", "recommended_expand_gib",
	"estimated_monthly_savings_value", "estimated_monthly_savings_units", "notification_codes", "last_recommended_at",
}

func generateVMRecCSV(ctx context.Context, w io.Writer, items []VMRecommendationItem) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(vmRecCSVHeader); err != nil {
		return fmt.Errorf("write VM CSV header: %w", err)
	}
	for _, item := range items {
		savingsVal, savingsUnits := "", ""
		if item.Savings != nil {
			savingsVal = item.Savings.Value
			savingsUnits = item.Savings.Units
		}
		instType := ""
		if item.Recommended.InstanceType != nil {
			instType = *item.Recommended.InstanceType
		}
		series := ""
		if item.Recommended.Series != nil {
			series = *item.Recommended.Series
		}
		currentDisk := ""
		if item.Current.DiskGiB != nil {
			currentDisk = strconv.FormatInt(int64(*item.Current.DiskGiB), 10)
		}
		recommendedDisk := ""
		if item.Recommended.DiskGiB != nil {
			recommendedDisk = strconv.FormatInt(int64(*item.Recommended.DiskGiB), 10)
		}
		gpuCount, gpuModel, gpuClass, gpuAction := "", "", "", ""
		if item.GPU != nil {
			gpuCount = strconv.FormatInt(int64(item.GPU.GPUCount), 10)
			gpuModel = item.GPU.GPUModel
			gpuClass = item.GPU.GPUClassification
			gpuAction = item.GPU.RecommendedGPUAction
		}
		record := []string{
			item.VMName,
			item.Namespace,
			item.ClusterUUID,
			item.GuestOS,
			strconv.FormatInt(int64(item.Current.VCPU), 10),
			strconv.FormatInt(int64(item.Current.MemoryGiB), 10),
			currentDisk,
			strconv.FormatInt(int64(item.Recommended.VCPU), 10),
			strconv.FormatInt(int64(item.Recommended.MemoryGiB), 10),
			recommendedDisk,
			instType,
			series,
			item.Metadata.Confidence,
			item.Metadata.Term,
			item.Metadata.Engine,
			item.Category,
			strconv.FormatBool(item.Metadata.IsIdle),
			strconv.FormatBool(item.Metadata.IsAbandoned),
			strconv.FormatBool(item.Metadata.IsOversized),
			strconv.FormatBool(item.Metadata.IsNetworkBound),
			strconv.FormatBool(item.Metadata.IsPowerOffCandidate),
			strconv.FormatBool(item.Metadata.IsRedundantPlacement),
			strconv.FormatBool(item.Metadata.HasSharedStorage),
			strconv.FormatBool(item.Metadata.NUMAOversized),
			strconv.FormatBool(item.Metadata.GuestAgentDetected),
			gpuCount,
			gpuModel,
			gpuClass,
			gpuAction,
			item.IOProfile.Pattern,
			optionalInt32CSV(item.DiskProjection.DaysUntilFull),
			optionalFloat64CSV(item.DiskProjection.GrowthGiBPerDay),
			optionalInt32CSV(item.DiskProjection.RecommendedExpandGiB),
			savingsVal,
			savingsUnits,
			vmNotificationsCSV(item.Notifications),
			item.LastRecommendedAt,
		}
		if err := writer.Write(sanitizeCSVRow(record)); err != nil {
			return fmt.Errorf("write VM CSV row: %w", err)
		}
	}
	writer.Flush()
	return writer.Error()
}

var vmHistoryCSVHeader = []string{
	"id", "cluster_id", "vm_name", "namespace", "term", "engine",
	"recommended_vcpu", "recommended_memory_gib", "recommended_instance_type",
	"gpu_classification", "recommended_gpu_action",
	"category", "confidence", "created_at",
}

func generateVMHistoryCSV(ctx context.Context, w io.Writer, rows []vm.VMRecommendationHistoryRow) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(vmHistoryCSVHeader); err != nil {
		return fmt.Errorf("write VM history CSV header: %w", err)
	}
	for _, r := range rows {
		record := []string{
			strconv.FormatInt(r.ID, 10),
			r.ClusterID,
			r.VMName,
			r.Namespace,
			r.Term,
			r.Engine,
			strconv.FormatInt(int64(r.RecommendedVCPU), 10),
			strconv.FormatFloat(r.RecommendedMemoryGiB, 'f', -1, 64),
			r.RecommendedInstanceType,
			r.GPUClassification,
			r.RecommendedGPUAction,
			r.Category,
			r.Confidence,
			r.CreatedAt.UTC().Format(time.RFC3339),
		}
		if err := writer.Write(sanitizeCSVRow(record)); err != nil {
			return fmt.Errorf("write VM history CSV row: %w", err)
		}
	}
	writer.Flush()
	return writer.Error()
}
