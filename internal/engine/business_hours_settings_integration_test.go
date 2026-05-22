package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestScheduleCRUD_Inheritance(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-crud-" + t.Name()
	cluster := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	require.NoError(t, UpsertBusinessHoursSchedule(ctx, pool, BusinessHoursSchedule{
		OrgID: orgID, ClusterUUID: OrgClusterSentinelUUID, Namespace: "",
		Timezone: "America/New_York", Days: []string{"monday"}, StartTime: "08:00", EndTime: "17:00",
		OffHoursWeight: 0.0, Enabled: true,
	}))
	require.NoError(t, UpsertBusinessHoursSchedule(ctx, pool, BusinessHoursSchedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "",
		Timezone: "America/Chicago", Days: []string{"tuesday"}, StartTime: "09:00", EndTime: "18:00",
		OffHoursWeight: 0.1, Enabled: true,
	}))
	require.NoError(t, UpsertBusinessHoursSchedule(ctx, pool, BusinessHoursSchedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "team-a",
		Timezone: "America/Los_Angeles", Days: []string{"wednesday"}, StartTime: "07:00", EndTime: "16:00",
		OffHoursWeight: 0.2, Enabled: true,
	}))

	cache, err := LoadSchedules(ctx, pool, orgID, cluster)
	require.NoError(t, err)

	// Namespaces without an override inherit the cluster row, not org.
	assert.Equal(t, "America/Chicago", cache.Resolve("other").Timezone)
	assert.Equal(t, "America/Chicago", cache.Resolve("team-b").Timezone)
	assert.Equal(t, "America/Los_Angeles", cache.Resolve("team-a").Timezone)
	assert.InDelta(t, 0.2, cache.Resolve("team-a").OffHoursWeight, 0.001)

	require.NoError(t, DeleteBusinessHoursSchedule(ctx, pool, orgID, cluster, "team-a"))
	cache2, err := LoadSchedules(ctx, pool, orgID, cluster)
	require.NoError(t, err)
	assert.Equal(t, "America/Chicago", cache2.Resolve("team-a").Timezone)
}
