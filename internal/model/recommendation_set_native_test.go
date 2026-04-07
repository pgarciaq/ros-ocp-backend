package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
