package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func enableBusinessHoursForTest(t *testing.T) {
	t.Helper()
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	config.ResetForTest()
}

func seedWeekdayBHSchedule(t *testing.T, pool *pgxpool.Pool, orgID string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, UpsertBusinessHoursSchedule(ctx, pool, BusinessHoursSchedule{
		OrgID:       orgID,
		ClusterUUID: OrgClusterSentinelUUID,
		Timezone:    "UTC",
		Days:        []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
		StartTime:   "00:00",
		EndTime:     "23:59",
		Enabled:     true,
	}))
}

func TestQueryContainerDigestsByScheduleTypeForContainers_FiltersByKeys(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-keys-" + t.Name()
	end := testutil.BaseDate.AddDate(0, 0, 6)

	for i := 0; i < 3; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    testutil.BaseDate.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      testutil.TestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: testutil.TestContainer,
			CPUUsageP95MC: 50,
			ScheduleType:  "business_hours",
		})
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    testutil.BaseDate.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      "other-workload",
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: "other-container",
			CPUUsageP95MC: 999,
			ScheduleType:  "business_hours",
		})
	}

	grouped, err := QueryContainerDigestsByScheduleTypeForContainers(ctx, pool, orgID, []PageContainerDigestKey{{
		ClusterUUID:   testutil.TestClusterUUID,
		Namespace:     testutil.TestNamespace,
		Workload:      testutil.TestWorkload,
		ContainerName: testutil.TestContainer,
	}}, testutil.BaseDate, end, digestScheduleBusinessHours)
	require.NoError(t, err)
	require.Len(t, grouped[testutil.TestClusterUUID], 1)
	for _, rows := range grouped[testutil.TestClusterUUID] {
		for _, r := range rows {
			assert.Equal(t, int64(50), r.CPUUsageP95MC)
		}
	}
}

func TestBHEnrichment_OnlyQueriesPageContainers(t *testing.T) {
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-page-keys-" + t.Name()
	seedWeekdayBHSchedule(t, pool, orgID)

	for i := 0; i < 7; i++ {
		for _, spec := range []struct {
			workload, container string
			cpu                 int64
		}{
			{testutil.TestWorkload, testutil.TestContainer, 200},
			{"other-workload", "other-container", 400},
		} {
			testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
				BucketDate:     testutil.BaseDate.AddDate(0, 0, i),
				OrgID:          orgID,
				ClusterUUID:    testutil.TestClusterUUID,
				Namespace:      testutil.TestNamespace,
				Workload:       spec.workload,
				WorkloadType:   testutil.TestWorkloadType,
				ContainerName:  spec.container,
				CPUUsageP95MC:  spec.cpu,
				MemUsageP95KiB: 524288,
				ScheduleType:   "all_hours",
			})
			testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
				BucketDate:    testutil.BaseDate.AddDate(0, 0, i),
				OrgID:         orgID,
				ClusterUUID:   testutil.TestClusterUUID,
				Namespace:     testutil.TestNamespace,
				Workload:      spec.workload,
				WorkloadType:  testutil.TestWorkloadType,
				ContainerName: spec.container,
				CPUUsageP95MC: spec.cpu / 4,
				ScheduleType:  "business_hours",
			})
		}
	}

	var captured []PageContainerDigestKey
	origQuery := queryContainerDigestsByScheduleForContainers
	queryContainerDigestsByScheduleForContainers = func(
		ctx context.Context,
		pool *pgxpool.Pool,
		orgID string,
		keys []PageContainerDigestKey,
		start, end time.Time,
		scheduleType string,
	) (map[string]map[containerKey][]DigestRow, error) {
		captured = append([]PageContainerDigestKey(nil), keys...)
		return origQuery(ctx, pool, orgID, keys, start, end, scheduleType)
	}
	t.Cleanup(func() { queryContainerDigestsByScheduleForContainers = origQuery })

	page := []model.NativeContainerResult{{
		ClusterUUID:     testutil.TestClusterUUID,
		Project:         testutil.TestNamespace,
		Workload:        testutil.TestWorkload,
		WorkloadType:    testutil.TestWorkloadType,
		Container:       testutil.TestContainer,
		Recommendations: map[string]model.TermRecommendation{},
	}}
	require.NoError(t, EnrichNativeContainerResultsWithBusinessHours(ctx, pool, orgID, page))

	require.Len(t, captured, 1)
	assert.Equal(t, testutil.TestContainer, captured[0].ContainerName)
	assert.Equal(t, testutil.TestWorkload, captured[0].Workload)
}

func TestDigestQuery_FilterAllHours(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-filter-all-" + t.Name()
	end := testutil.BaseDate.AddDate(0, 0, 6)

	for i := 0; i < 3; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    testutil.BaseDate.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      testutil.TestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: testutil.TestContainer,
			CPUUsageP95MC: 500,
			ScheduleType:  "all_hours",
		})
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    testutil.BaseDate.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      testutil.TestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: testutil.TestContainer,
			CPUUsageP95MC: 50,
			ScheduleType:  "business_hours",
		})
	}

	allGrouped, err := QueryContainerDigestsByScheduleType(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end, digestScheduleAllHours)
	require.NoError(t, err)
	require.Len(t, allGrouped, 1)
	for _, rows := range allGrouped {
		require.Len(t, rows, 3)
		for _, r := range rows {
			assert.Equal(t, int64(500), r.CPUUsageP95MC)
		}
	}
}

func TestDigestQuery_FilterBusinessHours(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-filter-bh-" + t.Name()
	end := testutil.BaseDate.AddDate(0, 0, 6)

	for i := 0; i < 3; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    testutil.BaseDate.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      testutil.TestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: testutil.TestContainer,
			CPUUsageP95MC: 500,
			ScheduleType:  "all_hours",
		})
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    testutil.BaseDate.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      testutil.TestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: testutil.TestContainer,
			CPUUsageP95MC: 50,
			ScheduleType:  "business_hours",
		})
	}

	bhGrouped, err := QueryContainerDigestsByScheduleType(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end, digestScheduleBusinessHours)
	require.NoError(t, err)
	require.Len(t, bhGrouped, 1)
	for _, rows := range bhGrouped {
		require.Len(t, rows, 3)
		for _, r := range rows {
			assert.Equal(t, int64(50), r.CPUUsageP95MC)
		}
	}
}

func TestRecommendContainer_DualStream(t *testing.T) {
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-dual-" + t.Name()
	seedWeekdayBHSchedule(t, pool, orgID)

	end := testutil.BaseDate.AddDate(0, 0, 6)
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:       testutil.BaseDate.AddDate(0, 0, i),
			OrgID:            orgID,
			ClusterUUID:      testutil.TestClusterUUID,
			Namespace:        testutil.TestNamespace,
			Workload:         testutil.TestWorkload,
			WorkloadType:     testutil.TestWorkloadType,
			ContainerName:    testutil.TestContainer,
			CPURequestP50MC:  400,
			CPURequestP95MC:  450,
			CPUUsageP50MC:    380,
			CPUUsageP95MC:    400,
			CPUUsageMaxMC:    420,
			MemRequestP50KiB: 524288,
			MemRequestP95KiB: 550000,
			MemUsageP95KiB:   500000,
			ScheduleType:     "all_hours",
		})
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:       testutil.BaseDate.AddDate(0, 0, i),
			OrgID:            orgID,
			ClusterUUID:      testutil.TestClusterUUID,
			Namespace:        testutil.TestNamespace,
			Workload:         testutil.TestWorkload,
			WorkloadType:     testutil.TestWorkloadType,
			ContainerName:    testutil.TestContainer,
			CPURequestP50MC:  80,
			CPURequestP95MC:  90,
			CPUUsageP50MC:    70,
			CPUUsageP95MC:    80,
			CPUUsageMaxMC:    85,
			MemRequestP50KiB: 100000,
			MemRequestP95KiB: 110000,
			MemUsageP95KiB:   95000,
			ScheduleType:     "business_hours",
		})
	}

	allRecs, err := RecommendAllWorkloads(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, allRecs)

	var allHoursCostCPU int64
	for _, r := range allRecs {
		if r.Term == "short" && r.Engine == "cost" {
			allHoursCostCPU = r.RecCPURequestMC
		}
	}
	require.True(t, allHoursCostCPU > 200, "all-hours CPU should reflect high usage")

	native := buildNativeFromRecs(allRecs)
	require.NoError(t, EnrichNativeContainerResultsWithBusinessHours(ctx, pool, orgID, native))

	term := native[0].Recommendations["short_term"]
	require.NotNil(t, term.Cost)
	require.NotNil(t, term.Cost.BusinessHours)
	require.NotNil(t, term.Cost.BusinessHours.CPURequestMillicores)
	assert.True(t, *term.Cost.BusinessHours.CPURequestMillicores < allHoursCostCPU,
		"business hours CPU request should be lower than all-hours on fixture")
}

func TestRecommendContainer_AllHoursUnchanged(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-unchanged-" + t.Name()
	end := testutil.BaseDate.AddDate(0, 0, 6)

	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:     testutil.BaseDate.AddDate(0, 0, i),
			OrgID:          orgID,
			ClusterUUID:    testutil.TestClusterUUID,
			Namespace:      testutil.TestNamespace,
			Workload:       testutil.TestWorkload,
			WorkloadType:   testutil.TestWorkloadType,
			ContainerName:  testutil.TestContainer,
			CPUUsageP95MC:  200 + int64(i)*10,
			MemUsageP95KiB: 524288,
			ScheduleType:   "all_hours",
		})
	}

	baseline, err := RecommendAllWorkloads(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)

	// Add business_hours digests that must not affect all-hours stream.
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:     testutil.BaseDate.AddDate(0, 0, i),
			OrgID:          orgID,
			ClusterUUID:    testutil.TestClusterUUID,
			Namespace:      testutil.TestNamespace,
			Workload:       testutil.TestWorkload,
			WorkloadType:   testutil.TestWorkloadType,
			ContainerName:  testutil.TestContainer,
			CPUUsageP95MC:  10,
			MemUsageP95KiB: 10000,
			ScheduleType:   "business_hours",
		})
	}

	after, err := RecommendAllWorkloads(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)
	require.Equal(t, len(baseline), len(after))

	baselineMap := recMap(baseline)
	for _, r := range after {
		key := r.Term + "/" + r.Engine
		want, ok := baselineMap[key]
		require.True(t, ok)
		assert.Equal(t, want.RecCPURequestMC, r.RecCPURequestMC, key)
		assert.Equal(t, want.RecMemRequestKiB, r.RecMemRequestKiB, key)
	}
}

func TestRecommendContainer_BHNotConfigured_NoBHField(t *testing.T) {
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-no-sched-" + t.Name()
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:     testutil.BaseDate.AddDate(0, 0, i),
			OrgID:          orgID,
			ClusterUUID:    testutil.TestClusterUUID,
			Namespace:      testutil.TestNamespace,
			Workload:       testutil.TestWorkload,
			WorkloadType:   testutil.TestWorkloadType,
			ContainerName:  testutil.TestContainer,
			CPUUsageP95MC:  200 + int64(i)*10,
			MemUsageP95KiB: 524288,
			ScheduleType:   "all_hours",
		})
	}

	end := testutil.BaseDate.AddDate(0, 0, 6)
	recs, err := RecommendAllWorkloads(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)
	native := buildNativeFromRecs(recs)
	require.NoError(t, EnrichNativeContainerResultsWithBusinessHours(ctx, pool, orgID, native))

	detail := model.BuildDetailResponse(&native[0], nil, time.Time{}, model.ListResponseOptions{})
	b, err := json.Marshal(detail)
	require.NoError(t, err)
	assert.NotContains(t, string(b), `"business_hours"`)
}

func TestRecommendContainer_BHInsufficientData(t *testing.T) {
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-insuff-" + t.Name()
	seedWeekdayBHSchedule(t, pool, orgID)

	end := testutil.BaseDate.AddDate(0, 0, 6)
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    testutil.BaseDate.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      testutil.TestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: testutil.TestContainer,
			CPUUsageP95MC: 200,
			ScheduleType:  "all_hours",
		})
	}
	for i := 0; i < 2; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    testutil.BaseDate.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      testutil.TestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: testutil.TestContainer,
			CPUUsageP95MC: 50,
			ScheduleType:  "business_hours",
		})
	}

	recs, err := RecommendAllWorkloads(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)
	native := buildNativeFromRecs(recs)
	require.NoError(t, EnrichNativeContainerResultsWithBusinessHours(ctx, pool, orgID, native))

	term := native[0].Recommendations["long_term"]
	require.NotNil(t, term.Cost)
	require.NotNil(t, term.Cost.BusinessHours)
	assert.Contains(t, term.Cost.BusinessHours.Reason, "insufficient business hours data")
	assert.Nil(t, term.Cost.BusinessHours.CPURequestMillicores)
}

func TestRecommendContainer_BHFieldShape(t *testing.T) {
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-shape-" + t.Name()
	seedWeekdayBHSchedule(t, pool, orgID)
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:     testutil.BaseDate.AddDate(0, 0, i),
			OrgID:          orgID,
			ClusterUUID:    testutil.TestClusterUUID,
			Namespace:      testutil.TestNamespace,
			Workload:       testutil.TestWorkload,
			WorkloadType:   testutil.TestWorkloadType,
			ContainerName:  testutil.TestContainer,
			CPUUsageP95MC:  200 + int64(i)*10,
			MemUsageP95KiB: 524288,
			ScheduleType:   "all_hours",
		})
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    testutil.BaseDate.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      testutil.TestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: testutil.TestContainer,
			CPUUsageP95MC: 80,
			ScheduleType:  "business_hours",
		})
	}

	end := testutil.BaseDate.AddDate(0, 0, 6)
	recs, _ := RecommendAllWorkloads(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	native := buildNativeFromRecs(recs)
	require.NoError(t, EnrichNativeContainerResultsWithBusinessHours(ctx, pool, orgID, native))

	detail := model.BuildDetailResponse(&native[0], nil, time.Time{}, model.ListResponseOptions{})
	b, err := json.Marshal(detail)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))
	terms := raw["recommendations"].(map[string]any)["recommendation_terms"].(map[string]any)
	cost := terms["short_term"].(map[string]any)["recommendation_engines"].(map[string]any)["cost"].(map[string]any)
	bh := cost["business_hours"].(map[string]any)
	req := bh["requests"].(map[string]any)
	cpu := req["cpu"].(map[string]any)
	assert.Equal(t, "cores", cpu["format"])
	_, hasLimits := bh["limits"]
	assert.True(t, hasLimits)
}

func TestRecommendContainer_NoBHDigests_SkipsGracefully(t *testing.T) {
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-nodig-" + t.Name()
	seedWeekdayBHSchedule(t, pool, orgID)
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:     testutil.BaseDate.AddDate(0, 0, i),
			OrgID:          orgID,
			ClusterUUID:    testutil.TestClusterUUID,
			Namespace:      testutil.TestNamespace,
			Workload:       testutil.TestWorkload,
			WorkloadType:   testutil.TestWorkloadType,
			ContainerName:  testutil.TestContainer,
			CPUUsageP95MC:  200 + int64(i)*10,
			MemUsageP95KiB: 524288,
			ScheduleType:   "all_hours",
		})
	}

	end := testutil.BaseDate.AddDate(0, 0, 6)
	recs, err := RecommendAllWorkloads(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)
	native := buildNativeFromRecs(recs)
	require.NoError(t, EnrichNativeContainerResultsWithBusinessHours(ctx, pool, orgID, native))

	detail := model.BuildDetailResponse(&native[0], nil, time.Time{}, model.ListResponseOptions{})
	b, err := json.Marshal(detail)
	require.NoError(t, err)
	assert.NotContains(t, string(b), `"business_hours"`)
}

func TestRecommendContainer_DecayIndependent(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-decay-" + t.Name()
	end := testutil.BaseDate.AddDate(0, 0, 6)

	// All-hours: recent high usage dominates with default decay.
	for i := 0; i < 7; i++ {
		cpu := int64(200)
		if i >= 5 {
			cpu = 800
		}
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    testutil.BaseDate.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      testutil.TestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: testutil.TestContainer,
			CPUUsageP95MC: cpu,
			ScheduleType:  "all_hours",
		})
	}
	// Business hours: recent low usage; older days were high (decay weights streams independently).
	for i := 0; i < 7; i++ {
		cpu := int64(700)
		if i >= 5 {
			cpu = 150
		}
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    testutil.BaseDate.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      testutil.TestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: testutil.TestContainer,
			CPUUsageP95MC: cpu,
			ScheduleType:  "business_hours",
		})
	}

	allRecs, err := RecommendAllWorkloads(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)
	var allMediumCost int64
	for _, r := range allRecs {
		if r.Term == "medium" && r.Engine == "cost" {
			allMediumCost = r.RecCPURequestMC
		}
	}
	require.True(t, allMediumCost > 100, "all-hours medium term should reflect high recent usage")

	bhGrouped, err := QueryContainerDigestsByScheduleType(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end, digestScheduleBusinessHours)
	require.NoError(t, err)
	key := containerKey{Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload, WorkloadType: testutil.TestWorkloadType, ContainerName: testutil.TestContainer}
	terms := DefaultTerms()
	bhByTerm := recommendContainerStream(key, bhGrouped[key], terms, OOMConfig{}, defaultContainerSizingThresholds)
	bhMedium := bhByTerm["medium_term"]["cost"].CPURequestMillicores

	require.NotNil(t, bhMedium)
	assert.True(t, *bhMedium > 100, "business-hours stream should produce non-floor recommendation")
	assert.NotEqual(t, allMediumCost, *bhMedium, "streams should diverge with different digest histories")
}

func TestRecommendContainer_AllHoursDecay_Unchanged(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-decay-all-" + t.Name()
	end := testutil.BaseDate.AddDate(0, 0, 6)

	for i := 0; i < 7; i++ {
		cpu := int64(100)
		if i >= 5 {
			cpu = 400
		}
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    testutil.BaseDate.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      testutil.TestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: testutil.TestContainer,
			CPUUsageP95MC: cpu,
			ScheduleType:  "all_hours",
		})
	}

	before, err := RecommendAllWorkloads(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)

	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    testutil.BaseDate.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      testutil.TestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: testutil.TestContainer,
			CPUUsageP95MC: 10,
			ScheduleType:  "business_hours",
		})
	}

	after, err := RecommendAllWorkloads(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)

	beforeMap := recMap(before)
	for _, r := range after {
		if r.Term != "short" || r.Engine != "cost" {
			continue
		}
		assert.Equal(t, beforeMap["short/cost"].RecCPURequestMC, r.RecCPURequestMC)
	}
}

func TestRecommendNamespace_DualStream(t *testing.T) {
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-ns-dual-" + t.Name()
	seedWeekdayBHSchedule(t, pool, orgID)

	end := testutil.BaseDate.AddDate(0, 0, 6)
	seedNamespaceDigestSeriesForBH(t, pool, orgID, testutil.TestNamespace, 7, 500, 20, 400000, 1024, "all_hours")
	seedNamespaceDigestSeriesForBH(t, pool, orgID, testutil.TestNamespace, 7, 100, 5, 80000, 256, "business_hours")

	nsRecs, err := RecommendAllNamespaces(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, nsRecs)

	var allMediumCPU int64
	for _, r := range nsRecs {
		if r.Term == "medium" && r.Engine == "cost" {
			allMediumCPU = r.RecCPURequestMC
			assert.Equal(t, digestScheduleAllHours, r.ScheduleType)
		}
	}
	require.True(t, allMediumCPU > 100)

	bhRecs, err := RecommendBusinessHoursNamespaces(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, bhRecs)

	var bhMediumCPU int64
	for _, r := range bhRecs {
		if r.Term == "medium" && r.Engine == "cost" {
			bhMediumCPU = r.RecCPURequestMC
			assert.Equal(t, digestScheduleBusinessHours, r.ScheduleType)
		}
	}
	require.True(t, bhMediumCPU > 0)
	assert.NotEqual(t, allMediumCPU, bhMediumCPU)

	allMediumCPUCopy := allMediumCPU
	native := []model.NativeNamespaceResult{{
		ID:          model.NativeNamespaceID(testutil.TestClusterUUID, testutil.TestNamespace),
		ClusterUUID: testutil.TestClusterUUID,
		Project:     testutil.TestNamespace,
		Recommendations: map[string]any{
			"medium_term": model.TermRecommendation{
				Cost: &model.EngineRecommendation{CPURequestMillicores: &allMediumCPUCopy},
			},
		},
	}}
	require.NoError(t, EnrichNativeNamespaceResultsWithBusinessHours(ctx, pool, orgID, native))
	term := native[0].Recommendations["medium_term"].(model.TermRecommendation)
	require.NotNil(t, term.Cost.BusinessHours)
	require.NotNil(t, term.Cost.BusinessHours.CPURequestMillicores)
	assert.Equal(t, bhMediumCPU, *term.Cost.BusinessHours.CPURequestMillicores)
}

func TestRecommendBusinessHoursNamespaces_NoSchedule(t *testing.T) {
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-ns-none-" + t.Name()

	end := testutil.BaseDate.AddDate(0, 0, 6)
	seedNamespaceDigestSeriesForBH(t, pool, orgID, testutil.TestNamespace, 7, 100, 5, 80000, 256, digestScheduleBusinessHours)

	recs, err := RecommendBusinessHoursNamespaces(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	assert.Empty(t, recs)
}

func TestRecommendBusinessHoursNamespaces_PersistedRows(t *testing.T) {
	enableBusinessHoursForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-ns-persist-" + t.Name()
	seedWeekdayBHSchedule(t, pool, orgID)

	end := testutil.BaseDate.AddDate(0, 0, 6)
	seedNamespaceDigestSeriesForBH(t, pool, orgID, testutil.TestNamespace, 7, 500, 20, 400000, 1024, digestScheduleAllHours)
	seedNamespaceDigestSeriesForBH(t, pool, orgID, testutil.TestNamespace, 7, 100, 5, 80000, 256, digestScheduleBusinessHours)

	allRecs, err := RecommendAllNamespaces(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NoError(t, WriteNamespaceRecommendations(ctx, pool, allRecs))

	bhRecs, err := RecommendBusinessHoursNamespaces(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, bhRecs)
	require.NoError(t, WriteNamespaceRecommendations(ctx, pool, bhRecs))

	var allCount, bhCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM namespace_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace_name = $3 AND term IS NOT NULL`,
		orgID, testutil.TestClusterUUID, testutil.TestNamespace).Scan(&allCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM namespace_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace_name = $3
		  AND term IS NOT NULL AND schedule_type = 'business_hours'`,
		orgID, testutil.TestClusterUUID, testutil.TestNamespace).Scan(&bhCount))
	assert.Greater(t, allCount, 0)
	assert.Greater(t, bhCount, 0)
	assert.NotEqual(t, allCount, bhCount)
}

func seedNamespaceDigestSeriesForBH(
	t *testing.T,
	pool *pgxpool.Pool,
	orgID, namespace string,
	days int,
	baseCPU, cpuStep, baseMem, memStep int64,
	scheduleType string,
) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < days; i++ {
		bucketDate := testutil.BaseDate.AddDate(0, 0, i)
		testutil.EnsureMonthlyPartition(t, pool, "daily_namespace_digests", bucketDate)
		cpuVal := baseCPU + int64(i)*cpuStep
		memVal := baseMem + int64(i)*memStep
		_, err := pool.Exec(ctx, `
			INSERT INTO daily_namespace_digests (
				bucket_date, org_id, cluster_uuid, namespace, schedule_type,
				cpu_request_p50_mc, cpu_request_p60_mc, cpu_request_p95_mc, cpu_request_p99_mc,
				cpu_usage_p50_mc, cpu_usage_p60_mc, cpu_usage_p95_mc, cpu_usage_p98_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
				memory_request_p50_kib, memory_request_p60_kib, memory_request_p95_kib,
				memory_usage_p50_kib, memory_usage_p60_kib, memory_usage_p95_kib, memory_usage_p98_kib, memory_usage_p99_kib, memory_usage_max_kib,
				cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
			) VALUES (
				$1, $2, $3, $4, $5::digest_schedule_type,
				$6, $7, $8, $9,
				$10, $11, $12, $13, $14, $15,
				$16, $17, $18,
				$19, $20, $21, $22, $23, $24,
				$25, $26, $27
			)
			ON CONFLICT (org_id, cluster_uuid, namespace, bucket_date, schedule_type)
			DO UPDATE SET cpu_usage_p95_mc = EXCLUDED.cpu_usage_p95_mc`,
			bucketDate, orgID, testutil.TestClusterUUID, namespace, scheduleType,
			cpuVal-20, cpuVal-10, cpuVal+10, cpuVal+20,
			cpuVal-10, cpuVal, cpuVal+10, cpuVal+15, cpuVal+18, cpuVal+25,
			memVal-1024, memVal-512, memVal+512,
			memVal-512, memVal, memVal+512, memVal+768, memVal+900, memVal+1024,
			cpuVal-5, memVal-256, int64(96),
		)
		require.NoError(t, err)
	}
}

func buildNativeFromRecs(recs []ContainerRec) []model.NativeContainerResult {
	if len(recs) == 0 {
		return nil
	}
	first := recs[0]
	result := model.NativeContainerResult{
		ID:              model.NativeContainerID(first.ClusterUUID, first.Namespace, first.Workload, first.WorkloadType, first.ContainerName),
		ClusterUUID:     first.ClusterUUID,
		Container:       first.ContainerName,
		Project:         first.Namespace,
		Workload:        first.Workload,
		WorkloadType:    first.WorkloadType,
		Recommendations: make(map[string]model.TermRecommendation),
	}
	for _, r := range recs {
		termKey := r.Term + "_term"
		term, ok := result.Recommendations[termKey]
		if !ok {
			term = model.TermRecommendation{}
		}
		cpu := r.RecCPURequestMC
		mem := r.RecMemRequestKiB
		eng := &model.EngineRecommendation{
			CPURequestMillicores: &cpu,
			MemRequestKiB:        &mem,
		}
		switch r.Engine {
		case "cost":
			term.Cost = eng
		case "performance":
			term.Performance = eng
		}
		result.Recommendations[termKey] = term
	}
	return []model.NativeContainerResult{result}
}

func recMap(recs []ContainerRec) map[string]ContainerRec {
	m := make(map[string]ContainerRec, len(recs))
	for _, r := range recs {
		m[r.Term+"/"+r.Engine] = r
	}
	return m
}
