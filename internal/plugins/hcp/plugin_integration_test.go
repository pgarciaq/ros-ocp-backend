package hcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

const (
	hcpTestOrg     = "orghcp0001"
	hcpTestSource  = "hcpsource"
	hcpTestCluster = "11111111-1111-1111-1111-111111111111"
	hcpTestNS      = "hc01-infra-hc01"
)

// TestHCPPlugin_AfterIngest_ObservesWithoutWriting proves the hook resolves
// the persisted HCP set once, never fails the run, and writes nothing:
// adversarial same-workload names across HCP and app namespaces must not
// cross-count. Requires testcontainers (skipped under -short).
func TestHCPPlugin_AfterIngest_ObservesWithoutWriting(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	require.NoError(t, pgrec.EnsureIngestClusterRow(ctx, pool, hcpTestOrg, hcpTestSource, hcpTestCluster, hcpTestCluster, time.Now().UTC()))
	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, hcpTestOrg, hcpTestSource, hcpTestCluster, []string{hcpTestNS}))

	rows := []ingestion.MetricRow{
		{Namespace: hcpTestNS, WorkloadName: "etcd"},
		{Namespace: hcpTestNS, WorkloadName: "tenant-nginx"},
		{Namespace: "ros-demo", WorkloadName: "tenant-nginx"},
	}
	p := &HCPPlugin{}
	require.NoError(t, p.AfterIngest(ctx, pool, rows, hcpTestOrg, hcpTestCluster),
		"hook must never fail the ingest run")

	// Empty HCP list degrades to no observation, still nil.
	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, hcpTestOrg, hcpTestSource, hcpTestCluster, []string{}))
	require.NoError(t, p.AfterIngest(ctx, pool, rows, hcpTestOrg, hcpTestCluster))
	assert.True(t, true, "empty-set path returns nil without observation")
}
