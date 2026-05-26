package tags_test

import (
	"context"
	"testing"
	"time"

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
	updated, err := svc.SyncOrgTags(ctx, tags.SyncRequest{
		OrgID:    orgID,
		SyncedAt: time.Now().UTC().Format(time.RFC3339),
		TagKeys: []tags.TagKeyCatalog{
			{Key: "environment", Values: []string{"production"}},
			{Key: "team", Values: []string{"payments"}},
		},
		NamespaceTags: []tags.NamespaceTags{
			{
				ClusterUUID: clusterUUID,
				Namespace:   "payments",
				Tags: map[string]string{
					"environment": "production",
					"team":        "payments",
				},
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
	updated, err := svc.SyncOrgTags(ctx, tags.SyncRequest{
		OrgID: orgID,
		TagKeys: []tags.TagKeyCatalog{
			{Key: "environment", Values: []string{"production"}},
		},
		NamespaceTags: []tags.NamespaceTags{
			{
				ClusterUUID: clusterUUID,
				Namespace:   "payments",
				Tags:        map[string]string{"environment": "production"},
			},
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
	updated, err := svc.SyncOrgTags(ctx, tags.SyncRequest{
		OrgID: orgID,
		NamespaceTags: []tags.NamespaceTags{
			{
				ClusterUUID: clusterUUID,
				Namespace:   "payments",
				Tags:        map[string]string{"environment": "production"},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, updated)
}

func TestSyncOrgTags_EmptyTagKeyValues(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "tag-sync-empty-values-org"
	clusterUUID := testutil.TestClusterUUID
	syncedAt := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)

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
	_, err = svc.SyncOrgTags(ctx, tags.SyncRequest{
		OrgID:    orgID,
		SyncedAt: syncedAt.Format(time.RFC3339),
		TagKeys: []tags.TagKeyCatalog{
			{Key: "environment", Values: []string{}},
		},
		NamespaceTags: []tags.NamespaceTags{
			{
				ClusterUUID: clusterUUID,
				Namespace:   "payments",
				Tags:        map[string]string{},
			},
		},
	})
	require.NoError(t, err)

	status, err := svc.GetSyncStatus(ctx, orgID)
	require.NoError(t, err)
	require.NotNil(t, status.SyncedAt)
	assert.Equal(t, syncedAt, status.SyncedAt.UTC())
	require.Len(t, status.TagKeys, 1)
	assert.Equal(t, "environment", status.TagKeys[0].Key)
	assert.Empty(t, status.TagKeys[0].Values)

	var tagsJSON string
	err = pool.QueryRow(ctx, `
		SELECT resolved_tags::text FROM org_container_keys
		WHERE org_id = $1 AND namespace = 'payments'`, orgID).Scan(&tagsJSON)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, tagsJSON)
}

func TestSyncOrgTags_RemovedTagKeyClearsMetadataCatalog(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "tag-sync-key-removal-org"
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
	_, err = svc.SyncOrgTags(ctx, tags.SyncRequest{
		OrgID: orgID,
		TagKeys: []tags.TagKeyCatalog{
			{Key: "environment", Values: []string{"production"}},
			{Key: "team", Values: []string{"payments"}},
		},
		NamespaceTags: []tags.NamespaceTags{
			{
				ClusterUUID: clusterUUID,
				Namespace:   "payments",
				Tags: map[string]string{
					"environment": "production",
					"team":        "payments",
				},
			},
		},
	})
	require.NoError(t, err)

	_, err = svc.SyncOrgTags(ctx, tags.SyncRequest{
		OrgID: orgID,
		TagKeys: []tags.TagKeyCatalog{
			{Key: "environment", Values: []string{"production"}},
		},
		NamespaceTags: []tags.NamespaceTags{
			{
				ClusterUUID: clusterUUID,
				Namespace:   "payments",
				Tags:        map[string]string{"environment": "production"},
			},
		},
	})
	require.NoError(t, err)

	status, err := svc.GetSyncStatus(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, status.TagKeys, 1)
	assert.Equal(t, "environment", status.TagKeys[0].Key)

	var tagsJSON string
	err = pool.QueryRow(ctx, `
		SELECT resolved_tags::text FROM org_container_keys
		WHERE org_id = $1 AND namespace = 'payments'`, orgID).Scan(&tagsJSON)
	require.NoError(t, err)
	assert.JSONEq(t, `{"environment":"production"}`, tagsJSON)
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
	byKey := make(map[string][]string, len(merged))
	for _, f := range merged {
		byKey[f.Key] = f.Values
	}
	assert.Equal(t, []string{"production", "staging"}, byKey["environment"])
	assert.Equal(t, []string{"payments"}, byKey["team"])
}
