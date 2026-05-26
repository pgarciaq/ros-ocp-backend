package engine

import (
	"context"
	"math/rand"
	"runtime"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestThresholdCache_EvictsExpiredEntries(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	const orgCount = 100

	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	t.Cleanup(ClearThresholdSettingsCacheForTest)

	start := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	clock := start
	restoreClock := SetThresholdSettingsNowForTest(func() time.Time { return clock })
	defer restoreClock()

	orgIDs := seedThresholdCacheForOrgs(orgCount, "container")
	requireCacheLen(t, orgCount)

	targetOrg := orgIDs[0]

	// DB holds a value different from the pre-seeded cache entry.
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds)
		VALUES ($1, 'container', '{"cpu_cost_percentile": 0.41}'::jsonb)
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds`, targetOrg)
	if err != nil {
		t.Fatalf("seed DB thresholds: %v", err)
	}

	gotCached, err := ResolveContainerSizingThresholds(ctx, pool, targetOrg)
	if err != nil {
		t.Fatalf("cached resolve: %v", err)
	}
	if gotCached.CPUCostPercentile != defaultContainerSizingThresholds.CPUCostPercentile {
		t.Fatalf("expected cached default %v, got %v",
			defaultContainerSizingThresholds.CPUCostPercentile, gotCached.CPUCostPercentile)
	}

	clock = start.Add(thresholdSettingsCacheTTL + time.Second)

	gotFresh, err := ResolveContainerSizingThresholds(ctx, pool, targetOrg)
	if err != nil {
		t.Fatalf("post-TTL resolve: %v", err)
	}
	if gotFresh.CPUCostPercentile != 0.41 {
		t.Fatalf("expected refreshed DB value 0.41 after TTL, got %v", gotFresh.CPUCostPercentile)
	}

	// Lazy eviction on expired access keeps cache size bounded.
	requireCacheLen(t, orgCount)
}

func requireCacheLen(t *testing.T, want int) {
	t.Helper()
	if got := thresholdCacheLen(); got != want {
		t.Fatalf("threshold cache len = %d, want %d", got, want)
	}
}

func readHeapInuse() uint64 {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapInuse
}

// BenchmarkThresholdCache_MemoryStability verifies cached threshold resolution does not
// grow heap usage unboundedly when resolving across many orgs repeatedly.
func BenchmarkThresholdCache_MemoryStability(b *testing.B) {
	pool := thresholdPerfPoolHandle(b)
	ctx := context.Background()
	const orgCount = 100

	config.ResetForTest()
	InitThresholdDefaults(config.GetConfig())
	ClearThresholdSettingsCacheForTest()
	b.Cleanup(ClearThresholdSettingsCacheForTest)

	orgIDs := seedThresholdCacheForOrgs(orgCount, "container")
	for _, orgID := range orgIDs {
		if err := resolveThresholdForOrg(ctx, pool, orgID); err != nil {
			b.Fatalf("warm-up resolve %s: %v", orgID, err)
		}
	}

	baseline := readHeapInuse()
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		orgID := orgIDs[rng.Intn(len(orgIDs))]
		if err := resolveThresholdForOrg(ctx, pool, orgID); err != nil {
			b.Fatalf("resolve %s: %v", orgID, err)
		}
	}
	b.StopTimer()

	const maxGrowthBytes = 10 * 1024 * 1024
	after := readHeapInuse()
	if after > baseline+maxGrowthBytes {
		b.Errorf("HeapInuse grew by %d bytes (baseline %d, after %d); max allowed growth %d",
			after-baseline, baseline, after, maxGrowthBytes)
	}
	b.ReportMetric(float64(after-baseline)/1024/1024, "heap_growth_mib")
}
