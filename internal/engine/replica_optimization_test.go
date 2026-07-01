package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeRecommendedReplicas_DeploymentOverReplicated(t *testing.T) {
	rec := ContainerRec{
		WorkloadType:     "deployment",
		RecCPURequestMC:  200,
		RecMemRequestKiB: 1024,
		DesiredReplicas:  10,
		PodCountMin:      10,
		PodCountMax:      10,
		PodCountAvg:      10,
	}
	digest := DigestRow{
		CPUUsageP95MC:  100,
		CPUUsageP50MC:  80,
		MemUsageP95KiB: 512,
	}

	ComputeRecommendedReplicas(&rec, 70, digest)

	// totalCPU = 100 * 10 = 1000
	// minCPU = ceil(1000 / (200 * 0.7)) = ceil(7.14) = 8
	// totalMem = 512 * 10 = 5120
	// minMem = ceil(5120 / (1024 * 0.7)) = ceil(7.14) = 8
	// recommended = max(8, 8) = 8, floor = 2, final = 8
	assert.Equal(t, int64(8), rec.RecommendedReplicas)
	assert.Equal(t, "high", rec.ReplicaConfidence)
	assert.NotEmpty(t, rec.ReplicaExplanation)
	assert.Contains(t, rec.ReplicaExplanation, "consolidated")
}

func TestComputeRecommendedReplicas_DeploymentUnderReplicated(t *testing.T) {
	rec := ContainerRec{
		WorkloadType:     "deployment",
		RecCPURequestMC:  100,
		RecMemRequestKiB: 512,
		DesiredReplicas:  2,
		PodCountMin:     2,
		PodCountMax:     2,
		PodCountAvg:     2,
	}
	digest := DigestRow{
		CPUUsageP95MC:  90,
		CPUUsageP50MC:  80,
		MemUsageP95KiB: 450,
	}

	ComputeRecommendedReplicas(&rec, 70, digest)

	// totalCPU = 90 * 2 = 180
	// minCPU = ceil(180 / (100 * 0.7)) = ceil(2.57) = 3
	// totalMem = 450 * 2 = 900
	// minMem = ceil(900 / (512 * 0.7)) = ceil(2.51) = 3
	// recommended = max(3, 3) = 3, floor = 2, final = 3
	assert.Equal(t, int64(3), rec.RecommendedReplicas)
	assert.Equal(t, "high", rec.ReplicaConfidence)
	assert.Contains(t, rec.ReplicaExplanation, "needs 3 replicas")
}

func TestComputeRecommendedReplicas_DeploymentOptimal(t *testing.T) {
	rec := ContainerRec{
		WorkloadType:     "deployment",
		RecCPURequestMC:  200,
		RecMemRequestKiB: 1024,
		DesiredReplicas:  3,
		PodCountMin:     3,
		PodCountMax:     3,
		PodCountAvg:     3,
	}
	digest := DigestRow{
		CPUUsageP95MC:  130,
		CPUUsageP50MC:  100,
		MemUsageP95KiB: 700,
	}

	ComputeRecommendedReplicas(&rec, 70, digest)

	// totalCPU = 130 * 3 = 390
	// minCPU = ceil(390 / (200 * 0.7)) = ceil(2.79) = 3
	// totalMem = 700 * 3 = 2100
	// minMem = ceil(2100 / (1024 * 0.7)) = ceil(2.93) = 3
	// recommended = max(3, 3) = 3, floor = 2, final = 3
	assert.Equal(t, int64(3), rec.RecommendedReplicas)
	assert.Contains(t, rec.ReplicaExplanation, "optimal")
}

func TestComputeRecommendedReplicas_DeploymentMinFloor(t *testing.T) {
	rec := ContainerRec{
		WorkloadType:     "deployment",
		RecCPURequestMC:  1000,
		RecMemRequestKiB: 4096,
		DesiredReplicas:  5,
		PodCountMin:     5,
		PodCountMax:     5,
		PodCountAvg:     5,
	}
	digest := DigestRow{
		CPUUsageP95MC:  10,
		CPUUsageP50MC:  5,
		MemUsageP95KiB: 100,
	}

	ComputeRecommendedReplicas(&rec, 70, digest)

	// totalCPU = 10 * 5 = 50
	// minCPU = ceil(50 / (1000 * 0.7)) = ceil(0.071) = 1
	// totalMem = 100 * 5 = 500
	// minMem = ceil(500 / (4096 * 0.7)) = ceil(0.17) = 1
	// recommended = max(1, 1) = 1, floor = 2 (Deployment HA), final = 2
	assert.Equal(t, int64(2), rec.RecommendedReplicas)
	assert.Equal(t, "high", rec.ReplicaConfidence)
}

func TestComputeRecommendedReplicas_StatefulSetMinFloor(t *testing.T) {
	rec := ContainerRec{
		WorkloadType:     "statefulset",
		RecCPURequestMC:  1000,
		RecMemRequestKiB: 4096,
		DesiredReplicas:  5,
		PodCountMin:     5,
		PodCountMax:     5,
		PodCountAvg:     5,
	}
	digest := DigestRow{
		CPUUsageP95MC:  10,
		CPUUsageP50MC:  5,
		MemUsageP95KiB: 100,
	}

	ComputeRecommendedReplicas(&rec, 70, digest)

	// Same math as above but floor = 1 for StatefulSet
	assert.Equal(t, int64(1), rec.RecommendedReplicas)
}

func TestComputeRecommendedReplicas_DaemonSetSkipped(t *testing.T) {
	rec := ContainerRec{
		WorkloadType:     "daemonset",
		RecCPURequestMC:  200,
		RecMemRequestKiB: 1024,
		DesiredReplicas:  3,
		PodCountMin:     3,
		PodCountMax:     3,
		PodCountAvg:     3,
	}
	digest := DigestRow{
		CPUUsageP95MC:  100,
		CPUUsageP50MC:  80,
		MemUsageP95KiB: 512,
	}

	ComputeRecommendedReplicas(&rec, 70, digest)

	assert.Equal(t, int64(0), rec.RecommendedReplicas)
	assert.Empty(t, rec.ReplicaConfidence)
	assert.Empty(t, rec.ReplicaExplanation)
}

func TestComputeRecommendedReplicas_SingleReplicaSkipped(t *testing.T) {
	rec := ContainerRec{
		WorkloadType:     "deployment",
		RecCPURequestMC:  200,
		RecMemRequestKiB: 1024,
		DesiredReplicas:  1,
		PodCountMin:     1,
		PodCountMax:     1,
		PodCountAvg:     1,
	}
	digest := DigestRow{
		CPUUsageP95MC:  100,
		CPUUsageP50MC:  80,
		MemUsageP95KiB: 512,
	}

	ComputeRecommendedReplicas(&rec, 70, digest)

	assert.Equal(t, int64(0), rec.RecommendedReplicas)
}

func TestComputeRecommendedReplicas_ZeroRecommendedResources(t *testing.T) {
	rec := ContainerRec{
		WorkloadType:     "deployment",
		RecCPURequestMC:  0,
		RecMemRequestKiB: 1024,
		DesiredReplicas:  3,
		PodCountMin:     3,
		PodCountMax:     3,
		PodCountAvg:     3,
	}
	digest := DigestRow{
		CPUUsageP95MC:  100,
		MemUsageP95KiB: 512,
	}

	ComputeRecommendedReplicas(&rec, 70, digest)

	assert.Equal(t, int64(0), rec.RecommendedReplicas)
}

// --- Confidence tests ---

func TestComputeReplicaConfidence_DeploymentAlwaysHigh(t *testing.T) {
	rec := ContainerRec{
		WorkloadType: "deployment",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	digest := DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 50,
	}

	assert.Equal(t, "high", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_StatefulSetLowSpread(t *testing.T) {
	rec := ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	digest := DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 90, // spread = (100-90)/100 = 0.10 < 0.25 → high
	}

	assert.Equal(t, "high", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_StatefulSetMediumSpread(t *testing.T) {
	rec := ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	digest := DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 70, // spread = 0.30, 0.25 ≤ 0.30 < 0.40 → medium
	}

	assert.Equal(t, "medium", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_StatefulSetHighSpread(t *testing.T) {
	rec := ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	digest := DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 50, // spread = 0.50 ≥ 0.40 → low
	}

	assert.Equal(t, "low", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_StatefulSetUnstablePodCount(t *testing.T) {
	rec := ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  2,
		PodCountMax:  5, // unstable
	}
	digest := DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 90,
	}

	assert.Equal(t, "medium", computeReplicaConfidence(&rec, digest))
}

// --- Replica reduction savings tests ---

func TestReplicaReductionSavingsMicroCents_ReductionPresent(t *testing.T) {
	rec := ContainerRec{
		RecCPURequestMC:     200,
		RecMemRequestKiB:    1024,
		DesiredReplicas:     5,
		PodCountAvg:         5,
		RecommendedReplicas: 3,
	}

	cpuRate := int64(1000)
	memRate := int64(500)

	cpuSavings, memSavings := ReplicaReductionSavingsMicroCents(&rec, cpuRate, memRate)

	// freedReplicas = 5 - 3 = 2
	// cpuSavings = CPUSavingsMicroCents(200, 1000, 730, 2)
	// memSavings = MemSavingsMicroCentsFromKiB(1024, 500, 730, 2)
	assert.Greater(t, cpuSavings, int64(0))
	assert.Greater(t, memSavings, int64(0))
}

func TestReplicaReductionSavingsMicroCents_NoReduction(t *testing.T) {
	rec := ContainerRec{
		RecCPURequestMC:     200,
		RecMemRequestKiB:    1024,
		DesiredReplicas:     3,
		PodCountAvg:         3,
		RecommendedReplicas: 5, // recommending more, not fewer
	}

	cpuSavings, memSavings := ReplicaReductionSavingsMicroCents(&rec, 1000, 500)

	assert.Equal(t, int64(0), cpuSavings)
	assert.Equal(t, int64(0), memSavings)
}

func TestReplicaReductionSavingsMicroCents_ZeroRecommended(t *testing.T) {
	rec := ContainerRec{
		RecCPURequestMC:     200,
		RecMemRequestKiB:    1024,
		DesiredReplicas:     3,
		PodCountAvg:         3,
		RecommendedReplicas: 0, // not set
	}

	cpuSavings, memSavings := ReplicaReductionSavingsMicroCents(&rec, 1000, 500)

	assert.Equal(t, int64(0), cpuSavings)
	assert.Equal(t, int64(0), memSavings)
}

// --- Phase 2: CV-based confidence tests ---

func TestComputeReplicaConfidence_Phase2_HighCV(t *testing.T) {
	rec := ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	cv := int64(4000) // > 3000 → low
	digest := DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 90,
		CPUUsageCVBP:  &cv,
	}

	assert.Equal(t, "low", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_Phase2_MediumCV(t *testing.T) {
	rec := ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	cv := int64(2000) // 1500 ≤ 2000 < 3000 → medium
	digest := DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 90,
		CPUUsageCVBP:  &cv,
	}

	assert.Equal(t, "medium", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_Phase2_LowCV(t *testing.T) {
	rec := ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	cv := int64(800) // < 1500 → high
	digest := DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 90,
		CPUUsageCVBP:  &cv,
	}

	assert.Equal(t, "high", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_Phase2_Fallback(t *testing.T) {
	// CPUUsageCVBP = nil → uses Phase 1 P50/P95 heuristic
	rec := ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	digest := DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 90, // spread = 0.10 < 0.25 → high (Phase 1)
		CPUUsageCVBP:  nil,
	}

	assert.Equal(t, "high", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_Phase2_DeploymentIgnoresCV(t *testing.T) {
	rec := ContainerRec{
		WorkloadType: "deployment",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	cv := int64(9000) // very high CV, but Deployment → still "high"
	digest := DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 50,
		CPUUsageCVBP:  &cv,
	}

	assert.Equal(t, "high", computeReplicaConfidence(&rec, digest))
}

func TestNullIfZeroInt64(t *testing.T) {
	assert.Nil(t, nullIfZeroInt64(0))
	v := nullIfZeroInt64(42)
	assert.NotNil(t, v)
	assert.Equal(t, int64(42), *v)
}
