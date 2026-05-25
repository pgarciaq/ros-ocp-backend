package tags_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestSyncOrgTags_UpdatesMatchingRows(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "tag-sync-test-org"
	clusterUUID := testutil.TestClusterUUID

	_, err := pool.Exec(ctx, `
		INSERT INTO org_container_keys (
			org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags
		) VALUES ($1, $2, 'payments', 'api-server', 'Deployment', 'api', '{}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name) DO UPDATE
		SET cluster_uuid = EXCLUDED.cluster_uuid`,
		orgID, clusterUUID,
	)
	require.NoError(t, err)

	svc := tags.NewSyncService(pool)
	updated, err := svc.SyncOrgTags(ctx, orgID, []tags.NamespaceTags{
		{
			ClusterUUID: clusterUUID,
			Namespace:   "payments",
			Tags: map[string]string{
				"environment": "production",
				"team":        "payments",
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, updated)

	var tagsJSON string
	err = pool.QueryRow(ctx, `
		SELECT resolved_tags::text FROM org_container_keys
		WHERE org_id = $1 AND namespace = $2 AND workload = $3 AND container_name = $4`,
		orgID, "payments", "api-server", "api",
	).Scan(&tagsJSON)
	require.NoError(t, err)
	assert.JSONEq(t, `{"environment":"production","team":"payments"}`, tagsJSON)
}

func TestSyncOrgTags_FullReplaceClearsRemovedTags(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "tag-sync-replace-org"
	clusterUUID := testutil.TestClusterUUID

	_, err := pool.Exec(ctx, `
		INSERT INTO org_container_keys (
			org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags
		) VALUES
			($1, $2, 'payments', 'api-server', 'Deployment', 'api', '{"environment":"production","team":"payments"}'::jsonb),
			($1, $2, 'billing', 'worker', 'Deployment', 'worker', '{"environment":"staging"}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name) DO UPDATE
		SET cluster_uuid = EXCLUDED.cluster_uuid,
		    resolved_tags = EXCLUDED.resolved_tags`,
		orgID, clusterUUID,
	)
	require.NoError(t, err)

	svc := tags.NewSyncService(pool)
	updated, err := svc.SyncOrgTags(ctx, orgID, []tags.NamespaceTags{
		{
			ClusterUUID: clusterUUID,
			Namespace:   "payments",
			Tags:        map[string]string{"environment": "production"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, updated)

	var billingTags string
	err = pool.QueryRow(ctx, `
		SELECT resolved_tags::text FROM org_container_keys
		WHERE org_id = $1 AND namespace = 'billing'`, orgID).Scan(&billingTags)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, billingTags)
}

func TestSyncOrgTags_AppliesNamespaceTagsToAllContainers(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "tag-sync-namespace-org"
	clusterUUID := testutil.TestClusterUUID

	_, err := pool.Exec(ctx, `
		INSERT INTO org_container_keys (
			org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags
		) VALUES
			($1, $2, 'payments', 'api-server', 'Deployment', 'api', '{}'::jsonb),
			($1, $2, 'payments', 'api-server', 'Deployment', 'sidecar', '{}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name) DO UPDATE
		SET cluster_uuid = EXCLUDED.cluster_uuid`,
		orgID, clusterUUID,
	)
	require.NoError(t, err)

	svc := tags.NewSyncService(pool)
	updated, err := svc.SyncOrgTags(ctx, orgID, []tags.NamespaceTags{
		{
			ClusterUUID: clusterUUID,
			Namespace:   "payments",
			Tags:        map[string]string{"environment": "production"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, updated)
}

func TestParseTagFilters(t *testing.T) {
	t.Parallel()

	filters, err := model.ParseTagFilters([]string{"environment:production", "team:*"})
	require.NoError(t, err)
	require.Len(t, filters, 2)
	assert.Equal(t, "environment", filters[0].Key)
	assert.Equal(t, []string{"production"}, filters[0].Values)
	assert.Equal(t, "team", filters[1].Key)
	assert.Equal(t, []string{"*"}, filters[1].Values)

	_, err = model.ParseTagFilters([]string{"invalid"})
	require.Error(t, err)
}

func TestParseKokuTagFilterParams(t *testing.T) {
	t.Parallel()

	filters, err := model.ParseKokuTagFilterParams(map[string][]string{
		"filter[tag:environment]": {"production,staging"},
		"filter[tag:team]":        {"payments"},
	})
	require.NoError(t, err)
	require.Len(t, filters, 2)

	merged := model.MergeTagFilters(filters)
	require.Len(t, merged, 2)
	assert.Equal(t, []string{"production", "staging"}, merged[0].Values)
}
