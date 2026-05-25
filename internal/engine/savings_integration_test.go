package engine_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// serveMockEffectiveRates starts an httptest server that returns the given
// ClusterCostData as a JSON response on any request.
func serveMockEffectiveRates(t *testing.T, data *costdata.ClusterCostData) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			t.Errorf("mock server encode error: %v", err)
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSavingsPipeline_Integration exercises the full savings pipeline:
// testcontainers DB → digest seeding → recommendation engine → mock Koku
// effective-rates → savings computation → DB write → DB read verification.
func TestSavingsPipeline_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`,
		testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES (1, $1, 'savings-test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID)
	require.NoError(t, err)

	// Seed 7 days of digest data with known request values.
	// CPU requests grow from 200 to 260 mc; memory from 512 MiB to ~518 MiB.
	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)

	// Generate recommendations
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs, "engine should produce at least one recommendation")

	// Capture pre-savings state
	for _, r := range recs {
		require.Equal(t, float32(0), r.EstimatedSavingsCents, "savings should be zero before ApplySavingsEstimates")
	}

	t.Run("with cost data from mock Koku", func(t *testing.T) {
		// Set up mock effective-rates response with known cost/usage values.
		// Rates: $1/core-hour CPU cost model, $0.5/GiB-hour memory cost model,
		//        $0.5/core-hour infra, $0.3/core-hour distributed (cpu distribution).
		mockData := &costdata.ClusterCostData{
			DistributionType: "cpu",
			Namespaces: map[string]costdata.NamespaceCosts{
				testutil.TestNamespace: {
					CostModelCPUCost: 730.0,
					CostModelMemCost: 365.0,
					InfraCost:        365.0,
					DistributedCost:  219.0,
					CPURequestHours:  730.0,
					MemRequestHours:  730.0,
				},
			},
		}

		srv := serveMockEffectiveRates(t, mockData)
		provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*1e9) // 5s timeout

		costData, err := provider.GetEffectiveRates(ctx, testutil.TestOrgID, testutil.TestClusterUUID, start, end)
		require.NoError(t, err)
		require.NotNil(t, costData)
		assert.Equal(t, "cpu", costData.DistributionType)

		// Apply savings
		engine.ApplySavingsEstimates(recs, costData)

		// Verify savings were computed (non-zero for at least one rec)
		hasNonZero := false
		for _, r := range recs {
			if r.EstimatedSavingsCents != 0 {
				hasNonZero = true
				break
			}
		}
		assert.True(t, hasNonZero, "at least one recommendation should have non-zero savings")

		// Verify no NotifNoCostData notification since namespace was found
		for _, r := range recs {
			if r.Namespace == testutil.TestNamespace {
				for _, code := range r.NotificationCodes {
					assert.NotEqual(t, engine.NotifNoCostData, code,
						"container %s should not have NotifNoCostData", r.ContainerName)
				}
			}
		}

		// Write to DB and verify persistence
		err = engine.WriteRecommendations(ctx, pool, recs)
		require.NoError(t, err)

		// Read back and verify savings persisted
		var savedSavings float32
		err = pool.QueryRow(ctx,
			`SELECT estimated_monthly_savings_usd FROM recommendation_sets
			 WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3 AND container_name = $4
			 LIMIT 1`,
			testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace, testutil.TestContainer,
		).Scan(&savedSavings)
		require.NoError(t, err)
		assert.NotEqual(t, float32(0), savedSavings, "persisted savings should be non-zero")
	})

	t.Run("without cost data (nil provider)", func(t *testing.T) {
		// Re-generate recommendations (fresh, no savings yet)
		freshRecs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
		require.NoError(t, err)
		require.NotEmpty(t, freshRecs)

		engine.ApplySavingsEstimates(freshRecs, nil)

		for _, r := range freshRecs {
			assert.Equal(t, float32(0), r.EstimatedSavingsCents,
				"savings should be zero when cost data is nil")
			assert.Contains(t, r.NotificationCodes, engine.NotifNoCostData,
				"should have NotifNoCostData when cost data is nil")
		}
	})

	t.Run("memory distribution type", func(t *testing.T) {
		freshRecs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
		require.NoError(t, err)
		require.NotEmpty(t, freshRecs)

		mockData := &costdata.ClusterCostData{
			DistributionType: "memory",
			Namespaces: map[string]costdata.NamespaceCosts{
				testutil.TestNamespace: {
					CostModelCPUCost: 730.0,
					CostModelMemCost: 365.0,
					InfraCost:        365.0,
					DistributedCost:  219.0,
					CPURequestHours:  730.0,
					MemRequestHours:  730.0,
				},
			},
		}

		engine.ApplySavingsEstimates(freshRecs, mockData)

		// With memory distribution, infra+distributed savings use memory delta
		// instead of CPU delta. The total savings should differ from CPU distribution.
		hasNonZero := false
		for _, r := range freshRecs {
			if r.EstimatedSavingsCents != 0 {
				hasNonZero = true
				break
			}
		}
		assert.True(t, hasNonZero, "memory distribution should still produce non-zero savings")
	})

	t.Run("namespace missing from cost data", func(t *testing.T) {
		freshRecs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
		require.NoError(t, err)
		require.NotEmpty(t, freshRecs)

		mockData := &costdata.ClusterCostData{
			DistributionType: "cpu",
			Namespaces: map[string]costdata.NamespaceCosts{
				"unrelated-namespace": {
					CostModelCPUCost: 100.0,
					CPURequestHours:  100.0,
					MemRequestHours:  100.0,
				},
			},
		}

		engine.ApplySavingsEstimates(freshRecs, mockData)

		for _, r := range freshRecs {
			assert.Equal(t, float32(0), r.EstimatedSavingsCents,
				"savings should be zero when namespace not in cost data")
			assert.Contains(t, r.NotificationCodes, engine.NotifNoCostData,
				"should have NotifNoCostData when namespace not found")
		}
	})

	t.Run("zero cost rates produce zero savings", func(t *testing.T) {
		freshRecs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
		require.NoError(t, err)
		require.NotEmpty(t, freshRecs)

		mockData := &costdata.ClusterCostData{
			DistributionType: "cpu",
			Namespaces: map[string]costdata.NamespaceCosts{
				testutil.TestNamespace: {
					CostModelCPUCost: 0,
					CostModelMemCost: 0,
					InfraCost:        0,
					DistributedCost:  0,
					CPURequestHours:  730.0,
					MemRequestHours:  730.0,
				},
			},
		}

		engine.ApplySavingsEstimates(freshRecs, mockData)

		for _, r := range freshRecs {
			assert.Equal(t, float32(0), r.EstimatedSavingsCents,
				"savings should be zero when all cost rates are zero")
			for _, code := range r.NotificationCodes {
				assert.NotEqual(t, engine.NotifNoCostData, code,
					"should NOT have NotifNoCostData when namespace exists (even with zero costs)")
			}
		}
	})

	t.Run("HTTPCostDataProvider error handling", func(t *testing.T) {
		// Server that returns 500
		errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}))
		t.Cleanup(errSrv.Close)

		provider := costdata.NewHTTPCostDataProvider(errSrv.URL, 5*1e9)
		_, err := provider.GetEffectiveRates(ctx, testutil.TestOrgID, testutil.TestClusterUUID, start, end)
		assert.Error(t, err, "should return error on 500 response")
		assert.Contains(t, err.Error(), "500")
	})

	t.Run("HTTPCostDataProvider invalid JSON", func(t *testing.T) {
		badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("not json"))
		}))
		t.Cleanup(badSrv.Close)

		provider := costdata.NewHTTPCostDataProvider(badSrv.URL, 5*1e9)
		_, err := provider.GetEffectiveRates(ctx, testutil.TestOrgID, testutil.TestClusterUUID, start, end)
		assert.Error(t, err, "should return error on invalid JSON")
	})
}
