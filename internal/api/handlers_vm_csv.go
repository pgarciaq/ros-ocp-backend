package api

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

var vmRecCSVHeader = []string{
	"vm_name", "namespace", "cluster_uuid", "guest_os",
	"current_vcpu", "current_memory_gib", "recommended_vcpu", "recommended_memory_gib",
	"recommended_instance_type", "confidence", "term", "engine",
	"is_idle", "is_abandoned", "is_oversized", "is_network_bound",
	"guest_agent_detected", "savings_value", "savings_units",
	"last_recommended_at",
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
		record := []string{
			item.VMName,
			item.Namespace,
			item.ClusterUUID,
			item.GuestOS,
			strconv.FormatInt(int64(item.Current.VCPU), 10),
			strconv.FormatInt(int64(item.Current.MemoryGiB), 10),
			strconv.FormatInt(int64(item.Recommended.VCPU), 10),
			strconv.FormatInt(int64(item.Recommended.MemoryGiB), 10),
			instType,
			item.Metadata.Confidence,
			item.Metadata.Term,
			item.Metadata.Engine,
			strconv.FormatBool(item.Metadata.IsIdle),
			strconv.FormatBool(item.Metadata.IsAbandoned),
			strconv.FormatBool(item.Metadata.IsOversized),
			strconv.FormatBool(item.Metadata.IsNetworkBound),
			strconv.FormatBool(item.Metadata.GuestAgentDetected),
			savingsVal,
			savingsUnits,
			item.LastRecommendedAt,
		}
		if err := writer.Write(record); err != nil {
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
	"is_idle", "is_abandoned", "confidence", "created_at",
}

func generateVMHistoryCSV(ctx context.Context, w io.Writer, rows []engine.VMRecommendationHistoryRow) error {
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
			strconv.FormatBool(r.IsIdle),
			strconv.FormatBool(r.IsAbandoned),
			r.Confidence,
			r.CreatedAt.UTC().Format(time.RFC3339),
		}
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("write VM history CSV row: %w", err)
		}
	}
	writer.Flush()
	return writer.Error()
}
