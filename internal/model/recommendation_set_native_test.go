package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssembleNativeResults_IncludesNotifications(t *testing.T) {
	cpuReq := int64(200)
	memReq := int64(524288)
	conf := float32(0.4)
	now := time.Now()

	rows := []NativeRecommendationRow{
		{
			OrgID: "org1", ClusterUUID: "c1", Namespace: "ns", Workload: "deploy",
			WorkloadType: "deployment", ContainerName: "main",
			Term: "short", Engine: "cost",
			RecCPURequestMC: &cpuReq, RecMemRequestKiB: &memReq,
			ConfidenceLevel:   &conf,
			NotificationCodes: SmallintArray{1, 3},
			UpdatedAt:         now,
			SourceID:          "src-1", ClusterAlias: "my-cluster",
			LastReported: now,
		},
	}

	results := assembleNativeResults(rows)
	require.Len(t, results, 1)

	term, ok := results[0].Recommendations["short_term"]
	require.True(t, ok)
	require.NotNil(t, term.Cost)

	assert.NotNil(t, term.Cost.Notifications, "notifications should be populated")
	assert.Len(t, term.Cost.Notifications, 2)
	assert.Equal(t, "WARNING", term.Cost.Notifications["1"].Type)
	assert.Equal(t, "CRITICAL", term.Cost.Notifications["3"].Type)
}

func TestAssembleNativeResults_NoNotificationsWhenEmpty(t *testing.T) {
	cpuReq := int64(200)
	memReq := int64(524288)
	conf := float32(0.9)
	now := time.Now()

	rows := []NativeRecommendationRow{
		{
			OrgID: "org1", ClusterUUID: "c1", Namespace: "ns", Workload: "deploy",
			WorkloadType: "deployment", ContainerName: "main",
			Term: "short", Engine: "cost",
			RecCPURequestMC: &cpuReq, RecMemRequestKiB: &memReq,
			ConfidenceLevel:   &conf,
			NotificationCodes: nil,
			UpdatedAt:         now,
			SourceID:          "src-1", ClusterAlias: "my-cluster",
			LastReported: now,
		},
	}

	results := assembleNativeResults(rows)
	require.Len(t, results, 1)

	term := results[0].Recommendations["short_term"]
	require.NotNil(t, term.Cost)
	assert.Nil(t, term.Cost.Notifications, "no notifications when codes are empty")
}

func TestNativeContainerID_Deterministic(t *testing.T) {
	id1 := NativeContainerID("c-uuid", "ns", "deploy", "main")
	id2 := NativeContainerID("c-uuid", "ns", "deploy", "main")
	assert.Equal(t, id1, id2, "same input should produce same UUID")

	id3 := NativeContainerID("c-uuid", "ns", "deploy", "sidecar")
	assert.NotEqual(t, id1, id3, "different container name should produce different UUID")
}
