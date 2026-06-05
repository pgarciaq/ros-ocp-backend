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

func generatePVCRecCSV(_ context.Context, w io.Writer, data []PVCRecommendationResponse) error {
	writer := csv.NewWriter(w)
	header := []string{
		"cluster_uuid", "namespace", "persistentvolumeclaim", "storageclass",
		"recommendation_type", "usage_ratio", "capacity_bytes", "usage_bytes_max",
		"estimated_monthly_savings_value", "estimated_monthly_savings_units", "term",
	}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, r := range data {
		savingsVal, savingsUnits := "", ""
		if r.EstimatedMonthlySavings != nil {
			savingsVal = r.EstimatedMonthlySavings.Value
			savingsUnits = r.EstimatedMonthlySavings.Units
		}
		if err := writer.Write([]string{
			r.ClusterUUID, r.Namespace, r.PersistentVolumeClaim, r.StorageClass,
			r.RecommendationType, fmt.Sprintf("%g", r.UsageRatio),
			strconv.FormatInt(r.CapacityBytes, 10), strconv.FormatInt(r.UsageBytesMax, 10),
			savingsVal, savingsUnits, r.Term,
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func generateQuotaRecCSV(_ context.Context, w io.Writer, data []QuotaRecommendationListItem) error {
	writer := csv.NewWriter(w)
	header := []string{
		"cluster_uuid", "namespace", "quota_name", "recommendation_type", "risk_level",
		"estimated_savings_value", "estimated_savings_units", "last_observed_at", "count",
	}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, r := range data {
		savingsVal, savingsUnits := "", ""
		if r.EstimatedSavings != nil {
			savingsVal = r.EstimatedSavings.Value
			savingsUnits = r.EstimatedSavings.Units
		}
		if err := writer.Write([]string{
			r.ClusterUUID, r.Namespace, r.QuotaName, r.RecommendationType, r.RiskLevel,
			savingsVal, savingsUnits, r.LastObservedAt, strconv.Itoa(r.Count),
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func generateClusterQuotaRecCSV(_ context.Context, w io.Writer, data []ClusterQuotaRecommendationListItem) error {
	writer := csv.NewWriter(w)
	header := []string{
		"cluster_uuid", "cluster_quota_name", "recommendation_type", "risk_level",
		"estimated_savings_value", "estimated_savings_units", "namespaces", "count",
	}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, r := range data {
		savingsVal, savingsUnits := "", ""
		if r.EstimatedSavings != nil {
			savingsVal = strconv.Itoa(r.EstimatedSavings.Value)
			savingsUnits = r.EstimatedSavings.Units
		}
		if err := writer.Write([]string{
			r.ClusterUUID, r.ClusterQuotaName, r.RecommendationType, r.RiskLevel,
			savingsVal, savingsUnits, strings.Join(r.Namespaces, ";"), strconv.Itoa(r.Count),
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
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
		if err := writer.Write([]string{
			row.ClusterUUID, row.ClusterAlias,
			row.EstimatedMonthlySavings.Value, row.EstimatedMonthlySavings.Units,
			strconv.FormatBool(row.HasCostData),
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func generateGPUMIGCSV(_ context.Context, w io.Writer, data []model.GPUMIGRecommendationEntry) error {
	writer := csv.NewWriter(w)
	header := []string{
		"cluster_uuid", "namespace", "workload", "container", "node_name", "gpu_model",
		"term", "recommended_gpu_profile", "current_gpu_profile", "gpu_classification", "confidence", "gpu_idle_state",
	}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, r := range data {
		if err := writer.Write([]string{
			r.ClusterUUID, r.Namespace, r.Workload, r.Container, r.NodeName, r.GPUModel,
			r.Term, r.RecommendedGPUProfile, r.CurrentGPUProfile, r.Classification,
			fmt.Sprintf("%g", r.Confidence), r.GPUIdleState,
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func generateNodeGPURecCSV(_ context.Context, w io.Writer, data []model.NodeGPURecommendation) error {
	writer := csv.NewWriter(w)
	header := []string{
		"node_name", "cluster_uuid", "term", "recommendation_type", "gpu_model",
		"recommended_replicas", "confidence", "savings_per_gpu_usd", "total_node_savings_usd",
	}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, r := range data {
		savingsPerGPU, totalSavings := "", ""
		if r.SavingsPerGPUUSD != nil {
			savingsPerGPU = fmt.Sprintf("%g", *r.SavingsPerGPUUSD)
		}
		if r.TotalNodeSavingsUSD != nil {
			totalSavings = fmt.Sprintf("%g", *r.TotalNodeSavingsUSD)
		}
		if err := writer.Write([]string{
			r.NodeName, r.ClusterUUID, r.Term, r.RecommendationType, r.GPUModel,
			strconv.Itoa(r.RecommendedReplicas), fmt.Sprintf("%g", r.Confidence),
			savingsPerGPU, totalSavings,
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}
