package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestResolveSchedule_OrgDefaultOnly(t *testing.T) {
	cache := bhschedule.NewCacheForTest(ptrSchedule(BusinessHoursSchedule{
			ClusterUUID: OrgClusterSentinelUUID,
			Namespace:   "",
			Timezone:    "America/New_York",
			Days:        []string{"monday"},
			StartTime:   "08:00",
			EndTime:     "17:00",
			Enabled:     true,
		}), nil, nil)

	got := cache.Resolve("any-ns")
	assert.Equal(t, "America/New_York", got.Timezone)
	assert.True(t, got.Enabled)
}

func TestResolveSchedule_ClusterOverridesOrg(t *testing.T) {
	cache := bhschedule.NewCacheForTest(
		ptrSchedule(BusinessHoursSchedule{
			Timezone:  "America/New_York",
			StartTime: "08:00",
			EndTime:   "17:00",
			Enabled:   true,
		}),
		ptrSchedule(BusinessHoursSchedule{
			Timezone:  "America/Chicago",
			StartTime: "09:00",
			EndTime:   "18:00",
			Enabled:   true,
		}),
		nil,
	)

	got := cache.Resolve("team-a")
	assert.Equal(t, "America/Chicago", got.Timezone)
	assert.Equal(t, "09:00", got.StartTime)
}

func TestResolveSchedule_NamespaceOverridesCluster(t *testing.T) {
	cache := bhschedule.NewCacheForTest(
		nil,
		ptrSchedule(BusinessHoursSchedule{
			Timezone:  "America/Chicago",
			StartTime: "09:00",
			Enabled:   true,
		}),
		map[string]BusinessHoursSchedule{
			"team-a": {
				Timezone:  "America/Los_Angeles",
				StartTime: "07:00",
				EndTime:   "16:00",
				Enabled:   true,
			},
		},
	)

	got := cache.Resolve("team-a")
	assert.Equal(t, "America/Los_Angeles", got.Timezone)
	assert.Equal(t, "07:00", got.StartTime)
}

func TestResolveSchedule_NoRows_AllHoursOnly(t *testing.T) {
	cache := bhschedule.NewCacheForTest(nil, nil, nil)
	got := cache.Resolve("team-a")
	assert.False(t, got.Enabled)
}

func TestResolveSchedule_NamespaceDisabled(t *testing.T) {
	cache := bhschedule.NewCacheForTest(
		ptrSchedule(BusinessHoursSchedule{Enabled: true}),
		nil,
		map[string]BusinessHoursSchedule{
			"team-a": {Enabled: false},
		},
	)

	got := cache.Resolve("team-a")
	assert.False(t, got.Enabled)
}

func TestLoadSchedules_CacheSingleQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-cache-" + t.Name()
	cluster := testutil.TestClusterUUID

	require.NoError(t, UpsertBusinessHoursSchedule(ctx, pool, BusinessHoursSchedule{
		OrgID: orgID, ClusterUUID: OrgClusterSentinelUUID, Namespace: "",
		Timezone: "UTC", Days: []string{"monday"}, StartTime: "08:00", EndTime: "17:00",
		Enabled: true,
	}))
	require.NoError(t, UpsertBusinessHoursSchedule(ctx, pool, BusinessHoursSchedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "ns-a",
		Timezone: "UTC", Days: []string{"tuesday"}, StartTime: "09:00", EndTime: "18:00",
		Enabled: true,
	}))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	cache, err := LoadSchedules(ctx, pool, orgID, cluster)
	require.NoError(t, err)

	assert.Equal(t, "monday", cache.Resolve("other-ns").Days[0])
	assert.Equal(t, "tuesday", cache.Resolve("ns-a").Days[0])

	// Second resolve is in-memory (no additional LoadSchedules call needed).
	for i := 0; i < 100; i++ {
		_ = cache.Resolve("ns-a")
	}
}

func TestResolveSchedule_OrgRowSentinelNulls(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-sentinel-" + t.Name()
	cluster := testutil.TestClusterUUID

	require.NoError(t, UpsertBusinessHoursSchedule(ctx, pool, BusinessHoursSchedule{
		OrgID: orgID, ClusterUUID: OrgClusterSentinelUUID, Namespace: "",
		Timezone: "America/New_York", Days: []string{"monday"}, StartTime: "08:00", EndTime: "17:00",
		Enabled: true,
	}))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	cache, err := LoadSchedules(ctx, pool, orgID, cluster)
	require.NoError(t, err)
	got := cache.Resolve("any-ns")
	assert.True(t, got.Enabled)
	assert.Equal(t, OrgClusterSentinelUUID, got.ClusterUUID)
}

func ptrSchedule(s BusinessHoursSchedule) *BusinessHoursSchedule {
	return &s
}
