package api_test

import (
	"testing"
	"time"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeModeContainerAliasUsesUpdatedAtDateWindow(t *testing.T) {
	app, identityHeader, ctx := setupContractTestApp(t)
	pool := database.Pool
	require.NotNil(t, pool)

	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	previousMonth := monthStart.Add(-time.Second)

	_, err := pool.Exec(ctx, `
		UPDATE recommendation_sets
		SET monitoring_end_time = $1, updated_at = $2
		WHERE org_id = $3 AND cluster_uuid = $4 AND container_name = $5`,
		previousMonth, now, testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestContainer)
	require.NoError(t, err)

	compatCode, compat := contractGET(t, app, identityHeader,
		"/api/cost-management/v1/recommendations/openshift/container?limit=5")
	require.Equal(t, 200, compatCode)
	compatData, _ := compat["data"].([]interface{})

	canonicalCode, canonical := contractGET(t, app, identityHeader,
		"/api/cost-management/v1/recommendations/openshift?limit=5")
	require.Equal(t, 200, canonicalCode)
	canonicalData, _ := canonical["data"].([]interface{})
	// The current-month native query must return the fixture: its updated_at is this month.
	require.NotEmpty(t, canonicalData, "canonical native route should include current-month updated rows")
	// The legacy alias must use the documented current-month updated_at window, not drop it on prior-month monitoring_end_time.
	assert.NotEmpty(t, compatData, "native-mode /container alias filtered on prior-month monitoring_end_time")
	if len(compatData) > 0 {
		compatRow, _ := compatData[0].(map[string]interface{})
		canonicalRow, _ := canonicalData[0].(map[string]interface{})
		// Both list routes must identify the same seeded container.
		assert.Equal(t, canonicalRow["id"], compatRow["id"])
		// The compatibility response keeps its legacy empty source_id field.
		assert.Equal(t, "", compatRow["source_id"])
	}

	currentMonthMonitoringTime := monthStart.Add(time.Hour)
	_, err = pool.Exec(ctx, `
		UPDATE recommendation_sets
		SET monitoring_end_time = $1, updated_at = $2
		WHERE org_id = $3 AND cluster_uuid = $4 AND container_name = $5`,
		currentMonthMonitoringTime, previousMonth, testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestContainer)
	require.NoError(t, err)

	compatCode, compat = contractGET(t, app, identityHeader,
		"/api/cost-management/v1/recommendations/openshift/container?limit=5&end_date="+monthStart.Format("2006-01-02"))
	require.Equal(t, 200, compatCode)
	compatData, _ = compat["data"].([]interface{})
	// Explicit end_date includes the first day so a future one-hour fixture timestamp cannot mask the updated_at predicate.
	// A current monitoring window must not admit a row whose updated_at is before this month.
	assert.Empty(t, compatData, "native-mode /container alias ignored the updated_at start bound")
}

func TestNativeModeContainerAliasDoesNotReadAnotherOrgClusterAlias(t *testing.T) {
	app, identityHeader, ctx := setupContractTestApp(t)
	pool := database.Pool
	require.NotNil(t, pool)

	const otherOrg = "org-compat-alias-other"
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES ($1, $2)`, 2, otherOrg)
	require.NoError(t, err)
	insertOtherOrgAlias := func() {
		t.Helper()
		_, err := pool.Exec(ctx, `
			INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, org_id, last_reported_at)
			VALUES ($1, $2, $3, $4, $5, now())`,
			2, testutil.TestClusterUUID, "other-org-alias", "other-org-source", otherOrg)
		require.NoError(t, err)
	}
	queryAlias := func() string {
		t.Helper()
		startDate := time.Now().UTC().AddDate(0, 0, -10).Format("2006-01-02")
		endDate := time.Now().UTC().Format("2006-01-02")
		code, body := contractGET(t, app, identityHeader,
			"/api/cost-management/v1/recommendations/openshift/container?limit=10&start_date="+
				startDate+"&end_date="+endDate+"&filter[container]="+testutil.TestContainer)
		require.Equal(t, 200, code)
		data, _ := body["data"].([]interface{})
		require.NotEmpty(t, data)
		row, _ := data[0].(map[string]interface{})
		// The compatibility response remains linkage-free when both orgs have cluster metadata.
		assert.Equal(t, "", row["source_id"])
		return row["cluster_alias"].(string)
	}

	// Both orgs have the same UUID and different aliases. The requesting org's alias must win.
	insertOtherOrgAlias()
	assert.Equal(t, "contract-cluster", queryAlias(), "local alias must win when foreign row is inserted second")

	deleteResult, err := pool.Exec(ctx,
		`DELETE FROM clusters WHERE cluster_uuid = $1 AND org_id = ANY($2)`,
		testutil.TestClusterUUID, []string{testutil.TestOrgID, otherOrg})
	require.NoError(t, err)
	// Recreate both matching UUID rows in reverse insertion order to avoid a single-order fixture.
	require.Equal(t, int64(2), deleteResult.RowsAffected())
	insertOtherOrgAlias()
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, org_id, last_reported_at)
		VALUES ($1, $2, $3, $4, $5, now())`,
		1, testutil.TestClusterUUID, "contract-cluster", "src-1", testutil.TestOrgID)
	require.NoError(t, err)
	assert.Equal(t, "contract-cluster", queryAlias(), "local alias must win when foreign row is inserted first")
}
