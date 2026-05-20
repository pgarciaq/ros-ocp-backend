package costdata_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

// kokuEffectiveRatesResponse is the canonical JSON shape returned by
// Koku's /api/cost-management/v1/effective_rates/ endpoint.
// This fixture is taken from a real Koku response and validates that
// our Go struct can deserialize it without data loss.
const kokuEffectiveRatesResponse = `{
  "cluster_id": "my-ocp-cluster-1",
  "provider_uuid": "12345678-1234-1234-1234-123456789abc",
  "distribution_type": "cpu",
  "markup_pct": 10.0,
  "configured_rates": {
    "cpu_core_usage_per_hour": {"infrastructure": 0.0, "supplementary": 0.007},
    "cpu_core_request_per_hour": {"infrastructure": 0.0, "supplementary": 0.2},
    "memory_gb_usage_per_hour": {"infrastructure": 0.0, "supplementary": 0.009},
    "memory_gb_request_per_hour": {"infrastructure": 0.0, "supplementary": 0.05},
    "storage_gb_usage_per_month": {"infrastructure": 0.0, "supplementary": 0.01},
    "storage_gb_request_per_month": {"infrastructure": 0.0, "supplementary": 0.01},
    "node_cost_per_month": {"infrastructure": 1000.0, "supplementary": 0.0},
    "cluster_cost_per_month": {"infrastructure": 10000.0, "supplementary": 0.0}
  },
  "namespace_aggregates": {
    "koku": {
      "cost_model_cpu_cost": 156.24,
      "cost_model_memory_cost": 89.10,
      "infrastructure_cost": 2450.00,
      "distributed_cost": 1337.42,
      "cpu_usage_hours": 730.5,
      "cpu_request_hours": 1461.0,
      "mem_usage_hours": 365.25,
      "mem_request_hours": 730.5
    },
    "openshift-monitoring": {
      "cost_model_cpu_cost": 0.0,
      "cost_model_memory_cost": 0.0,
      "infrastructure_cost": 500.0,
      "distributed_cost": 200.0,
      "cpu_usage_hours": 100.0,
      "cpu_request_hours": 200.0,
      "mem_usage_hours": 50.0,
      "mem_request_hours": 100.0
    }
  }
}`

// TestKokuEffectiveRatesContract validates that the HTTPCostDataProvider can
// correctly parse a real Koku effective_rates JSON response, verifying the
// contract between Koku (Python) and ROS (Go).
func TestKokuEffectiveRatesContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request shape (query params the Go client sends).
		q := r.URL.Query()
		assert.Equal(t, "1234567", q.Get("org_id"), "org_id param")
		assert.Equal(t, "my-ocp-cluster-1", q.Get("cluster_id"), "cluster_id param")
		assert.NotEmpty(t, q.Get("start_date"), "start_date param")
		assert.NotEmpty(t, q.Get("end_date"), "end_date param")

		// Validate date format is YYYY-MM-DD
		for _, key := range []string{"start_date", "end_date"} {
			_, err := time.Parse("2006-01-02", q.Get(key))
			assert.NoError(t, err, "%s should be YYYY-MM-DD format", key)
		}

		// Verify path
		assert.Equal(t, "/api/cost-management/v1/effective_rates/", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(kokuEffectiveRatesResponse))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	ctx := context.Background()
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)

	data, err := provider.GetEffectiveRates(ctx, "1234567", "my-ocp-cluster-1", start, end)
	require.NoError(t, err)
	require.NotNil(t, data)

	// Verify top-level fields
	assert.Equal(t, "my-ocp-cluster-1", data.ClusterID)
	assert.Equal(t, "12345678-1234-1234-1234-123456789abc", data.ProviderUUID)
	assert.Equal(t, "cpu", data.DistributionType)
	assert.InDelta(t, 10.0, data.MarkupPct, 0.001)

	// Verify configured rates structure
	require.Len(t, data.ConfiguredRates, 8, "should parse all 8 metric rates")

	cpuUsage, ok := data.ConfiguredRates["cpu_core_usage_per_hour"]
	require.True(t, ok, "cpu_core_usage_per_hour should be present")
	assert.InDelta(t, 0.0, cpuUsage.Infrastructure, 0.0001)
	assert.InDelta(t, 0.007, cpuUsage.Supplementary, 0.0001)

	nodeCost, ok := data.ConfiguredRates["node_cost_per_month"]
	require.True(t, ok, "node_cost_per_month should be present")
	assert.InDelta(t, 1000.0, nodeCost.Infrastructure, 0.01)
	assert.InDelta(t, 0.0, nodeCost.Supplementary, 0.0001)

	// Verify namespace aggregates
	require.Len(t, data.Namespaces, 2, "should have 2 namespaces")

	kokuNS, ok := data.Namespaces["koku"]
	require.True(t, ok, "koku namespace should be present")
	assert.InDelta(t, 156.24, kokuNS.CostModelCPUCost, 0.01)
	assert.InDelta(t, 89.10, kokuNS.CostModelMemCost, 0.01)
	assert.InDelta(t, 2450.0, kokuNS.InfraCost, 0.01)
	assert.InDelta(t, 1337.42, kokuNS.DistributedCost, 0.01)
	assert.InDelta(t, 730.5, kokuNS.CPUUsageHours, 0.01)
	assert.InDelta(t, 1461.0, kokuNS.CPURequestHours, 0.01)
	assert.InDelta(t, 365.25, kokuNS.MemUsageHours, 0.01)
	assert.InDelta(t, 730.5, kokuNS.MemRequestHours, 0.01)

	monNS, ok := data.Namespaces["openshift-monitoring"]
	require.True(t, ok, "openshift-monitoring namespace should be present")
	assert.InDelta(t, 0.0, monNS.CostModelCPUCost, 0.0001)
	assert.InDelta(t, 500.0, monNS.InfraCost, 0.01)
}

// TestKokuEffectiveRatesContract_EmptyResponse validates that the provider
// handles a valid but empty response (no rates configured, no data).
func TestKokuEffectiveRatesContract_EmptyResponse(t *testing.T) {
	emptyResp := `{
		"cluster_id": "empty-cluster",
		"provider_uuid": "00000000-0000-0000-0000-000000000000",
		"distribution_type": "cpu",
		"markup_pct": 0,
		"configured_rates": {},
		"namespace_aggregates": {}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(emptyResp))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	data, err := provider.GetEffectiveRates(
		context.Background(), "1234567", "empty-cluster",
		time.Now().AddDate(0, 0, -30), time.Now(),
	)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Equal(t, "empty-cluster", data.ClusterID)
	assert.Empty(t, data.ConfiguredRates)
	assert.Empty(t, data.Namespaces)
}

// TestKokuEffectiveRatesContract_RoundTrip verifies that re-serializing the
// parsed struct produces a JSON object with the same keys as the input.
// This catches field name mismatches between Go struct tags and Koku output.
func TestKokuEffectiveRatesContract_RoundTrip(t *testing.T) {
	var original map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(kokuEffectiveRatesResponse), &original))

	var parsed costdata.ClusterCostData
	require.NoError(t, json.Unmarshal([]byte(kokuEffectiveRatesResponse), &parsed))

	reEncoded, err := json.Marshal(parsed)
	require.NoError(t, err)

	var roundTripped map[string]interface{}
	require.NoError(t, json.Unmarshal(reEncoded, &roundTripped))

	// Verify all top-level keys survive the round-trip
	for key := range original {
		assert.Contains(t, roundTripped, key,
			"key %q from Koku response should survive Go parse → re-serialize", key)
	}

	// Verify nested namespace keys
	origNS := original["namespace_aggregates"].(map[string]interface{})
	rtNS := roundTripped["namespace_aggregates"].(map[string]interface{})
	for nsName, nsData := range origNS {
		require.Contains(t, rtNS, nsName)
		origFields := nsData.(map[string]interface{})
		rtFields := rtNS[nsName].(map[string]interface{})
		for field := range origFields {
			assert.Contains(t, rtFields, field,
				"namespace %q field %q should survive round-trip", nsName, field)
		}
	}
}
