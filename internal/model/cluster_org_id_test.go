package model

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestCreateCluster_RejectsEmptyOrgID(t *testing.T) {
	c := Cluster{
		TenantID:     1,
		SourceId:     "src",
		ClusterUUID:  "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		ClusterAlias: "alias",
	}
	err := c.CreateCluster()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "org_id is required")
}

func TestCreateCluster_DualWritesOrgID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-create-cluster-a"
	clusterUUID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	acct := RHAccount{OrgId: orgID}
	require.NoError(t, acct.CreateRHAccount())
	require.NotZero(t, acct.ID)

	c := Cluster{
		TenantID:       acct.ID,
		OrgID:          orgID,
		SourceId:       "src-create-cluster-a",
		ClusterUUID:    clusterUUID,
		ClusterAlias:   "create-cluster-a",
		LastReportedAt: time.Now().UTC(),
	}
	require.NoError(t, c.CreateCluster())

	var got string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT org_id FROM clusters
		WHERE cluster_uuid = $1::uuid AND source_id = $2`, clusterUUID, c.SourceId).Scan(&got))
	assert.Equal(t, orgID, got)

	healed := orgID + "-healed"
	c.OrgID = healed
	c.LastReportedAt = time.Now().UTC()
	require.NoError(t, c.CreateCluster())
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT org_id FROM clusters
		WHERE cluster_uuid = $1::uuid AND source_id = $2`, clusterUUID, c.SourceId).Scan(&got))
	assert.Equal(t, healed, got, "OnConflict must assign org_id")
}
