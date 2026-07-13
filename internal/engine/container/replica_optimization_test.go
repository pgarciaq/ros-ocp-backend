package container

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

func TestComputeReplicaConfidence_DeploymentAlwaysHigh(t *testing.T) {
	rec := core.ContainerRec{
		WorkloadType: "deployment",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	digest := core.DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 50,
	}

	assert.Equal(t, "high", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_StatefulSetLowSpread(t *testing.T) {
	rec := core.ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	digest := core.DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 90, // spread = (100-90)/100 = 0.10 < 0.25 → high
	}

	assert.Equal(t, "high", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_StatefulSetMediumSpread(t *testing.T) {
	rec := core.ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	digest := core.DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 70, // spread = 0.30, 0.25 ≤ 0.30 < 0.40 → medium
	}

	assert.Equal(t, "medium", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_StatefulSetHighSpread(t *testing.T) {
	rec := core.ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	digest := core.DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 50, // spread = 0.50 ≥ 0.40 → low
	}

	assert.Equal(t, "low", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_StatefulSetUnstablePodCount(t *testing.T) {
	rec := core.ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  2,
		PodCountMax:  5, // unstable
	}
	digest := core.DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 90,
	}

	assert.Equal(t, "medium", computeReplicaConfidence(&rec, digest))
}

// --- Phase 2: CV-based confidence tests ---

func TestComputeReplicaConfidence_Phase2_HighCV(t *testing.T) {
	rec := core.ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	cv := int64(4000) // > 3000 → low
	digest := core.DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 90,
		CPUUsageCVBP:  &cv,
	}

	assert.Equal(t, "low", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_Phase2_MediumCV(t *testing.T) {
	rec := core.ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	cv := int64(2000) // 1500 ≤ 2000 < 3000 → medium
	digest := core.DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 90,
		CPUUsageCVBP:  &cv,
	}

	assert.Equal(t, "medium", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_Phase2_LowCV(t *testing.T) {
	rec := core.ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	cv := int64(800) // < 1500 → high
	digest := core.DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 90,
		CPUUsageCVBP:  &cv,
	}

	assert.Equal(t, "high", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_Phase2_Fallback(t *testing.T) {
	// CPUUsageCVBP = nil → uses Phase 1 P50/P95 heuristic
	rec := core.ContainerRec{
		WorkloadType: "statefulset",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	digest := core.DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 90, // spread = 0.10 < 0.25 → high (Phase 1)
		CPUUsageCVBP:  nil,
	}

	assert.Equal(t, "high", computeReplicaConfidence(&rec, digest))
}

func TestComputeReplicaConfidence_Phase2_DeploymentIgnoresCV(t *testing.T) {
	rec := core.ContainerRec{
		WorkloadType: "deployment",
		PodCountMin:  3,
		PodCountMax:  3,
	}
	cv := int64(9000) // very high CV, but Deployment → still "high"
	digest := core.DigestRow{
		CPUUsageP95MC: 100,
		CPUUsageP50MC: 50,
		CPUUsageCVBP:  &cv,
	}

	assert.Equal(t, "high", computeReplicaConfidence(&rec, digest))
}
