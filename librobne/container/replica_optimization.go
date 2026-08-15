package container

import (
	"fmt"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

const (
	// DefaultReplicaTargetUtilizationPct is the default target CPU/memory
	// utilization percentage for replica count optimization.
	DefaultReplicaTargetUtilizationPct = 70

	// MinReplicaTargetUtilizationPct is the lowest allowed value (too low = over-provisioned).
	MinReplicaTargetUtilizationPct = 10

	// MaxReplicaTargetUtilizationPct is the highest allowed value (too high = risky).
	MaxReplicaTargetUtilizationPct = 95

	deploymentMinReplicas  int64 = 2
	statefulsetMinReplicas int64 = 1
)

// ComputeRecommendedReplicas calculates the optimal replica count for a
// container recommendation based on total workload resource usage and the
// recommended per-replica resource request at a target utilization.
//
// Integer overflow safety: the numerator is int64(CPUUsageP95MC) * int64(currentReplicas) * 100.
// CPUUsageP95MC and MemUsageP95KiB originate from DigestRow (int32, max ~2.1e9).
// currentReplicas is derived from PodCount fields (int32). The worst-case product
// is ~2.1e9 * 2.1e9 * 100 = ~4.4e20, which exceeds int64 max (~9.2e18). However,
// CPUUsageP95MC > 2.1 million (2100 cores) combined with > 4400 replicas is
// physically implausible. For any realistic workload the product stays well within
// int64 range.
func ComputeRecommendedReplicas(rec *types.ContainerRec, targetUtilPct int, latestDigest types.DigestRow) {
	if rec.WorkloadType == "daemonset" {
		return
	}

	currentReplicas := types.ReplicaCountForSavingsApply(rec)
	if currentReplicas < 2 || rec.RecCPURequestMC <= 0 || rec.RecMemRequestKiB <= 0 {
		return
	}

	cpuNumer := int64(latestDigest.CPUUsageP95MC) * int64(currentReplicas) * 100
	cpuDenom := int64(rec.RecCPURequestMC) * int64(targetUtilPct)
	minReplicasCPU := (cpuNumer + cpuDenom - 1) / cpuDenom

	memNumer := int64(latestDigest.MemUsageP95KiB) * int64(currentReplicas) * 100
	memDenom := int64(rec.RecMemRequestKiB) * int64(targetUtilPct)
	minReplicasMem := (memNumer + memDenom - 1) / memDenom

	recommended := max(minReplicasCPU, minReplicasMem)

	minFloor := deploymentMinReplicas
	if rec.WorkloadType == "statefulset" {
		minFloor = statefulsetMinReplicas
	}
	recommended = max(recommended, minFloor)

	rec.RecommendedReplicas = recommended
	rec.ReplicaConfidence = computeReplicaConfidence(rec, latestDigest)
	rec.ReplicaExplanation = buildReplicaExplanation(currentReplicas, recommended, targetUtilPct, latestDigest)
}

func computeReplicaConfidence(rec *types.ContainerRec, latest types.DigestRow) string {
	if rec.WorkloadType != "statefulset" {
		return "high"
	}

	if latest.CPUUsageCVBP != nil {
		cv := *latest.CPUUsageCVBP
		switch {
		case cv < 1500:
			return "high"
		case cv < 3000:
			return "medium"
		default:
			return "low"
		}
	}

	if rec.PodCountMin != rec.PodCountMax || rec.PodCountMin < 2 {
		return "medium"
	}

	if latest.CPUUsageP95MC <= 0 {
		return "medium"
	}
	spread := float64(latest.CPUUsageP95MC-latest.CPUUsageP50MC) / float64(latest.CPUUsageP95MC)

	if spread < 0.25 {
		return "high"
	} else if spread < 0.40 {
		return "medium"
	}
	return "low"
}

func buildReplicaExplanation(currentReplicas, recommended int64, targetUtilPct int, latest types.DigestRow) string {
	if recommended < currentReplicas {
		return fmt.Sprintf(
			"Workload can be consolidated from %d to %d replicas at %d%% target utilization (P95 CPU: %d mc, P95 mem: %d KiB per replica).",
			currentReplicas, recommended, targetUtilPct, latest.CPUUsageP95MC, latest.MemUsageP95KiB,
		)
	} else if recommended > currentReplicas {
		return fmt.Sprintf(
			"Workload needs %d replicas (up from %d) to stay within %d%% target utilization (P95 CPU: %d mc, P95 mem: %d KiB per replica).",
			recommended, currentReplicas, targetUtilPct, latest.CPUUsageP95MC, latest.MemUsageP95KiB,
		)
	}
	return fmt.Sprintf(
		"Current replica count (%d) is optimal at %d%% target utilization.",
		currentReplicas, targetUtilPct,
	)
}

// ReplicaReductionSavingsMicroCents computes the additional savings from reducing
// replica count. When recommended_replicas < current replicas, the freed replicas'
// full resource cost is recoverable.
func ReplicaReductionSavingsMicroCents(rec *types.ContainerRec, cpuRate, memRate, hoursPerMonth int64) (cpuMicro, memMicro int64) {
	currentReplicas := types.ReplicaCountForSavingsApply(rec)
	if rec.RecommendedReplicas <= 0 || rec.RecommendedReplicas >= currentReplicas {
		return 0, 0
	}
	freedReplicas := currentReplicas - rec.RecommendedReplicas

	cpuMicro = types.CPUSavingsMicroCents(rec.RecCPURequestMC, cpuRate, hoursPerMonth, freedReplicas)
	memMicro = types.MemSavingsMicroCentsFromKiB(rec.RecMemRequestKiB, memRate, hoursPerMonth, freedReplicas)
	return cpuMicro, memMicro
}
