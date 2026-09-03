package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/clustercache"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestListClustersForOrg_FiltersByOrgID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	sharedUUID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaa10"
	onlyB := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaa11"
	orgA := "org-dir-a"
	orgB := "org-dir-b"

	var tenantA, tenantB int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO rh_accounts (org_id) VALUES ($1) RETURNING id`, orgA).Scan(&tenantA))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO rh_accounts (org_id) VALUES ($1) RETURNING id`, orgB).Scan(&tenantB))
	_, err := pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2::uuid, 'alias-a', 'src-a', now()),
		       ($3, $2::uuid, 'alias-b', 'src-b', now()),
		       ($3, $4::uuid, 'alias-b2', 'src-b2', now())`,
		tenantA, sharedUUID, tenantB, onlyB)
	require.NoError(t, err)

	gotA, err := ListClustersForOrg(ctx, pool, orgA)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{sharedUUID}, gotA)

	gotB, err := ListClustersForOrg(ctx, pool, orgB)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{sharedUUID, onlyB}, gotB)

	cachedA, err := clustercache.GetClustersForOrgWithPool(ctx, pool, orgA)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{sharedUUID}, cachedA)
}
