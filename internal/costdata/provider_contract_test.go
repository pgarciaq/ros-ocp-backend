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
// Koku's /api/cost-management/v1/effective_rates/ endpoint when the caller
// passes `money_scale=microcents` (#461): money is integer micro-cents, hours
// are integer milli-hours and markup is integer basis points. This fixture
// validates that our Go struct can deserialize it without data loss.
const kokuEffectiveRatesResponse = `{
  "cluster_id": "my-ocp-cluster-1",
  "provider_uuid": "12345678-1234-1234-1234-123456789abc",
  "distribution_type": "cpu",
  "money_scale": "microcents",
  "markup_pct": 1000,
  "currency": "USD",
  "configured_rates": {
    "cpu_core_usage_per_hour": {"infrastructure": 0, "supplementary": 700000},
    "cpu_core_request_per_hour": {"infrastructure": 0, "supplementary": 20000000},
    "memory_gb_usage_per_hour": {"infrastructure": 0, "supplementary": 900000},
    "memory_gb_request_per_hour": {"infrastructure": 0, "supplementary": 5000000},
    "storage_gb_usage_per_month": {"infrastructure": 0, "supplementary": 1000000},
    "storage_gb_request_per_month": {"infrastructure": 0, "supplementary": 1000000},
    "node_cost_per_month": {"infrastructure": 100000000000, "supplementary": 0},
    "cluster_cost_per_month": {"infrastructure": 1000000000000, "supplementary": 0}
  },
  "namespace_aggregates": {
    "koku": {
      "cost_model_cpu_cost": 15624000000,
      "cost_model_memory_cost": 8910000000,
      "infrastructure_cost": 245000000000,
      "distributed_cost": 133742000000,
      "cpu_usage_hours": 730500,
      "cpu_request_hours": 1461000,
      "mem_usage_hours": 365250,
      "mem_request_hours": 730500
    },
    "openshift-monitoring": {
      "cost_model_cpu_cost": 0,
      "cost_model_memory_cost": 0,
      "infrastructure_cost": 50000000000,
      "distributed_cost": 20000000000,
      "cpu_usage_hours": 100000,
      "cpu_request_hours": 200000,
      "mem_usage_hours": 50000,
      "mem_request_hours": 100000
    }
  }
}`

// TestKokuEffectiveRatesContract validates that the HTTPCostDataProvider can
// correctly parse a real Koku effective_rates JSON response, verifying the
// contract between Koku (Python) and ROS (Go).
func TestKokuEffectiveRatesContract(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request shape (query params the Go client sends).
		q := r.URL.Query()
		assert.Equal(t, "1234567", q.Get("org_id"), "org_id param")
		assert.Equal(t, "my-ocp-cluster-1", q.Get("cluster_id"), "cluster_id param")
		assert.NotEmpty(t, q.Get("start_date"), "start_date param")
		assert.NotEmpty(t, q.Get("end_date"), "end_date param")
		// #461: the reader requires integers and says so on every request.
		assert.Equal(t, "microcents", q.Get("money_scale"), "money_scale param")

		// Validate date format is YYYY-MM-DD
		for _, key := range []string{"start_date", "end_date"} {
			_, err := time.Parse("2006-01-02", q.Get(key))
			assert.NoError(t, err, "%s should be YYYY-MM-DD format", key)
		}

		// Verify path
		assert.Equal(t, "/api/cost-management/v1/effective_rates/", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(kokuEffectiveRatesResponse))
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
	assert.Equal(t, costdata.MoneyScaleMicroCents, data.MoneyScale)
	assert.Equal(t, int64(1000), data.MarkupPct, "10% is 1000 basis points")
	assert.Equal(t, "USD", data.Currency)

	// Verify configured rates structure
	require.Len(t, data.ConfiguredRates, 8, "should parse all 8 metric rates")

	cpuUsage, ok := data.ConfiguredRates["cpu_core_usage_per_hour"]
	require.True(t, ok, "cpu_core_usage_per_hour should be present")
	assert.Equal(t, int64(0), cpuUsage.Infrastructure)
	assert.Equal(t, int64(700000), cpuUsage.Supplementary, "$0.007/core-hour must arrive exactly")

	nodeCost, ok := data.ConfiguredRates["node_cost_per_month"]
	require.True(t, ok, "node_cost_per_month should be present")
	assert.Equal(t, int64(100000000000), nodeCost.Infrastructure, "$1000/month must arrive exactly")
	assert.Equal(t, int64(0), nodeCost.Supplementary)

	// Verify namespace aggregates
	require.Len(t, data.Namespaces, 2, "should have 2 namespaces")

	kokuNS, ok := data.Namespaces["koku"]
	require.True(t, ok, "koku namespace should be present")
	assert.Equal(t, int64(15624000000), kokuNS.CostModelCPUCost, "$156.24 in micro-cents")
	assert.Equal(t, int64(8910000000), kokuNS.CostModelMemCost)
	assert.Equal(t, int64(245000000000), kokuNS.InfraCost)
	assert.Equal(t, int64(133742000000), kokuNS.DistributedCost)
	assert.Equal(t, int64(730500), kokuNS.CPUUsageHours, "730.5 h in milli-hours")
	assert.Equal(t, int64(1461000), kokuNS.CPURequestHours)
	assert.Equal(t, int64(365250), kokuNS.MemUsageHours)
	assert.Equal(t, int64(730500), kokuNS.MemRequestHours)

	monNS, ok := data.Namespaces["openshift-monitoring"]
	require.True(t, ok, "openshift-monitoring namespace should be present")
	assert.Equal(t, int64(0), monNS.CostModelCPUCost)
	assert.Equal(t, int64(50000000000), monNS.InfraCost)
}

// TestKokuEffectiveRatesContract_EmptyResponse validates that the provider
// handles a valid but empty response (no rates configured, no data).
func TestKokuEffectiveRatesContract_EmptyResponse(t *testing.T) {
	t.Parallel()
	emptyResp := `{
		"cluster_id": "empty-cluster",
		"provider_uuid": "00000000-0000-0000-0000-000000000000",
		"distribution_type": "cpu",
		"money_scale": "microcents",
		"markup_pct": 0,
		"currency": "USD",
		"configured_rates": {},
		"namespace_aggregates": {}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(emptyResp))
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

func TestKokuEffectiveRatesContract_ServerTimeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 50*time.Millisecond)
	_, err := provider.GetEffectiveRates(
		context.Background(), "1234567", "cluster-1",
		time.Now().AddDate(0, 0, -30), time.Now(),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP request to Koku")
}

func TestKokuEffectiveRatesContract_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal server error"}`))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	_, err := provider.GetEffectiveRates(
		context.Background(), "1234567", "cluster-1",
		time.Now().AddDate(0, 0, -30), time.Now(),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestKokuEffectiveRatesContract_Unauthorized(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"detail":"Permission denied"}`))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	_, err := provider.GetEffectiveRates(
		context.Background(), "1234567", "cluster-1",
		time.Now().AddDate(0, 0, -30), time.Now(),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestKokuEffectiveRatesContract_MalformedJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cluster_id": "test", "configured_rates": INVALID`))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	_, err := provider.GetEffectiveRates(
		context.Background(), "1234567", "cluster-1",
		time.Now().AddDate(0, 0, -30), time.Now(),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode response")
}

func TestKokuEffectiveRatesContract_ContextCancellation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 10*time.Second)
	_, err := provider.GetEffectiveRates(
		ctx, "1234567", "cluster-1",
		time.Now().AddDate(0, 0, -30), time.Now(),
	)
	require.Error(t, err)
}

// TestKokuEffectiveRatesContract_RoundTrip verifies that re-serializing the
// parsed struct produces a JSON object with the same keys as the input.
// This catches field name mismatches between Go struct tags and Koku output.
func TestKokuEffectiveRatesContract_RoundTrip(t *testing.T) {
	t.Parallel()
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

// TestKokuEffectiveRatesContract_RejectsLegacyMoneyScale is the #461 guard that
// makes a stale Masu loud instead of 100_000_000x wrong.
//
// A Masu build that predates the contract ignores `money_scale` and answers with
// float dollars: `0.007` for a CPU rate. Read as micro-cents that is $0.007e-8,
// but read the other way — a float payload decoded into an integer field — every
// applied saving is off by eight orders of magnitude with no error unless one of
// these two checks fires. Both are exercised here: the echoed scale and, for a
// payload of whole-dollar floats that happen to survive decoding, the scale echo
// alone.
func TestKokuEffectiveRatesContract_RejectsLegacyMoneyScale(t *testing.T) {
	t.Parallel()

	legacyResponses := map[string]string{
		"missing money_scale": `{
			"cluster_id": "legacy-cluster",
			"currency": "USD",
			"markup_pct": 10.0,
			"configured_rates": {"cpu_core_usage_per_hour": {"infrastructure": 0.0, "supplementary": 0.007}},
			"namespace_aggregates": {}
		}`,
		"explicit dollars": `{
			"cluster_id": "legacy-cluster",
			"money_scale": "dollars",
			"currency": "USD",
			"markup_pct": 0,
			"configured_rates": {},
			"namespace_aggregates": {}
		}`,
	}

	for name, body := range legacyResponses {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(srv.Close)

			provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
			data, err := provider.GetEffectiveRates(
				context.Background(), "1234567", "legacy-cluster",
				time.Now().AddDate(0, 0, -30), time.Now(),
			)
			require.Error(t, err, "a non-microcents payload must never be accepted")
			assert.Nil(t, data, "no partial payload may reach the savings math")
			// Either guard may fire first — the float fields fail to decode, the
			// missing echo fails the scale check — but both must name #461 so the
			// operator does not have to guess why an old Masu broke savings.
			assert.Contains(t, err.Error(), "#461",
				"the error must say why the payload was refused")
		})
	}
}

// TestKokuEffectiveRatesContract_RejectsFloatMoneyEvenWhenScaleEchoed proves the
// integer field types are a second, independent guard: a producer that echoes
// `microcents` but still emits floats is rejected at decode time.
func TestKokuEffectiveRatesContract_RejectsFloatMoneyEvenWhenScaleEchoed(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"cluster_id": "float-cluster",
			"money_scale": "microcents",
			"configured_rates": {"cpu_core_usage_per_hour": {"infrastructure": 0.0, "supplementary": 0.007}}
		}`))
	}))
	t.Cleanup(srv.Close)

	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	_, err := provider.GetEffectiveRates(
		context.Background(), "1234567", "float-cluster",
		time.Now().AddDate(0, 0, -30), time.Now(),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode response")
}
