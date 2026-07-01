package engine

import (
	"fmt"
	"math"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/types/workload"
)

const (
	// DefaultReplicaTargetUtilizationPct is the default target CPU/memory
	// utilization percentage for replica count optimization.
	DefaultReplicaTargetUtilizationPct = 70

	// MinReplicaTargetUtilizationPct is the lowest allowed value (too low = over-provisioned).
	MinReplicaTargetUtilizationPct = 10

	// MaxReplicaTargetUtilizationPct is the highest allowed value (too high = risky).
	MaxReplicaTargetUtilizationPct = 95

	// deploymentMinReplicas is the HA floor for Deployment-like workloads.
	deploymentMinReplicas int64 = 2

	// statefulsetMinReplicas is the floor for StatefulSet workloads.
	statefulsetMinReplicas int64 = 1
)

// DefaultReplicaTargetUtilizationPctFromConfig returns the effective target
// utilization percentage from the global config, falling back to the hardcoded
// default if the config is unavailable.
func DefaultReplicaTargetUtilizationPctFromConfig() int {
	cfg := config.GetConfig()
	if cfg != nil && cfg.ReplicaTargetUtilizationPct >= MinReplicaTargetUtilizationPct &&
		cfg.ReplicaTargetUtilizationPct <= MaxReplicaTargetUtilizationPct {
		return cfg.ReplicaTargetUtilizationPct
	}
	return DefaultReplicaTargetUtilizationPct
}

// ComputeRecommendedReplicas calculates the optimal replica count for a
// container recommendation based on total workload resource usage and the
// recommended per-replica resource request at a target utilization.
//
// The function populates rec.RecommendedReplicas, rec.ReplicaConfidence,
// and rec.ReplicaExplanation. It skips DaemonSets (no replica scaling)
// and workloads with insufficient data (< 2 current replicas or zero
// recommended resources).
func ComputeRecommendedReplicas(rec *ContainerRec, targetUtilPct int, latestDigest DigestRow) {
	wt := workload.WorkloadType(rec.WorkloadType)

	if wt == workload.Daemonset {
		return
	}

	currentReplicas := replicaCountForSavingsApply(rec)
	if currentReplicas < 2 || rec.RecCPURequestMC <= 0 || rec.RecMemRequestKiB <= 0 {
		return
	}

	targetUtil := float64(targetUtilPct) / 100.0

	// Total workload resource usage = per-replica P95 × current replicas.
	totalCPU := float64(latestDigest.CPUUsageP95MC) * float64(currentReplicas)
	totalMem := float64(latestDigest.MemUsageP95KiB) * float64(currentReplicas)

	minReplicasCPU := int64(math.Ceil(totalCPU / (float64(rec.RecCPURequestMC) * targetUtil)))
	minReplicasMem := int64(math.Ceil(totalMem / (float64(rec.RecMemRequestKiB) * targetUtil)))

	recommended := max(minReplicasCPU, minReplicasMem)

	minFloor := deploymentMinReplicas
	if wt == workload.Statefulset {
		minFloor = statefulsetMinReplicas
	}
	recommended = max(recommended, minFloor)

	rec.RecommendedReplicas = recommended
	rec.ReplicaConfidence = computeReplicaConfidence(rec, latestDigest)
	rec.ReplicaExplanation = buildReplicaExplanation(rec, currentReplicas, recommended, targetUtilPct, latestDigest)
}

// computeReplicaConfidence determines confidence in the replica recommendation.
// Deployments are symmetric by design → always high.
// StatefulSets may have asymmetric per-pod load; we use the P50/P95 CPU spread
// as a proxy for load asymmetry.
func computeReplicaConfidence(rec *ContainerRec, latest DigestRow) string {
	wt := workload.WorkloadType(rec.WorkloadType)

	if wt != workload.Statefulset {
		return "high"
	}

	// Unstable replica count → uncertain recommendation.
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

func buildReplicaExplanation(rec *ContainerRec, currentReplicas, recommended int64, targetUtilPct int, latest DigestRow) string {
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
func ReplicaReductionSavingsMicroCents(rec *ContainerRec, cpuRate, memRate int64) (cpuMicro, memMicro int64) {
	currentReplicas := replicaCountForSavingsApply(rec)
	if rec.RecommendedReplicas <= 0 || rec.RecommendedReplicas >= currentReplicas {
		return 0, 0
	}
	freedReplicas := currentReplicas - rec.RecommendedReplicas

	cpuMicro = CPUSavingsMicroCents(rec.RecCPURequestMC, cpuRate, HoursPerMonthInt, freedReplicas)
	memMicro = MemSavingsMicroCentsFromKiB(rec.RecMemRequestKiB, memRate, HoursPerMonthInt, freedReplicas)
	return cpuMicro, memMicro
}
