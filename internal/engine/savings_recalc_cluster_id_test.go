package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestRecalculateSavingsForOrg_ClusterUUIDFlowsVerbatim pins the consumer side
// of the koku savings-callback contract (#769, V15-19): the cluster_uuid KOKU
// sends is used VERBATIM — the per-cluster recalculation selection targets
// exactly that cluster, and the rate-cache invalidation clears exactly that
// (org, cluster) entry. Koku's notifier once sent the provider UUID in the
// cluster_uuid field; this test is the contract guard so a change on either
// side that breaks the identity fails loudly.
func TestRecalculateSavingsForOrg_ClusterUUIDFlowsVerbatim(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	org := "org-savings-recalc-cluster-id"
	targetCluster := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb" // the REAL cluster (distinct from any provider UUID)
	otherCluster := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	seedClustersForRecalcTest(t, pool, org, targetCluster)
	seedClustersForRecalcTest(t, pool, org, otherCluster)

	// Selection: the per-cluster recalculation must receive the supplied
	// cluster_uuid verbatim — no provider→cluster translation exists. Capture
	// EVERY call (a last-write-only capture could false-pass if a regression
	// selected extra clusters first).
	var mu sync.Mutex
	var captured []string
	restore := SetClusterSavingsRecalcFuncForTest(func(ctx context.Context, p *pgxpool.Pool, orgID, clusterUUID string, recTypes []string) error {
		mu.Lock()
		captured = append(captured, clusterUUID)
		mu.Unlock()
		return nil
	})
	defer restore()

	// Rate-cache observability: an effective-rates provider whose fetches we
	// count. Seeding the cache for both clusters, then recalculating for the
	// target cluster, must invalidate ONLY the target's entry.
	var fetchCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetchCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cluster_id":"c","currency":"USD","configured_rates":{},"namespace_aggregates":{}}`))
	}))
	defer srv.Close()
	clearCache := costdata.ResetCostDataCacheForTest(128)
	defer clearCache()
	provider := costdata.NewHTTPCostDataProvider(srv.URL, 5*time.Second)
	start := time.Now().AddDate(0, 0, -7)
	end := time.Now()
	_, err := provider.GetEffectiveRates(ctx, org, targetCluster, start, end)
	require.NoError(t, err)
	_, err = provider.GetEffectiveRates(ctx, org, otherCluster, start, end)
	require.NoError(t, err)
	require.Equal(t, int32(2), atomic.LoadInt32(&fetchCount), "both cache entries seeded")

	RecalculateSavingsForOrg(ctx, pool, org, targetCluster, []string{savingsRecTypeNode})

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{targetCluster}, captured,
		"the per-cluster recalculation must select EXACTLY the supplied cluster_uuid, verbatim (#769)")

	// The target cluster's cache entry was invalidated (refetched); the other
	// cluster's was not.
	_, err = provider.GetEffectiveRates(ctx, org, targetCluster, start, end)
	require.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&fetchCount),
		"rate-cache invalidation must clear exactly the supplied cluster's entry (#769)")
	_, err = provider.GetEffectiveRates(ctx, org, otherCluster, start, end)
	require.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&fetchCount),
		"another cluster's cache entry must survive the targeted invalidation (#769)")
}
