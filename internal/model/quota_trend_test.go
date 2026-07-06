package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

const (
	resolveTestOrg       = "org-resolve-test"
	resolveTestCluster   = "550e8400-e29b-41d4-a716-446655440099"
	resolveTestNamespace = "perf01-ns"
	resolveTestQuotaName = "team-budget"
)

func TestResolveQuotaKeyByID_IndexedPath(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	quotaID := NativeQuotaID(resolveTestCluster, resolveTestNamespace, resolveTestQuotaName)

	_, err := pool.Exec(ctx, `
		INSERT INTO quota_recommendation_sets (
			org_id, cluster_uuid, namespace, quota_name, quota_id,
			recommendation_type, risk_level, last_observed_at
		) VALUES ($1, $2::uuid, $3, $4, $5, 'tighten', 'low', NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, quota_name) DO UPDATE SET
			quota_id = EXCLUDED.quota_id`,
		resolveTestOrg, resolveTestCluster, resolveTestNamespace, resolveTestQuotaName, quotaID,
	)
	require.NoError(t, err)

	key, err := ResolveQuotaKeyByID(ctx, pool, resolveTestOrg, quotaID)
	require.NoError(t, err)
	require.NotNil(t, key, "indexed path should find the row")
	assert.Equal(t, resolveTestCluster, key.ClusterUUID)
	assert.Equal(t, resolveTestNamespace, key.Namespace)
	assert.Equal(t, resolveTestQuotaName, key.QuotaName)
}

func TestResolveQuotaKeyByID_FallbackPath(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	fallbackNS := "fallback-ns"
	fallbackQuota := "legacy-quota"
	quotaID := NativeQuotaID(resolveTestCluster, fallbackNS, fallbackQuota)

	// Insert WITHOUT quota_id to exercise the NULL-fallback scan.
	_, err := pool.Exec(ctx, `
		INSERT INTO quota_recommendation_sets (
			org_id, cluster_uuid, namespace, quota_name,
			recommendation_type, risk_level, last_observed_at
		) VALUES ($1, $2::uuid, $3, $4, 'tighten', 'low', NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, quota_name) DO NOTHING`,
		resolveTestOrg, resolveTestCluster, fallbackNS, fallbackQuota,
	)
	require.NoError(t, err)

	key, err := ResolveQuotaKeyByID(ctx, pool, resolveTestOrg, quotaID)
	require.NoError(t, err)
	require.NotNil(t, key, "fallback path should find the row with NULL quota_id")
	assert.Equal(t, resolveTestCluster, key.ClusterUUID)
	assert.Equal(t, fallbackNS, key.Namespace)
	assert.Equal(t, fallbackQuota, key.QuotaName)
}

func TestResolveQuotaKeyByID_NotFound(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	fakeID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	key, err := ResolveQuotaKeyByID(ctx, pool, resolveTestOrg, fakeID)
	require.NoError(t, err)
	assert.Nil(t, key, "non-existent quota ID should return nil")
}

func TestResolveQuotaKeyByID_WrongOrg(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	quotaID := NativeQuotaID(resolveTestCluster, resolveTestNamespace, "")

	_, err := pool.Exec(ctx, `
		INSERT INTO quota_recommendation_sets (
			org_id, cluster_uuid, namespace, quota_name, quota_id,
			recommendation_type, risk_level, last_observed_at
		) VALUES ($1, $2::uuid, $3, '', $4, 'optimal', 'low', NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, quota_name) DO UPDATE SET
			quota_id = EXCLUDED.quota_id`,
		resolveTestOrg, resolveTestCluster, resolveTestNamespace, quotaID,
	)
	require.NoError(t, err)

	key, err := ResolveQuotaKeyByID(ctx, pool, "wrong-org", quotaID)
	require.NoError(t, err)
	assert.Nil(t, key, "different org should not resolve the quota")
}
