package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindAdoptedContainers_WithinTolerance_Adopted(t *testing.T) {
	results := []ContainerRec{
		{
			Namespace:            "prod",
			Workload:             "api",
			WorkloadType:         "Deployment",
			ContainerName:        "web",
			CurrentCPURequestMC:  102,  // within 5% of 100
			CurrentMemRequestKiB: 2100, // within 5% of 2048
		},
	}
	oldRecs := map[containerKey]OldRecommendation{
		{Namespace: "prod", Workload: "api", WorkloadType: "Deployment", ContainerName: "web"}: {
			RecCPURequestMC:  100,
			RecMemRequestKiB: 2048,
		},
	}

	adopted := FindAdoptedContainers(results, oldRecs)
	assert.Len(t, adopted, 1)
	assert.Equal(t, "web", adopted[0].ContainerName)
}

func TestFindAdoptedContainers_OutsideTolerance_NotAdopted(t *testing.T) {
	results := []ContainerRec{
		{
			Namespace:            "prod",
			Workload:             "api",
			WorkloadType:         "Deployment",
			ContainerName:        "web",
			CurrentCPURequestMC:  200, // 100% above 100
			CurrentMemRequestKiB: 2048,
		},
	}
	oldRecs := map[containerKey]OldRecommendation{
		{Namespace: "prod", Workload: "api", WorkloadType: "Deployment", ContainerName: "web"}: {
			RecCPURequestMC:  100,
			RecMemRequestKiB: 2048,
		},
	}

	adopted := FindAdoptedContainers(results, oldRecs)
	assert.Empty(t, adopted)
}

func TestFindAdoptedContainers_NoPriorRec_NotAdopted(t *testing.T) {
	results := []ContainerRec{
		{
			Namespace:            "prod",
			Workload:             "api",
			WorkloadType:         "Deployment",
			ContainerName:        "web",
			CurrentCPURequestMC:  100,
			CurrentMemRequestKiB: 2048,
		},
	}
	oldRecs := map[containerKey]OldRecommendation{}

	adopted := FindAdoptedContainers(results, oldRecs)
	assert.Empty(t, adopted)
}

func TestFindAdoptedContainers_DeduplicatesKeys(t *testing.T) {
	results := []ContainerRec{
		{Namespace: "ns", Workload: "w", WorkloadType: "D", ContainerName: "c", Term: "short", Engine: "cost", CurrentCPURequestMC: 100, CurrentMemRequestKiB: 2048},
		{Namespace: "ns", Workload: "w", WorkloadType: "D", ContainerName: "c", Term: "medium", Engine: "cost", CurrentCPURequestMC: 100, CurrentMemRequestKiB: 2048},
		{Namespace: "ns", Workload: "w", WorkloadType: "D", ContainerName: "c", Term: "long", Engine: "cost", CurrentCPURequestMC: 100, CurrentMemRequestKiB: 2048},
	}
	oldRecs := map[containerKey]OldRecommendation{
		{Namespace: "ns", Workload: "w", WorkloadType: "D", ContainerName: "c"}: {
			RecCPURequestMC:  100,
			RecMemRequestKiB: 2048,
		},
	}

	adopted := FindAdoptedContainers(results, oldRecs)
	assert.Len(t, adopted, 1, "same container across multiple terms should only be detected once")
}

func TestDetectAdoption_WithinFivePercent(t *testing.T) {
	assert.True(t, DetectAdoption(100, 2048, 100, 2048))
	assert.True(t, DetectAdoption(105, 2100, 100, 2048))  // 5% CPU, ~2.5% mem
	assert.False(t, DetectAdoption(200, 2048, 100, 2048)) // 100% CPU diff
	assert.True(t, DetectAdoption(0, 0, 0, 0))
}
