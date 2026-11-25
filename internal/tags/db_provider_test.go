package tags

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupKokuTagSchema(t *testing.T, pool *pgxpool.Pool, orgID string) string {
	t.Helper()
	schema, err := TenantSchema(orgID)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		CREATE SCHEMA IF NOT EXISTS `+schema+`;
		CREATE TABLE IF NOT EXISTS `+schema+`.reporting_enabledtagkeys (
			uuid UUID PRIMARY KEY,
			key VARCHAR(512) NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT true,
			provider_type VARCHAR(50) NOT NULL,
			UNIQUE (key, provider_type)
		);
		CREATE TABLE IF NOT EXISTS `+schema+`.reporting_ocptags_values (
			uuid UUID PRIMARY KEY,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			cluster_ids TEXT[] NOT NULL,
			cluster_aliases TEXT[] NOT NULL DEFAULT '{}',
			namespaces TEXT[] NOT NULL,
			nodes TEXT[],
			UNIQUE (key, value)
		);
	`)
	require.NoError(t, err)
	return schema
}

func TestDBTagProvider_GetEnabledTagKeysAndValues(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	schema := setupKokuTagSchema(t, pool, testutil.TestOrgID)
	setupLabelSummaryTables(t, pool, schema)

	_, err := pool.Exec(context.Background(), `
		INSERT INTO `+schema+`.reporting_enabledtagkeys (uuid, key, enabled, provider_type)
		VALUES ($1, 'environment', true, 'OCP'), ($2, 'app', true, 'OCP'), ($3, 'disabled-key', false, 'OCP')
		ON CONFLICT DO NOTHING`,
		uuid.New(), uuid.New(), uuid.New(),
	)
	require.NoError(t, err)

	// #868: GetTagValues reads the label summaries — seed the pod summary
	// (with its report period in the month-snapped window).
	winStart, _ := LabelSummaryWindow()
	_, err = pool.Exec(context.Background(), `
		INSERT INTO `+schema+`.reporting_ocpusagereportperiod (id, cluster_id, report_period_start)
		VALUES (1, $1, $2) ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID, winStart.AddDate(0, 0, 15).Format("2006-01-02"),
	)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO `+schema+`.reporting_ocpusagepodlabel_summary (uuid, key, values, report_period_id, namespace, node)
		VALUES ($1, 'environment', ARRAY['production'], 1, 'billing-ns', 'node-a')
		ON CONFLICT DO NOTHING`,
		uuid.New(),
	)
	require.NoError(t, err)

	provider := NewDBTagProvider(pool)
	keys, err := provider.GetEnabledTagKeys(context.Background(), testutil.TestOrgID)
	require.NoError(t, err)
	assert.Equal(t, []string{"app", "environment"}, keys)

	values, err := provider.GetTagValues(context.Background(), testutil.TestOrgID, "environment")
	require.NoError(t, err)
	assert.Equal(t, []string{"production"}, values)
}

func TestNewProviderFromConfig_SelectsSource(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	config.ResetTagsForTest()
	ResetProviderForTest()
	t.Setenv("ROS_TAGS_SOURCE", "api")
	provider := NewProviderFromConfig(pool)
	_, ok := provider.(*APITagProvider)
	assert.True(t, ok)

	config.ResetTagsForTest()
	ResetProviderForTest()
	t.Setenv("ROS_TAGS_SOURCE", "db")
	provider = NewProviderFromConfig(pool)
	_, ok = provider.(*DBTagProvider)
	assert.True(t, ok)
}

// setupLabelSummaryTables creates the #815 predicate's source tables for the
// #868 regression: the pod and storage label summaries joined through the
// report period.
func setupLabelSummaryTables(t *testing.T, pool *pgxpool.Pool, schema string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS `+schema+`.reporting_ocpusagereportperiod (
			id BIGINT PRIMARY KEY,
			cluster_id TEXT NOT NULL,
			report_period_start DATE NOT NULL
		);
		CREATE TABLE IF NOT EXISTS `+schema+`.reporting_ocpusagepodlabel_summary (
			uuid UUID PRIMARY KEY,
			key TEXT NOT NULL,
			values TEXT[] NOT NULL,
			report_period_id BIGINT NOT NULL REFERENCES `+schema+`.reporting_ocpusagereportperiod (id),
			namespace TEXT NOT NULL,
			node TEXT
		);
		CREATE TABLE IF NOT EXISTS `+schema+`.reporting_ocpstoragevolumelabel_summary (
			uuid UUID PRIMARY KEY,
			key TEXT NOT NULL,
			values TEXT[] NOT NULL,
			report_period_id BIGINT NOT NULL REFERENCES `+schema+`.reporting_ocpusagereportperiod (id),
			namespace TEXT NOT NULL,
			node TEXT
		);
	`)
	require.NoError(t, err)
}

// TestDBTagProvider_GetTagValuesReadsLabelSummaries is #868's regression in
// the shape of #815's executed tests: two clusters sharing a key — the
// values from BOTH must be present. reporting_ocptags_values (the old
// source) holds only the last-summarized cluster's value for any shared
// key, and is deliberately seeded with just one side to prove the new
// source. An out-of-window period's value must not appear.
func TestDBTagProvider_GetTagValuesReadsLabelSummaries(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	schema := setupKokuTagSchema(t, pool, testutil.TestOrgID)
	setupLabelSummaryTables(t, pool, schema)

	winStart, _ := LabelSummaryWindow()
	inWindow := winStart.AddDate(0, 0, 15)
	outOfWindow := winStart.AddDate(0, -2, 0)

	_, err := pool.Exec(context.Background(), `
		INSERT INTO `+schema+`.reporting_ocpusagereportperiod (id, cluster_id, report_period_start)
		VALUES (1, $1, $2), (2, $3, $4), (3, $3, $5)
		ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID, inWindow.Format("2006-01-02"),
		"11111111-2222-3333-4444-555555555555", inWindow.Format("2006-01-02"),
		outOfWindow.Format("2006-01-02"),
	)
	require.NoError(t, err)

	// Cluster A's pod summary carries the shared key with its value; cluster
	// B carries the same key with a different value (the #815 shape) plus an
	// out-of-window value.
	_, err = pool.Exec(context.Background(), `
		INSERT INTO `+schema+`.reporting_ocpusagepodlabel_summary (uuid, key, values, report_period_id, namespace, node)
		VALUES ($1, 'environment', ARRAY['production'], 1, 'ns-a', 'node-a'),
		       ($2, 'environment', ARRAY['staging'], 2, 'ns-b', 'node-b'),
		       ($3, 'environment', ARRAY['ancient'], 3, 'ns-b', 'node-b'),
		       ($4, 'app', ARRAY['payments'], 1, 'ns-a', 'node-a')
		ON CONFLICT DO NOTHING`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(),
	)
	require.NoError(t, err)

	// The storage summary is a second source: a key only present there.
	_, err = pool.Exec(context.Background(), `
		INSERT INTO `+schema+`.reporting_ocpstoragevolumelabel_summary (uuid, key, values, report_period_id, namespace, node)
		VALUES ($1, 'storageclass', ARRAY['gp2', 'gp3'], 1, 'ns-a', NULL)
		ON CONFLICT DO NOTHING`,
		uuid.New(),
	)
	require.NoError(t, err)

	// The OLD source deliberately holds only cluster A's value: the
	// last-summarized-cluster shape (#815's mechanism).
	_, err = pool.Exec(context.Background(), `
		INSERT INTO `+schema+`.reporting_ocptags_values (uuid, key, value, cluster_ids, cluster_aliases, namespaces)
		VALUES ($1, 'environment', 'production', ARRAY[$2], ARRAY['alias'], ARRAY['ns-a'])
		ON CONFLICT DO NOTHING`,
		uuid.New(), testutil.TestClusterUUID,
	)
	require.NoError(t, err)

	provider := NewDBTagProvider(pool)
	values, err := provider.GetTagValues(context.Background(), testutil.TestOrgID, "environment")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"production", "staging"}, values,
		"#868: values from BOTH clusters sharing the key must be present (the label summaries), not just the last-summarized cluster's (reporting_ocptags_values); the out-of-window 'ancient' must not appear")

	storageValues, err := provider.GetTagValues(context.Background(), testutil.TestOrgID, "storageclass")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"gp2", "gp3"}, storageValues,
		"#868: the storage label summary is a values source too")
}
