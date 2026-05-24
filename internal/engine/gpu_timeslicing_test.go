package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- TS-03: Notification constant ---

func TestNotifGPUTimeSharingCandidate_Exists(t *testing.T) {
	assert.Equal(t, int16(36), NotifGPUTimeSharingCandidate)
}

// --- TS-04: Result types ---

func TestTimeslicingRec_ZeroValue(t *testing.T) {
	var rec TimeslicingRec
	assert.Equal(t, "", rec.NodeName)
	assert.Equal(t, 0, rec.RecommendedReplicas)
	assert.Nil(t, rec.SavingsPerGPU)
	assert.Empty(t, rec.CandidateContainers)
	assert.Empty(t, rec.ImpactedContainers)
}

// --- TS-05: computeReplicas ---

func TestComputeReplicas(t *testing.T) {
	tests := []struct {
		name     string
		smAvg    float32
		dramAvg  float32
		fbFrac   float32
		wantReps int
		wantOK   bool
	}{
		{"very_low_util", 0.05, 0.03, 0.02, 8, true},
		{"moderate_util", 0.20, 0.15, 0.10, 5, true},
		{"higher_util", 0.40, 0.30, 0.20, 2, true},
		{"too_high_util", 0.55, 0.30, 0.20, 0, false},
		{"dram_dominates", 0.10, 0.40, 0.10, 2, true},
		{"fb_dominates", 0.10, 0.10, 0.60, 0, false},
		{"exact_50pct", 0.50, 0.20, 0.10, 2, true},
		{"all_zero", 0.00, 0.00, 0.00, 8, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reps, ok := computeReplicas(tt.smAvg, tt.dramAvg, tt.fbFrac, defaultGPUThresholdSettings)
			assert.Equal(t, tt.wantOK, ok, "ok mismatch")
			if ok {
				assert.Equal(t, tt.wantReps, reps, "replicas mismatch")
			}
		})
	}
}

// --- TS-06: computeTimeslicingConfidence ---

func TestComputeTimeslicingConfidence(t *testing.T) {
	tests := []struct {
		name        string
		avgCandConf float32
		nImpacted   int
		nTotal      int
		wantConf    float32
	}{
		{"no_impacted", 0.8, 0, 4, 0.56},
		{"one_impacted_of4", 0.8, 1, 4, 0.518},
		{"half_impacted", 0.8, 2, 4, 0.476},
		{"all_impacted", 0.8, 4, 4, 0.392},
		{"low_base_conf", 0.3, 0, 2, 0.21},
		{"zero_total", 0.8, 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeTimeslicingConfidence(tt.avgCandConf, tt.nImpacted, tt.nTotal, defaultGPUThresholdSettings)
			assert.InDelta(t, tt.wantConf, got, 0.01)
		})
	}
}

// --- TS-07: computeTimeslicingSavings ---

func TestComputeTimeslicingSavings(t *testing.T) {
	rate := float32(300.0)
	t.Run("4_replicas_3_candidates", func(t *testing.T) {
		perGPU, total := computeTimeslicingSavings(4, 3, &rate)
		require.NotNil(t, perGPU)
		require.NotNil(t, total)
		assert.InDelta(t, 225.0, *perGPU, 0.01)
		assert.InDelta(t, 675.0, *total, 0.01)
	})
	t.Run("no_cost_data", func(t *testing.T) {
		perGPU, total := computeTimeslicingSavings(4, 3, nil)
		assert.Nil(t, perGPU)
		assert.Nil(t, total)
	})
	t.Run("2_replicas_1_candidate", func(t *testing.T) {
		perGPU, total := computeTimeslicingSavings(2, 1, &rate)
		require.NotNil(t, perGPU)
		assert.InDelta(t, 150.0, *perGPU, 0.01)
		assert.InDelta(t, 150.0, *total, 0.01)
	})
}

// --- TS-08: partitionContainers ---

func TestPartitionContainers(t *testing.T) {
	t.Run("mixed_node", func(t *testing.T) {
		containers := []NodeGPUContainer{
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.12}},
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.08}},
			{Rec: &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.67}},
		}
		candidates, impacted := partitionContainers(containers)
		assert.Len(t, candidates, 2)
		assert.Len(t, impacted, 1)
	})

	t.Run("idle_excluded", func(t *testing.T) {
		containers := []NodeGPUContainer{
			{Rec: &GPURec{Classification: GPUClassIdle, SMActiveAvg: 0.01}},
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.12}},
		}
		candidates, impacted := partitionContainers(containers)
		assert.Len(t, candidates, 1)
		assert.Len(t, impacted, 0)
	})

	t.Run("memory_bound_excluded", func(t *testing.T) {
		containers := []NodeGPUContainer{
			{Rec: &GPURec{Classification: GPUClassMemoryBound, SMActiveAvg: 0.30}},
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.12}},
		}
		candidates, impacted := partitionContainers(containers)
		assert.Len(t, candidates, 1)
		assert.Len(t, impacted, 0)
	})

	t.Run("mig_rec_takes_precedence", func(t *testing.T) {
		containers := []NodeGPUContainer{
			{Rec: &GPURec{Classification: GPUClassUnderutilized, RecommendedGPUProfile: "3g.40gb", SMActiveAvg: 0.12}},
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10}},
		}
		candidates, impacted := partitionContainers(containers)
		assert.Len(t, candidates, 1)
		assert.Len(t, impacted, 0)
	})

	t.Run("compute_bound_underutil_is_candidate", func(t *testing.T) {
		containers := []NodeGPUContainer{
			{Rec: &GPURec{Classification: GPUClassComputeBoundUnderutil, SMActiveAvg: 0.20}},
		}
		candidates, impacted := partitionContainers(containers)
		assert.Len(t, candidates, 1)
		assert.Len(t, impacted, 0)
	})

	t.Run("nil_rec_skipped", func(t *testing.T) {
		containers := []NodeGPUContainer{
			{Rec: nil},
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10}},
		}
		candidates, _ := partitionContainers(containers)
		assert.Len(t, candidates, 1)
	})

	t.Run("no_profiling_treated_as_impacted", func(t *testing.T) {
		containers := []NodeGPUContainer{
			{Rec: &GPURec{Classification: "", SMActiveAvg: 0.0}},
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.12}},
		}
		candidates, impacted := partitionContainers(containers)
		assert.Len(t, candidates, 1, "underutilized container should be candidate")
		assert.Len(t, impacted, 1, "no-profiling container should be impacted (default)")
	})
}

// --- TS-08b: avgCandidateUtilization ---

func TestAvgCandidateUtilization(t *testing.T) {
	candidates := []NodeGPUContainer{
		{Rec: &GPURec{SMActiveAvg: 0.12, DRAMActiveAvg: 0.08, FBUsageMaxMiB: 4096}},
		{Rec: &GPURec{SMActiveAvg: 0.18, DRAMActiveAvg: 0.10, FBUsageMaxMiB: 8192}},
	}
	totalFBMiB := float32(16384)

	avgSM, avgDRAM, avgFBFrac := avgCandidateUtilization(candidates, totalFBMiB)
	assert.InDelta(t, 0.15, avgSM, 0.01)
	assert.InDelta(t, 0.09, avgDRAM, 0.01)
	assert.InDelta(t, 0.375, avgFBFrac, 0.01)
}

func TestAvgCandidateUtilization_ZeroTotalFB(t *testing.T) {
	candidates := []NodeGPUContainer{
		{Rec: &GPURec{SMActiveAvg: 0.12, DRAMActiveAvg: 0.08, FBUsageMaxMiB: 4096}},
	}
	avgSM, avgDRAM, avgFBFrac := avgCandidateUtilization(candidates, 0)
	assert.InDelta(t, 0.12, avgSM, 0.01)
	assert.InDelta(t, 0.08, avgDRAM, 0.01)
	assert.InDelta(t, 0.0, avgFBFrac, 0.01)
}

func TestAvgCandidateUtilization_Empty(t *testing.T) {
	avgSM, avgDRAM, avgFBFrac := avgCandidateUtilization(nil, 16384)
	assert.Equal(t, float32(0), avgSM)
	assert.Equal(t, float32(0), avgDRAM)
	assert.Equal(t, float32(0), avgFBFrac)
}

// --- TS-08c: isNodeFresh ---

func TestIsNodeFresh(t *testing.T) {
	now := time.Now().UTC()
	t.Run("seen_today", func(t *testing.T) {
		assert.True(t, isNodeFresh(now.AddDate(0, 0, -1), now, NodeGPUFreshnessDays))
	})
	t.Run("seen_6_days_ago", func(t *testing.T) {
		assert.True(t, isNodeFresh(now.AddDate(0, 0, -6), now, NodeGPUFreshnessDays))
	})
	t.Run("seen_7_days_ago", func(t *testing.T) {
		assert.True(t, isNodeFresh(now.AddDate(0, 0, -7), now, NodeGPUFreshnessDays))
	})
	t.Run("seen_8_days_ago", func(t *testing.T) {
		assert.False(t, isNodeFresh(now.AddDate(0, 0, -8), now, NodeGPUFreshnessDays))
	})
	t.Run("seen_30_days_ago", func(t *testing.T) {
		assert.False(t, isNodeFresh(now.AddDate(0, 0, -30), now, NodeGPUFreshnessDays))
	})
}

// --- TS-09: ComputeNodeTimeslicingRec — happy path ---

func TestComputeNodeTimeslicingRec_HappyPath(t *testing.T) {
	gpuRate := float32(300.0)
	input := NodeGPUGroup{
		NodeName: "gpu-t4-worker-1", ClusterUUID: "cluster-1", GPUModel: "T4",
		Containers: []NodeGPUContainer{
			{Namespace: "ns", Workload: "wl", Container: "c1",
				Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.12, DRAMActiveAvg: 0.05, FBUsageMaxMiB: 2000, Confidence: 0.8}},
			{Namespace: "ns", Workload: "wl", Container: "c2",
				Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.08, DRAMActiveAvg: 0.04, FBUsageMaxMiB: 1500, Confidence: 0.8}},
			{Namespace: "ns", Workload: "wl", Container: "c3",
				Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.18, DRAMActiveAvg: 0.06, FBUsageMaxMiB: 3000, Confidence: 0.8}},
			{Namespace: "ns", Workload: "wl", Container: "c4",
				Rec: &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.67, DRAMActiveAvg: 0.30, FBUsageMaxMiB: 10000, Confidence: 0.8}},
		},
	}

	rec := ComputeNodeTimeslicingRec(input, &gpuRate, time.Now().UTC())
	require.NotNil(t, rec)
	assert.Equal(t, "gpu-t4-worker-1", rec.NodeName)
	assert.Equal(t, "T4", rec.GPUModel)
	assert.GreaterOrEqual(t, rec.RecommendedReplicas, 2)
	assert.LessOrEqual(t, rec.RecommendedReplicas, 8)
	assert.Len(t, rec.CandidateContainers, 3)
	assert.Len(t, rec.ImpactedContainers, 1)
	assert.Contains(t, rec.NotificationCodes, NotifGPUTimeSharingCandidate)
	require.NotNil(t, rec.SavingsPerGPU)
	assert.Greater(t, *rec.SavingsPerGPU, float32(0))
	assert.Greater(t, rec.Confidence, float32(0))
	assert.Less(t, rec.Confidence, float32(1.0))
}

// --- TS-10: Skip: majority not met ---

func TestComputeNodeTimeslicingRec_SkipBelowMajority(t *testing.T) {
	input := NodeGPUGroup{
		NodeName: "node-1", ClusterUUID: "c1", GPUModel: "T4",
		Containers: []NodeGPUContainer{
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.12, Confidence: 0.8}},
			{Rec: &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.67, Confidence: 0.8}},
			{Rec: &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.55, Confidence: 0.8}},
			{Rec: &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.72, Confidence: 0.8}},
		},
	}
	rec := ComputeNodeTimeslicingRec(input, nil, time.Now().UTC())
	assert.Nil(t, rec)
}

// --- TS-11: Skip: all idle ---

func TestComputeNodeTimeslicingRec_SkipAllIdle(t *testing.T) {
	input := NodeGPUGroup{
		NodeName: "node-idle", ClusterUUID: "c1", GPUModel: "T4",
		Containers: []NodeGPUContainer{
			{Rec: &GPURec{Classification: GPUClassIdle, SMActiveAvg: 0.01, Confidence: 0.8}},
			{Rec: &GPURec{Classification: GPUClassIdle, SMActiveAvg: 0.005, Confidence: 0.8}},
		},
	}
	rec := ComputeNodeTimeslicingRec(input, nil, time.Now().UTC())
	assert.Nil(t, rec)
}

// --- TS-12: Skip: all MIG ---

func TestComputeNodeTimeslicingRec_SkipAllMIG(t *testing.T) {
	input := NodeGPUGroup{
		NodeName: "node-mig", ClusterUUID: "c1", GPUModel: "A100",
		Containers: []NodeGPUContainer{
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10,
				RecommendedGPUProfile: "3g.40gb", Confidence: 0.8}},
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.15,
				RecommendedGPUProfile: "1g.10gb", Confidence: 0.8}},
		},
	}
	rec := ComputeNodeTimeslicingRec(input, nil, time.Now().UTC())
	assert.Nil(t, rec)
}

// --- TS-13: All underutilized, no impacted ---

func TestComputeNodeTimeslicingRec_AllUnderutilized(t *testing.T) {
	rate := float32(300.0)
	input := NodeGPUGroup{
		NodeName: "node-all-under", ClusterUUID: "c1", GPUModel: "T4",
		Containers: []NodeGPUContainer{
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.20, DRAMActiveAvg: 0.10, FBUsageMaxMiB: 3000, Confidence: 0.8}},
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.20, DRAMActiveAvg: 0.10, FBUsageMaxMiB: 3000, Confidence: 0.8}},
		},
	}
	rec := ComputeNodeTimeslicingRec(input, &rate, time.Now().UTC())
	require.NotNil(t, rec)
	assert.Empty(t, rec.ImpactedContainers)
	assert.InDelta(t, 0.8*0.7, rec.Confidence, 0.01)
}

// --- TS-14: Multi-model grouping ---

func TestComputeNodeTimeslicingRecs_MultipleGPUModels(t *testing.T) {
	rate := float32(300.0)
	groups := []NodeGPUGroup{
		{NodeName: "mixed-node", ClusterUUID: "c1", GPUModel: "T4",
			Containers: []NodeGPUContainer{
				{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.15, DRAMActiveAvg: 0.05, Confidence: 0.8}},
				{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10, DRAMActiveAvg: 0.05, Confidence: 0.8}},
			}},
		{NodeName: "mixed-node", ClusterUUID: "c1", GPUModel: "L4",
			Containers: []NodeGPUContainer{
				{Rec: &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.60, DRAMActiveAvg: 0.30, Confidence: 0.8}},
			}},
	}

	var results []*TimeslicingRec
	for _, g := range groups {
		if r := ComputeNodeTimeslicingRec(g, &rate, time.Now().UTC()); r != nil {
			results = append(results, r)
		}
	}
	assert.Len(t, results, 1)
	assert.Equal(t, "T4", results[0].GPUModel)
}

// --- TS-15: Notif 29 on candidate GPURecs ---

func TestComputeNodeTimeslicingRec_SetsNotifOnCandidates(t *testing.T) {
	rec1 := &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.12, DRAMActiveAvg: 0.05, Confidence: 0.8}
	rec2 := &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10, DRAMActiveAvg: 0.04, Confidence: 0.8}
	impactedRec := &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.55, DRAMActiveAvg: 0.20, Confidence: 0.8}

	input := NodeGPUGroup{
		NodeName: "node-1", ClusterUUID: "c1", GPUModel: "T4",
		Containers: []NodeGPUContainer{
			{Namespace: "ns", Workload: "wl", Container: "c1", Rec: rec1},
			{Namespace: "ns", Workload: "wl", Container: "c2", Rec: rec2},
			{Namespace: "ns", Workload: "wl", Container: "c3", Rec: impactedRec},
		},
	}
	result := ComputeNodeTimeslicingRec(input, nil, time.Now().UTC())
	require.NotNil(t, result)

	assert.Contains(t, rec1.NotificationCodes, NotifGPUTimeSharingCandidate)
	assert.Contains(t, rec2.NotificationCodes, NotifGPUTimeSharingCandidate)
	assert.NotContains(t, impactedRec.NotificationCodes, NotifGPUTimeSharingCandidate)
}

// --- E-T17: Container cross-reference fields on candidate GPURecs ---

func TestComputeNodeTimeslicingRec_SetsContainerCrossRef(t *testing.T) {
	rec1 := &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.12, DRAMActiveAvg: 0.05, Confidence: 0.8}
	rec2 := &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10, DRAMActiveAvg: 0.04, Confidence: 0.8}
	impactedRec := &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.55, DRAMActiveAvg: 0.20, Confidence: 0.8}

	input := NodeGPUGroup{
		NodeName: "gpu-worker-7", ClusterUUID: "c1", GPUModel: "T4",
		Containers: []NodeGPUContainer{
			{Namespace: "ns", Workload: "wl", Container: "c1", Rec: rec1},
			{Namespace: "ns", Workload: "wl", Container: "c2", Rec: rec2},
			{Namespace: "ns", Workload: "wl", Container: "c3", Rec: impactedRec},
		},
	}
	result := ComputeNodeTimeslicingRec(input, nil, time.Now().UTC())
	require.NotNil(t, result)

	assert.Equal(t, "gpu-worker-7", rec1.TimeSlicingNode, "candidate should have node name")
	assert.Equal(t, result.RecommendedReplicas, rec1.TimeSlicingReplicas, "candidate should have replicas")

	assert.Equal(t, "gpu-worker-7", rec2.TimeSlicingNode)
	assert.Equal(t, result.RecommendedReplicas, rec2.TimeSlicingReplicas)

	assert.Equal(t, "", impactedRec.TimeSlicingNode, "impacted container should NOT have cross-ref")
	assert.Equal(t, 0, impactedRec.TimeSlicingReplicas)
}

func TestComputeNodeTimeslicingRec_NoCandidatesNoCrossRef(t *testing.T) {
	rec := &GPURec{Classification: GPUClassIdle, SMActiveAvg: 0.01, Confidence: 0.8}
	input := NodeGPUGroup{
		NodeName: "node-idle", ClusterUUID: "c1", GPUModel: "T4",
		Containers: []NodeGPUContainer{
			{Namespace: "ns", Workload: "wl", Container: "c1", Rec: rec},
		},
	}
	result := ComputeNodeTimeslicingRec(input, nil, time.Now().UTC())
	assert.Nil(t, result)
	assert.Equal(t, "", rec.TimeSlicingNode, "no recommendation means no cross-ref")
	assert.Equal(t, 0, rec.TimeSlicingReplicas)
}

// --- E-T17b: ComputeNodeTimeslicingRec side effects with nil rate ---

func TestComputeNodeTimeslicingRec_NilRateStillAnnotatesCandidates(t *testing.T) {
	rec1 := &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.12, DRAMActiveAvg: 0.05, Confidence: 0.8}
	rec2 := &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.55, DRAMActiveAvg: 0.20, Confidence: 0.8}

	group := NodeGPUGroup{
		NodeName: "node-ann", ClusterUUID: "c1", GPUModel: "T4",
		Containers: []NodeGPUContainer{
			{Namespace: "ns", Workload: "wl", Container: "c1", Rec: rec1},
			{Namespace: "ns", Workload: "wl", Container: "c2", Rec: rec2},
		},
	}
	ComputeNodeTimeslicingRec(group, nil, time.Now().UTC())

	assert.Equal(t, "node-ann", rec1.TimeSlicingNode)
	assert.Greater(t, rec1.TimeSlicingReplicas, 0)
	assert.Contains(t, rec1.NotificationCodes, NotifGPUTimeSharingCandidate)
	assert.Nil(t, rec1.EstimatedTimeslicingSavingsUSD, "nil rate means no savings")

	assert.Equal(t, "", rec2.TimeSlicingNode, "impacted container should not be annotated")
	assert.Equal(t, 0, rec2.TimeSlicingReplicas)
}

// --- TS-16: GPUDigestRow.NodeName ---

func TestGPUDigestRow_HasNodeName(t *testing.T) {
	row := GPUDigestRow{NodeName: "gpu-worker-1"}
	assert.Equal(t, "gpu-worker-1", row.NodeName)
}

// --- HasMIGRecommendation helper ---

func TestGPURec_HasMIGRecommendation(t *testing.T) {
	t.Run("empty profile", func(t *testing.T) {
		assert.False(t, (&GPURec{}).HasMIGRecommendation())
	})
	t.Run("full_gpu", func(t *testing.T) {
		assert.False(t, (&GPURec{RecommendedGPUProfile: "full_gpu"}).HasMIGRecommendation())
	})
	t.Run("mig profile", func(t *testing.T) {
		assert.True(t, (&GPURec{RecommendedGPUProfile: "3g.40gb"}).HasMIGRecommendation())
	})
}

// --- Edge cases ---

func TestComputeNodeTimeslicingRec_EmptyGroup(t *testing.T) {
	rec := ComputeNodeTimeslicingRec(NodeGPUGroup{}, nil, time.Now().UTC())
	assert.Nil(t, rec)
}

func TestComputeNodeTimeslicingRec_StaleNode(t *testing.T) {
	staleDate := time.Now().UTC().AddDate(0, 0, -10)
	input := NodeGPUGroup{
		NodeName: "stale-node", ClusterUUID: "c1", GPUModel: "T4",
		LastSeen: staleDate,
		Containers: []NodeGPUContainer{
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10, DRAMActiveAvg: 0.05, Confidence: 0.8}},
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.15, DRAMActiveAvg: 0.06, Confidence: 0.8}},
		},
	}
	rec := ComputeNodeTimeslicingRec(input, nil, time.Now().UTC())
	assert.Nil(t, rec, "stale node (>7 days) should produce no recommendation")
}

func TestComputeNodeTimeslicingRec_FreshNode(t *testing.T) {
	freshDate := time.Now().UTC().AddDate(0, 0, -3)
	input := NodeGPUGroup{
		NodeName: "fresh-node", ClusterUUID: "c1", GPUModel: "T4",
		LastSeen: freshDate,
		Containers: []NodeGPUContainer{
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10, DRAMActiveAvg: 0.05, Confidence: 0.8}},
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.15, DRAMActiveAvg: 0.06, Confidence: 0.8}},
		},
	}
	rec := ComputeNodeTimeslicingRec(input, nil, time.Now().UTC())
	require.NotNil(t, rec, "fresh node (3 days ago) should produce a recommendation")
	assert.Equal(t, "fresh-node", rec.NodeName)
	assert.Equal(t, "T4", rec.GPUModel)
	assert.GreaterOrEqual(t, rec.RecommendedReplicas, defaultGPUThresholdSettings.TimeslicingMinReplicas)
	assert.LessOrEqual(t, rec.RecommendedReplicas, defaultGPUThresholdSettings.TimeslicingMaxReplicas)
	assert.Greater(t, rec.Confidence, float32(0))
	assert.Len(t, rec.CandidateContainers, 2)
}

func TestComputeNodeTimeslicingRec_ZeroLastSeen(t *testing.T) {
	input := NodeGPUGroup{
		NodeName: "no-timestamp", ClusterUUID: "c1", GPUModel: "T4",
		Containers: []NodeGPUContainer{
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10, DRAMActiveAvg: 0.05, Confidence: 0.8}},
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.15, DRAMActiveAvg: 0.06, Confidence: 0.8}},
		},
	}
	rec := ComputeNodeTimeslicingRec(input, nil, time.Now().UTC())
	require.NotNil(t, rec, "zero LastSeen should be treated as fresh (backward compat)")
	assert.Equal(t, "no-timestamp", rec.NodeName)
	assert.GreaterOrEqual(t, rec.RecommendedReplicas, defaultGPUThresholdSettings.TimeslicingMinReplicas)
}

// --- Container-level time-slicing savings ---

func TestComputeNodeTimeslicingRec_SetsTimeslicingSavingsOnCandidates(t *testing.T) {
	gpuRate := float32(300.0)
	rec1 := &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.12, DRAMActiveAvg: 0.05, Confidence: 0.8}
	rec2 := &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10, DRAMActiveAvg: 0.04, Confidence: 0.8}
	impactedRec := &GPURec{Classification: GPUClassWellUtilized, SMActiveAvg: 0.55, DRAMActiveAvg: 0.20, Confidence: 0.8}

	input := NodeGPUGroup{
		NodeName: "node-1", ClusterUUID: "c1", GPUModel: "T4",
		Containers: []NodeGPUContainer{
			{Namespace: "ns", Workload: "wl", Container: "c1", Rec: rec1},
			{Namespace: "ns", Workload: "wl", Container: "c2", Rec: rec2},
			{Namespace: "ns", Workload: "wl", Container: "c3", Rec: impactedRec},
		},
	}
	result := ComputeNodeTimeslicingRec(input, &gpuRate, time.Now().UTC())
	require.NotNil(t, result)
	require.NotNil(t, result.SavingsPerGPU)

	require.NotNil(t, rec1.EstimatedTimeslicingSavingsUSD, "candidate should have time-slicing savings")
	assert.Equal(t, *result.SavingsPerGPU, *rec1.EstimatedTimeslicingSavingsUSD)

	require.NotNil(t, rec2.EstimatedTimeslicingSavingsUSD, "candidate should have time-slicing savings")
	assert.Equal(t, *result.SavingsPerGPU, *rec2.EstimatedTimeslicingSavingsUSD)

	assert.Nil(t, impactedRec.EstimatedTimeslicingSavingsUSD, "impacted container should NOT have time-slicing savings")
}

func TestComputeNodeTimeslicingRec_NoRateNoTimeslicingSavings(t *testing.T) {
	rec1 := &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.12, DRAMActiveAvg: 0.05, Confidence: 0.8}
	rec2 := &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10, DRAMActiveAvg: 0.04, Confidence: 0.8}

	input := NodeGPUGroup{
		NodeName: "node-1", ClusterUUID: "c1", GPUModel: "T4",
		Containers: []NodeGPUContainer{
			{Namespace: "ns", Workload: "wl", Container: "c1", Rec: rec1},
			{Namespace: "ns", Workload: "wl", Container: "c2", Rec: rec2},
		},
	}
	result := ComputeNodeTimeslicingRec(input, nil, time.Now().UTC())
	require.NotNil(t, result)

	assert.Nil(t, rec1.EstimatedTimeslicingSavingsUSD, "no rate means no time-slicing savings")
	assert.Nil(t, rec2.EstimatedTimeslicingSavingsUSD, "no rate means no time-slicing savings")
}

func TestComputeNodeTimeslicingRec_NoRate(t *testing.T) {
	input := NodeGPUGroup{
		NodeName: "node-no-rate", ClusterUUID: "c1", GPUModel: "T4",
		Containers: []NodeGPUContainer{
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.10, DRAMActiveAvg: 0.05, Confidence: 0.8}},
			{Rec: &GPURec{Classification: GPUClassUnderutilized, SMActiveAvg: 0.15, DRAMActiveAvg: 0.06, Confidence: 0.8}},
		},
	}
	rec := ComputeNodeTimeslicingRec(input, nil, time.Now().UTC())
	require.NotNil(t, rec)
	assert.Nil(t, rec.SavingsPerGPU)
	assert.Nil(t, rec.TotalNodeSavings)
}
