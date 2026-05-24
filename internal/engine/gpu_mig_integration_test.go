package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestGPU_MIG_EndToEnd_Integration exercises the full MIG data flow:
// testcontainers DB → GPU digest seeding → QueryGPURecommendations →
// classification + MIG profile selection → verify output per workload type.
//
// Covers: idle, underutilized, memory-bound, compute-bound, well-utilized,
// MIG-partitioned (A100 1g.5gb), and multi-day windowed workloads.
func TestGPU_MIG_EndToEnd_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`,
		testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES (1, $1, 'gpu-mig-test', 'src-gpu', now()) ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(24 * time.Hour)
	start := now.AddDate(0, 0, -7)

	type gpuWorkload struct {
		namespace     string
		workload      string
		container     string
		gpuModel      string
		gpuProfile    string
		nodeName      string
		smAvg         float64
		tensorAvg     float64
		dramAvg       float64
		fbUsageAvgMiB float64
	}

	workloads := []gpuWorkload{
		{
			// Idle: avgSM(0.01) < IdleThreshold(0.02) → MIG downsizing applies
			namespace: "ml-idle", workload: "abandoned-notebook", container: "jupyter",
			gpuModel: "NVIDIA A100-SXM4-40GB", gpuProfile: "", nodeName: "gpu-node-1",
			smAvg: 0.01, tensorAvg: 0.005, dramAvg: 0.02, fbUsageAvgMiB: 512,
		},
		{
			// Underutilized: avgTensor(0.08) < 0.15 AND avgSM(0.12) < 0.25
			namespace: "ml-underutil", workload: "light-etl", container: "etl",
			gpuModel: "NVIDIA H100 80GB HBM3", gpuProfile: "", nodeName: "gpu-node-2",
			smAvg: 0.12, tensorAvg: 0.08, dramAvg: 0.15, fbUsageAvgMiB: 4096,
		},
		{
			// Memory-bound: avgDRAM(0.82) > 0.60 AND avgTensor(0.10) < 0.15
			// FB too large for any MIG profile → "full_gpu"
			namespace: "ml-membound", workload: "llm-serving", container: "inference",
			gpuModel: "NVIDIA A100-SXM4-80GB", gpuProfile: "", nodeName: "gpu-node-1",
			smAvg: 0.25, tensorAvg: 0.10, dramAvg: 0.82, fbUsageAvgMiB: 72000,
		},
		{
			// Compute-bound underutil: avgTensor(0.20) < UnderutilizedSM(0.25) AND avgDRAM(0.20) < 0.30
			// (after passing idle/membound/underutil checks)
			namespace: "ml-cbu", workload: "sparse-inference", container: "model",
			gpuModel: "NVIDIA A100-SXM4-40GB", gpuProfile: "", nodeName: "gpu-node-1",
			smAvg: 0.30, tensorAvg: 0.20, dramAvg: 0.20, fbUsageAvgMiB: 2048,
		},
		{
			// Well-utilized: high SM + tensor, falls through all other checks
			namespace: "ml-compute", workload: "diffusion-train", container: "trainer",
			gpuModel: "NVIDIA H100 80GB HBM3", gpuProfile: "", nodeName: "gpu-node-2",
			smAvg: 0.78, tensorAvg: 0.72, dramAvg: 0.45, fbUsageAvgMiB: 65000,
		},
		{
			// Already on MIG partition, well-utilized → no downsizing
			namespace: "ml-serving", workload: "nlp-inference", container: "model",
			gpuModel: "NVIDIA A100-SXM4-40GB", gpuProfile: "1g.5gb", nodeName: "gpu-mig-node",
			smAvg: 0.35, tensorAvg: 0.30, dramAvg: 0.40, fbUsageAvgMiB: 3500,
		},
		{
			// Well-utilized (full GPU): no MIG recommendation
			namespace: "ml-wellutil", workload: "stable-diffusion", container: "sd",
			gpuModel: "NVIDIA A100-SXM4-80GB", gpuProfile: "", nodeName: "gpu-node-1",
			smAvg: 0.65, tensorAvg: 0.55, dramAvg: 0.55, fbUsageAvgMiB: 60000,
		},
	}

	for _, wl := range workloads {
		for day := 0; day < 7; day++ {
			jitter := float64(day) * 0.001
			testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
				IntervalStart:       start.AddDate(0, 0, day),
				ClusterUUID:         testutil.TestClusterUUID,
				Namespace:           wl.namespace,
				Workload:            wl.workload,
				WorkloadType:        "deployment",
				ContainerName:       wl.container,
				GPUModelName:        wl.gpuModel,
				GPUProfileName:      wl.gpuProfile,
				NodeName:            wl.nodeName,
				FBUsageMinMiB:       wl.fbUsageAvgMiB * 0.7,
				FBUsageMaxMiB:       wl.fbUsageAvgMiB * 1.2,
				FBUsageAvgMiB:       wl.fbUsageAvgMiB + jitter*100,
				TensorPipeActiveMin: wl.tensorAvg * 0.5,
				TensorPipeActiveMax: wl.tensorAvg * 1.5,
				TensorPipeActiveAvg: wl.tensorAvg + jitter,
				DRAMActiveMin:       wl.dramAvg * 0.6,
				DRAMActiveMax:       wl.dramAvg * 1.3,
				DRAMActiveAvg:       wl.dramAvg + jitter,
				SMActiveMin:         wl.smAvg * 0.5,
				SMActiveMax:         wl.smAvg * 1.4,
				SMActiveAvg:         wl.smAvg + jitter,
			})
		}
	}

	terms := []engine.TermConfig{
		{Name: "short_term", WindowDays: 7, MinDataDays: 3},
	}

	recs, nodeMap, nodeLastSeen, err := engine.QueryGPURecommendations(
		ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, now, terms, nil)
	require.NoError(t, err)
	require.NotNil(t, recs)

	type expectedResult struct {
		key                string
		classification     engine.GPUClassification
		expectMIGSlice     bool   // non-empty profile that is NOT "full_gpu"
		expectFullGPU      bool   // profile == "full_gpu" (MIG-capable but FB too large)
		expectNoMIGProfile bool   // RecommendedGPUProfile == ""
		hasProf            bool
	}

	expectations := []expectedResult{
		{
			key:            "ml-idle/abandoned-notebook/jupyter",
			classification: engine.GPUClassIdle,
			expectMIGSlice: true,
			hasProf:        true,
		},
		{
			key:            "ml-underutil/light-etl/etl",
			classification: engine.GPUClassUnderutilized,
			expectMIGSlice: true,
			hasProf:        true,
		},
		{
			key:            "ml-membound/llm-serving/inference",
			classification: engine.GPUClassMemoryBound,
			expectFullGPU:  true,
			hasProf:        true,
		},
		{
			key:            "ml-cbu/sparse-inference/model",
			classification: engine.GPUClassComputeBoundUnderutil,
			expectMIGSlice: true,
			hasProf:        true,
		},
		{
			key:                "ml-compute/diffusion-train/trainer",
			classification:     engine.GPUClassWellUtilized,
			expectNoMIGProfile: true,
			hasProf:            true,
		},
		{
			key:                "ml-serving/nlp-inference/model",
			classification:     engine.GPUClassWellUtilized,
			expectNoMIGProfile: true,
			hasProf:            true,
		},
		{
			key:                "ml-wellutil/stable-diffusion/sd",
			classification:     engine.GPUClassWellUtilized,
			expectNoMIGProfile: true,
			hasProf:            true,
		},
	}

	for _, exp := range expectations {
		t.Run(exp.key, func(t *testing.T) {
			recList, ok := recs[exp.key]
			require.True(t, ok, "expected recommendation for %s", exp.key)
			require.NotEmpty(t, recList)

			rec := recList[0]
			assert.Equal(t, exp.classification, rec.Classification,
				"classification mismatch for %s", exp.key)
			assert.Equal(t, exp.hasProf, rec.HasProfilingData,
				"HasProfilingData mismatch for %s", exp.key)

			switch {
			case exp.expectMIGSlice:
				assert.NotEmpty(t, rec.RecommendedGPUProfile,
					"expected MIG profile for %s", exp.key)
				assert.NotEqual(t, "full_gpu", rec.RecommendedGPUProfile,
					"expected a MIG slice, not full_gpu, for %s", exp.key)
			case exp.expectFullGPU:
				assert.Equal(t, "full_gpu", rec.RecommendedGPUProfile,
					"FB exceeds all MIG profiles → expect full_gpu for %s", exp.key)
			case exp.expectNoMIGProfile:
				assert.Empty(t, rec.RecommendedGPUProfile,
					"well-utilized should not get MIG recommendation for %s", exp.key)
			}

			assert.Equal(t, "short_term", rec.Term)
			assert.Greater(t, rec.Confidence, float32(0))
		})
	}

	t.Run("node_map_populated", func(t *testing.T) {
		assert.NotEmpty(t, nodeMap)
		assert.Contains(t, nodeMap, "ml-idle/abandoned-notebook/jupyter")
		assert.Equal(t, "gpu-node-1", nodeMap["ml-idle/abandoned-notebook/jupyter"])
	})

	t.Run("node_last_seen_populated", func(t *testing.T) {
		assert.NotEmpty(t, nodeLastSeen)
		assert.Contains(t, nodeLastSeen, "gpu-node-1")
		assert.Contains(t, nodeLastSeen, "gpu-node-2")
		assert.Contains(t, nodeLastSeen, "gpu-mig-node")
	})

	t.Run("mig_profile_workload_keeps_current", func(t *testing.T) {
		recList := recs["ml-serving/nlp-inference/model"]
		require.NotEmpty(t, recList)
		assert.Equal(t, "1g.5gb", recList[0].CurrentGPUProfile)
	})
}
