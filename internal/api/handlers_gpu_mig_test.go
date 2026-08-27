package api

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func TestGpuMIGRowsToEntries(t *testing.T) {
	totalFB := int64(40960)
	rows := []model.GPUMIGRecommendationSetRow{
		{
			ClusterUUID:           "c1",
			Namespace:             "ns1",
			Workload:              "deploy1",
			WorkloadType:          "deployment",
			Container:             "ctr1",
			GPUModel:              "A100",
			Term:                  "short",
			RecommendedGPUProfile: "1g.5gb",
			CurrentGPUProfile:     "full_gpu",
			Classification:        "memory_bound",
			Confidence:            0.85,
			ConfidenceLevel:       0.85,
			FBUsageMaxMiB:         4096,
			TotalFBMiB:            &totalFB,
			GPUIdleState:          "active",
			NodeName:              "node-1",
		},
	}

	entries := gpuMIGRowsToEntries(rows)
	assert.Len(t, entries, 1)

	e := entries[0]
	assert.Equal(t, model.NativeContainerID("c1", "ns1", "deploy1", "deployment", "ctr1"), e.ID)
	assert.Equal(t, "c1", e.ClusterUUID)
	assert.Equal(t, "ns1", e.Namespace)
	assert.Equal(t, "deploy1", e.Workload)
	assert.Equal(t, "deployment", e.WorkloadType)
	assert.Equal(t, "ctr1", e.Container)
	assert.Equal(t, "A100", e.GPUModel)
	assert.Equal(t, "short", e.Term)
	assert.Equal(t, "1g.5gb", e.RecommendedGPUProfile)
	assert.Equal(t, "full_gpu", e.CurrentGPUProfile)
	assert.Equal(t, "memory_bound", e.Classification)
	assert.Equal(t, float32(0.85), e.Confidence)
	assert.Equal(t, float32(0.85), e.ConfidenceLevel)
	assert.Equal(t, float32(4096), e.FBUsageMaxMiB)
	assert.NotNil(t, e.TotalFBMiB)
	assert.Equal(t, int64(40960), *e.TotalFBMiB)
	assert.Equal(t, "active", e.GPUIdleState)
	assert.Equal(t, "node-1", e.NodeName)
}

func TestGpuMIGRowsToEntries_Empty(t *testing.T) {
	entries := gpuMIGRowsToEntries(nil)
	assert.Empty(t, entries)
}

func TestGpuMIGRowsToEntries_SameContainerDifferentTermsShareID(t *testing.T) {
	rows := []model.GPUMIGRecommendationSetRow{
		{
			ClusterUUID:  "c1",
			Namespace:    "ns1",
			Workload:     "deploy1",
			WorkloadType: "deployment",
			Container:    "ctr1",
			Term:         "short",
			GPUModel:     "A100",
		},
		{
			ClusterUUID:  "c1",
			Namespace:    "ns1",
			Workload:     "deploy1",
			WorkloadType: "deployment",
			Container:    "ctr1",
			Term:         "medium",
			GPUModel:     "A100",
		},
	}

	entries := gpuMIGRowsToEntries(rows)
	assert.Len(t, entries, 2)
	want := model.NativeContainerID("c1", "ns1", "deploy1", "deployment", "ctr1")
	assert.Equal(t, want, entries[0].ID)
	assert.Equal(t, want, entries[1].ID)
	assert.NotEqual(t, entries[0].Term, entries[1].Term)
}

func TestGpuMIGSortValue(t *testing.T) {
	e := model.GPUMIGRecommendationEntry{
		ClusterUUID:  "c1",
		Namespace:    "ns1",
		Workload:     "wl1",
		Container:    "ctr1",
		GPUModel:     "A100",
		Term:         "short",
		Confidence:   0.9,
		GPUIdleState: "idle",
	}
	assert.Equal(t, "c1", gpuMIGSortValue(e, "cluster_uuid"))
	assert.Equal(t, "ns1", gpuMIGSortValue(e, "namespace"))
	assert.Equal(t, "wl1", gpuMIGSortValue(e, "workload"))
	assert.Equal(t, "ctr1", gpuMIGSortValue(e, "container"))
	assert.Equal(t, "A100", gpuMIGSortValue(e, "gpu_model"))
	assert.Equal(t, "short", gpuMIGSortValue(e, "term"))
	assert.Equal(t, float32(0.9), gpuMIGSortValue(e, "confidence"))
	assert.Equal(t, "idle", gpuMIGSortValue(e, "gpu_idle_state"))
	assert.Equal(t, "c1", gpuMIGSortValue(e, "unknown"))
}

func TestFilterGPUMIGEntriesByRBAC(t *testing.T) {
	entries := []model.GPUMIGRecommendationEntry{
		{NodeName: "node-1"},
		{NodeName: "node-2"},
		{NodeName: "node-3"},
	}

	t.Run("wildcard allows all", func(t *testing.T) {
		perms := map[string][]string{"*": {}}
		result := filterGPUMIGEntriesByRBAC(entries, perms)
		assert.Len(t, result, 3)
	})

	t.Run("no rbac config allows all regardless of perms", func(t *testing.T) {
		perms := map[string][]string{"openshift.node": {"node-1", "node-3"}}
		result := filterGPUMIGEntriesByRBAC(entries, perms)
		assert.Len(t, result, 3)
	})

	t.Run("no node perms allows all", func(t *testing.T) {
		perms := map[string][]string{}
		result := filterGPUMIGEntriesByRBAC(entries, perms)
		assert.Len(t, result, 3)
	})
}

func TestCompareSortValues(t *testing.T) {
	assert.Equal(t, -1, compareSortValues("a", "b"))
	assert.Equal(t, 0, compareSortValues("same", "same"))
	assert.Equal(t, 1, compareSortValues("z", "a"))
	assert.Equal(t, -1, compareSortValues(float32(1.0), float32(2.0)))
	assert.Equal(t, 0, compareSortValues(float32(5.0), float32(5.0)))
	assert.Equal(t, 1, compareSortValues(float32(9.0), float32(1.0)))
	assert.Equal(t, 0, compareSortValues(42, "ignored"))
}
