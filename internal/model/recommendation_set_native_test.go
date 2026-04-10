package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr64(v int64) *int64      { return &v }
func ptrF32(v float32) *float32 { return &v }

func TestAssembleNativeResults_GroupsByContainer(t *testing.T) {
	now := time.Now().UTC()
	rows := []NativeRecommendationRow{
		{ClusterUUID: "c1", Namespace: "ns1", Workload: "deploy-a", ContainerName: "app",
			ClusterAlias: "cluster-1", WorkloadType: "deployment", SourceID: "s1", LastReported: now,
			Term: "short", Engine: "cost",
			RecCPURequestMC: ptr64(100), RecMemRequestKiB: ptr64(2048),
			ConfidenceLevel: ptrF32(0.85)},
		{ClusterUUID: "c1", Namespace: "ns1", Workload: "deploy-a", ContainerName: "app",
			ClusterAlias: "cluster-1", WorkloadType: "deployment", SourceID: "s1", LastReported: now,
			Term: "short", Engine: "performance",
			RecCPURequestMC: ptr64(200), RecMemRequestKiB: ptr64(4096),
			ConfidenceLevel: ptrF32(0.90)},
		{ClusterUUID: "c1", Namespace: "ns1", Workload: "deploy-a", ContainerName: "app",
			ClusterAlias: "cluster-1", WorkloadType: "deployment", SourceID: "s1", LastReported: now,
			Term: "medium", Engine: "cost",
			RecCPURequestMC: ptr64(150), RecMemRequestKiB: ptr64(3072)},
	}

	results := assembleNativeResults(rows)
	assert.Len(t, results, 1, "single container should produce one result")

	r := results[0]
	assert.Equal(t, "cluster-1", r.ClusterAlias)
	assert.Equal(t, "c1", r.ClusterUUID)
	assert.Equal(t, "app", r.Container)
	assert.Equal(t, "ns1", r.Project)
	assert.Equal(t, "deploy-a", r.Workload)

	shortTerm, ok := r.Recommendations["short_term"]
	assert.True(t, ok, "short_term should exist")
	assert.NotNil(t, shortTerm.Cost)
	assert.NotNil(t, shortTerm.Performance)
	assert.Equal(t, int64(100), *shortTerm.Cost.CPURequestMillicores)
	assert.Equal(t, int64(200), *shortTerm.Performance.CPURequestMillicores)

	mediumTerm, ok := r.Recommendations["medium_term"]
	assert.True(t, ok, "medium_term should exist")
	assert.NotNil(t, mediumTerm.Cost)
	assert.Nil(t, mediumTerm.Performance, "no performance row for medium term")
}

func TestAssembleNativeResults_MultipleContainers(t *testing.T) {
	now := time.Now().UTC()
	rows := []NativeRecommendationRow{
		{ClusterUUID: "c1", Namespace: "ns1", Workload: "deploy-a", ContainerName: "app",
			ClusterAlias: "cluster-1", WorkloadType: "deployment", SourceID: "s1", LastReported: now,
			Term: "short", Engine: "cost", RecCPURequestMC: ptr64(100)},
		{ClusterUUID: "c1", Namespace: "ns2", Workload: "deploy-b", ContainerName: "sidecar",
			ClusterAlias: "cluster-1", WorkloadType: "deployment", SourceID: "s1", LastReported: now,
			Term: "short", Engine: "cost", RecCPURequestMC: ptr64(50)},
	}

	results := assembleNativeResults(rows)
	assert.Len(t, results, 2, "two containers should produce two results")
	assert.Equal(t, "app", results[0].Container)
	assert.Equal(t, "sidecar", results[1].Container)
}

func TestAssembleNativeResults_EmptyInput(t *testing.T) {
	results := assembleNativeResults(nil)
	assert.Nil(t, results)
}

func TestAssembleNativeResults_AllSixRowsForOneContainer(t *testing.T) {
	now := time.Now().UTC()
	base := NativeRecommendationRow{
		ClusterUUID: "c1", Namespace: "ns1", Workload: "deploy-a", ContainerName: "app",
		ClusterAlias: "cluster-1", WorkloadType: "deployment", SourceID: "s1", LastReported: now,
	}

	var rows []NativeRecommendationRow
	terms := []string{"short", "medium", "long"}
	engines := []string{"cost", "performance"}
	for _, term := range terms {
		for _, eng := range engines {
			r := base
			r.Term = term
			r.Engine = eng
			r.RecCPURequestMC = ptr64(100)
			rows = append(rows, r)
		}
	}

	results := assembleNativeResults(rows)
	assert.Len(t, results, 1)

	recs := results[0].Recommendations
	for _, term := range terms {
		key := term + "_term"
		tr, ok := recs[key]
		assert.True(t, ok, "should have %s", key)
		assert.NotNil(t, tr.Cost, "%s should have cost", key)
		assert.NotNil(t, tr.Performance, "%s should have performance", key)
	}
}

func TestNativeContainerID_Deterministic(t *testing.T) {
	id1 := NativeContainerID("c1", "ns1", "deploy-a", "app")
	id2 := NativeContainerID("c1", "ns1", "deploy-a", "app")
	assert.Equal(t, id1, id2, "same inputs must produce the same UUID")

	// UUID format: 8-4-4-4-12 hex digits
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, id1)
}

func TestNativeContainerID_DifferentInputs(t *testing.T) {
	id1 := NativeContainerID("c1", "ns1", "deploy-a", "app")
	id2 := NativeContainerID("c1", "ns1", "deploy-a", "sidecar")
	id3 := NativeContainerID("c2", "ns1", "deploy-a", "app")
	assert.NotEqual(t, id1, id2, "different container names must produce different IDs")
	assert.NotEqual(t, id1, id3, "different cluster UUIDs must produce different IDs")
}

func TestAssembleNativeResults_SetsID(t *testing.T) {
	now := time.Now().UTC()
	rows := []NativeRecommendationRow{
		{ClusterUUID: "c1", Namespace: "ns1", Workload: "deploy-a", ContainerName: "app",
			ClusterAlias: "cluster-1", WorkloadType: "deployment", SourceID: "s1", LastReported: now,
			Term: "short", Engine: "cost", RecCPURequestMC: ptr64(100)},
	}

	results := assembleNativeResults(rows)
	require.Len(t, results, 1)

	expectedID := NativeContainerID("c1", "ns1", "deploy-a", "app")
	assert.Equal(t, expectedID, results[0].ID)
}

func TestAssembleNativeResults_IncludesEnrichedFields(t *testing.T) {
	now := time.Now().UTC()
	rows := []NativeRecommendationRow{
		{ClusterUUID: "c1", Namespace: "ns1", Workload: "deploy-a", ContainerName: "app",
			ClusterAlias: "cluster-1", WorkloadType: "deployment", SourceID: "s1", LastReported: now,
			Term: "short", Engine: "cost",
			RecCPURequestMC: ptr64(100), RecCPULimitMC: ptr64(200),
			RecMemRequestKiB: ptr64(2048), RecMemLimitKiB: ptr64(4096),
			CurrentCPURequestMC: ptr64(50), CurrentCPULimitMC: ptr64(150),
			CurrentMemRequestKiB: ptr64(1024), CurrentMemLimitKiB: ptr64(2048),
			VariationCPURequestPct: ptrF32(100.0), VariationMemRequestPct: ptrF32(100.0),
			ConfidenceLevel:   ptrF32(0.85),
			NotificationCodes: []int16{1001, 1002}},
	}

	results := assembleNativeResults(rows)
	require.Len(t, results, 1)

	shortTerm := results[0].Recommendations["short_term"]
	require.NotNil(t, shortTerm.Cost)

	eng := shortTerm.Cost
	assert.Equal(t, int64(100), *eng.CPURequestMillicores)
	assert.Equal(t, int64(200), *eng.CPULimitMillicores)
	assert.Equal(t, int64(2048), *eng.MemRequestKiB)
	assert.Equal(t, int64(4096), *eng.MemLimitKiB)

	assert.Equal(t, int64(50), *eng.CurrentCPURequestMC)
	assert.Equal(t, int64(150), *eng.CurrentCPULimitMC)
	assert.Equal(t, int64(1024), *eng.CurrentMemRequestKiB)
	assert.Equal(t, int64(2048), *eng.CurrentMemLimitKiB)

	assert.InDelta(t, 100.0, *eng.VariationCPURequestPct, 0.01)
	assert.InDelta(t, 100.0, *eng.VariationMemRequestPct, 0.01)
	assert.InDelta(t, 0.85, *eng.ConfidenceLevel, 0.01)
	assert.Equal(t, []int16{1001, 1002}, eng.NotificationCodes)
}
