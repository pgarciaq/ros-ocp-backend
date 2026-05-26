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

	_, err := pool.Exec(context.Background(), `
		INSERT INTO `+schema+`.reporting_enabledtagkeys (uuid, key, enabled, provider_type)
		VALUES ($1, 'environment', true, 'OCP'), ($2, 'app', true, 'OCP'), ($3, 'disabled-key', false, 'OCP')
		ON CONFLICT DO NOTHING`,
		uuid.New(), uuid.New(), uuid.New(),
	)
	require.NoError(t, err)

	_, err = pool.Exec(context.Background(), `
		INSERT INTO `+schema+`.reporting_ocptags_values (
			uuid, key, value, cluster_ids, cluster_aliases, namespaces
		) VALUES ($1, 'environment', 'production', ARRAY[$2], ARRAY['alias'], ARRAY['billing-ns'])
		ON CONFLICT DO NOTHING`,
		uuid.New(), testutil.TestClusterUUID,
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
