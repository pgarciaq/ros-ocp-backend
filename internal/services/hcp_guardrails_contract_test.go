package services

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
)

const (
	hcpContractOrg      = "org-hcp-guardrails"
	hcpContractSource   = "src-hcp-guardrails"
	hcpContractCluster  = "55555555-6666-7777-8888-999999999999"
	hcpContractManifest = "11111111-2222-3333-4444-555555555555"
	hcpNS               = "hc01-infra-hc01"
	hcpGenericNS        = "app-ns"
)

// seedHCPContractFixture seeds the steady-state clusters row plus 7 days of
// identical digests in both namespaces. The two namespaces get byte-identical
// request/usage rows (request 1000m / 524288KiB, usage one quarter) — an
// adversarial same-value collision: only the persisted HCP namespace list can
// discriminate the guardrail-floored rows from the generic ones.
func seedHCPContractAccounts(t *testing.T, pool *pgxpool.Pool, orgID string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (99001, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
}

func seedHCPContractClusterRow(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string) {
	t.Helper()
	ctx := context.Background()
	// source_id must match the message's for UpdateHCPNamespaces to land.
	_, err := pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at, analytics_incomplete)
		VALUES (99001, $1::uuid, 'hcp-guardrails-test', $2, NOW(), false)
		ON CONFLICT DO NOTHING`, clusterUUID, hcpContractSource)
	require.NoError(t, err)
}

func seedHCPContractFixture(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string) {
	t.Helper()
	seedHCPContractAccounts(t, pool, orgID)
	seedHCPContractClusterRow(t, pool, orgID, clusterUUID)
	seedHCPDigests(t, pool, orgID, clusterUUID, hcpNS)
	seedHCPDigests(t, pool, orgID, clusterUUID, hcpGenericNS)
}

func seedHCPDigests(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID, ns string) {
	t.Helper()
	start := testutil.RecentStart()
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:       start.AddDate(0, 0, i),
			OrgID:            orgID,
			ClusterUUID:      clusterUUID,
			Namespace:        ns,
			Workload:         "wl",
			WorkloadType:     "deployment",
			ContainerName:    "app",
			CPURequestP50MC:  1000,
			CPURequestP95MC:  1020,
			CPUUsageP50MC:    250,
			CPUUsageP60MC:    250,
			CPUUsageP95MC:    333,
			CPUUsageP98MC:    333,
			CPUUsageP99MC:    333,
			CPUUsageMaxMC:    500,
			CPUThrottleP95MC: 0,
			CPUThrottleMaxMC: 0,
			MemRequestP50KiB: 524288,
			MemRequestP60KiB: 524288,
			MemRequestP95KiB: 525312,
			MemRequestP98KiB: 525824,
			MemRequestP99KiB: 526336,
			MemUsageP50KiB:   262144,
			MemUsageP60KiB:   262144,
			MemUsageP95KiB:   349525,
			MemUsageP98KiB:   349525,
			MemUsageP99KiB:   349525,
			MemUsageMaxKiB:   524288,
			MemRSSP95KiB:     349525,
			MemRSSMaxKiB:     524288,
			CPUUsageMeanMC:   250,
			MemUsageMeanKiB:  262144,
			SampleCount:      96,
		})
	}
}

// TestHCPGuardrails_ManifestPersistAndDeferredFloors is the #590 contract
// test: manifest topology facts persisted by the W0 helper must (a) land on
// the clusters row (hcp_namespaces + classification) and (b) drive the HCP
// guardrail floors in a deferred run whose input message carries no topology —
// the engine must read the list from the database, not the message.
func TestHCPGuardrails_ManifestPersistAndDeferredFloors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	t.Setenv("ROS_INGEST_STRICT_ANALYTICS", "false")
	config.ResetForTest()
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	ctx := context.Background()
	orgID := hcpContractOrg
	clusterUUID := hcpContractCluster
	seedHCPContractFixture(t, pool, orgID, clusterUUID)

	kafkaMsg := types.KafkaMsg{}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = hcpContractSource
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "hcp-guardrails-test"
	kafkaMsg.Metadata.Manifest_id = hcpContractManifest
	kafkaMsg.Metadata.Topology = topology.TopologyFacts{
		ControlPlaneTopology:         "HighlyAvailable",
		HostedClusterCount:           1,
		HostedControlPlaneNamespaces: []string{hcpNS},
	}

	// W0 persist path (#580 + #590): facts land on the clusters row.
	persistTopologyFacts(ctx, pool, kafkaMsg)

	got, err := pgrec.ReadHCPNamespaces(ctx, pool, orgID, clusterUUID)
	require.NoError(t, err)
	assert.Equal(t, []string{hcpNS}, got, "manifest HCP namespaces must persist to the clusters row")

	topo, err := pgrec.ReadClusterTopology(ctx, pool, orgID, clusterUUID)
	require.NoError(t, err)
	assert.Equal(t, topology.TopologyManagement, topo, "highly-available + hosted evidence classifies management")

	// Deferred run: no topology in the message — the engine reads the
	// persisted list through the cfg loader.
	require.NoError(t, runContainerRecommendations(ctx, kafkaMsg), "deferred run must never fail")

	assertHCPGuardrailRecs(t, pool, orgID, clusterUUID)
}

type nsRec struct {
	ns     string
	term   string
	engine string
	cpuMC  int64
	memKiB int64
}

func assertHCPGuardrailRecs(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string) {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx, `
		SELECT namespace, term, engine, rec_cpu_request_millicores, rec_memory_request_kib
		FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2
		ORDER BY namespace, term, engine`, orgID, clusterUUID)
	require.NoError(t, err)
	defer rows.Close()
	var recs []nsRec
	for rows.Next() {
		var r nsRec
		require.NoError(t, rows.Scan(&r.ns, &r.term, &r.engine, &r.cpuMC, &r.memKiB))
		recs = append(recs, r)
	}
	require.NoError(t, rows.Err())

	byNS := map[string][]nsRec{}
	for _, r := range recs {
		byNS[r.ns] = append(byNS[r.ns], r)
	}
	require.Len(t, byNS[hcpNS], 6, "HCP namespace must get the full term×engine grid")
	require.Len(t, byNS[hcpGenericNS], 6, "generic namespace must get the full term×engine grid")

	// Guardrail floors derive from the window median request: 70% of
	// 1000m / 524288KiB → 700m / 367001KiB, the same fixture as the lib
	// #584 routing test. These are the mutation-discriminating assertions:
	// identical digests in both namespaces, only the persisted list routes.
	for _, r := range byNS[hcpNS] {
		assert.GreaterOrEqual(t, r.cpuMC, int64(700), "HCP ns %s/%s must take the guardrail CPU floor", r.term, r.engine)
		assert.GreaterOrEqual(t, r.memKiB, int64(367001), "HCP ns %s/%s must take the guardrail memory floor", r.term, r.engine)
	}

	// Floors only raise: identical HCP rows must recommend at least as much
	// as identical generic rows, term by term and engine by engine.
	generic := map[string]nsRec{}
	for _, r := range byNS[hcpGenericNS] {
		generic[r.term+"|"+r.engine] = r
	}
	for _, r := range byNS[hcpNS] {
		g := generic[r.term+"|"+r.engine]
		assert.GreaterOrEqual(t, r.cpuMC, g.cpuMC, "guardrail must not lower CPU vs generic (%s/%s)", r.term, r.engine)
		assert.GreaterOrEqual(t, r.memKiB, g.memKiB, "guardrail must not lower memory vs generic (%s/%s)", r.term, r.engine)
	}

	// Detect-not-exclude: the generic namespace stays below the CPU floor
	// (usage one quarter → generic recs ~300-500m). The memory floor is
	// intentionally NOT asserted — generic memory can exceed it (see plan).
	for _, r := range byNS[hcpGenericNS] {
		assert.Less(t, r.cpuMC, int64(700), "generic ns %s/%s must stay below the CPU floor (list-driven guardrail)", r.term, r.engine)
	}
}

// TestHCPGuardrails_NoFactsGenericNeverErrors: a manifest without topology
// facts must persist nothing and the deferred run must succeed with every
// namespace generic — including one literally named like an HCP control
// plane. The persisted list enables floors; the namespace name never does
// (detect-not-exclude).
func TestHCPGuardrails_NoFactsGenericNeverErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	t.Setenv("ROS_INGEST_STRICT_ANALYTICS", "false")
	config.ResetForTest()
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	ctx := context.Background()
	orgID := "org-hcp-nofacts"
	clusterUUID := "66666666-7777-8888-9999-aaaaaaaaaaaa"
	// Same steady-state seed as the persist test, including digests in hcpNS.
	seedHCPContractFixture(t, pool, orgID, clusterUUID)

	kafkaMsg := types.KafkaMsg{}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = hcpContractSource
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "hcp-guardrails-test"
	kafkaMsg.Metadata.Manifest_id = "22222222-3333-4444-5555-666666666666"
	// No Topology facts: reportTopologyFacts reports absent → persist no-op.

	persistTopologyFacts(ctx, pool, kafkaMsg)

	got, err := pgrec.ReadHCPNamespaces(ctx, pool, orgID, clusterUUID)
	require.NoError(t, err)
	assert.Empty(t, got, "facts-less manifest must persist no namespaces")
	topo, err := pgrec.ReadClusterTopology(ctx, pool, orgID, clusterUUID)
	require.NoError(t, err)
	assert.Equal(t, topology.TopologyUnknown, topo, "facts-less manifest must leave classification unknown")

	require.NoError(t, runContainerRecommendations(ctx, kafkaMsg), "run must never fail on absent facts")

	var minCPUMC int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT MIN(rec_cpu_request_millicores) FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3`,
		orgID, clusterUUID, hcpNS).Scan(&minCPUMC))
	assert.Less(t, minCPUMC, int64(700), "namespace name alone must not enable HCP floors")
}

// TestHCPGuardrails_FirstCyclePersistLandsWithoutSeededRow is the #612
// regression test: with no clusters row (source-sync hasn't run yet — the
// normal state for a brand-new cluster), the W0 persist must bootstrap the
// row so this same cycle's facts land and the deferred run takes guardrail
// floors. Pre-fix this fails naming the mechanism: persist no-ops on the
// missing row, the list reads empty, HCP rows land generic (333/444 < 700).
func TestHCPGuardrails_FirstCyclePersistLandsWithoutSeededRow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	t.Setenv("ROS_INGEST_STRICT_ANALYTICS", "false")
	config.ResetForTest()
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	ctx := context.Background()
	orgID := "org-hcp-firstcycle"
	clusterUUID := "77777777-8888-9999-aaaa-bbbbbbbbbbbb"
	// Digests only: no rh_accounts, no clusters row — source-sync lag.
	seedHCPDigests(t, pool, orgID, clusterUUID, hcpNS)
	seedHCPDigests(t, pool, orgID, clusterUUID, hcpGenericNS)

	kafkaMsg := types.KafkaMsg{}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = hcpContractSource
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "hcp-guardrails-test"
	kafkaMsg.Metadata.Manifest_id = "33333333-4444-5555-6666-777777777777"
	kafkaMsg.Metadata.Topology = topology.TopologyFacts{
		ControlPlaneTopology:         "HighlyAvailable",
		HostedClusterCount:           1,
		HostedControlPlaneNamespaces: []string{hcpNS},
	}

	persistTopologyFacts(ctx, pool, kafkaMsg)

	var rowCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM clusters WHERE cluster_uuid = $1`, clusterUUID).Scan(&rowCount))
	assert.Equal(t, 1, rowCount, "persist must bootstrap the missing row")

	got, err := pgrec.ReadHCPNamespaces(ctx, pool, orgID, clusterUUID)
	require.NoError(t, err)
	assert.Equal(t, []string{hcpNS}, got, "first-cycle facts must persist without a seeded row")

	require.NoError(t, runContainerRecommendations(ctx, kafkaMsg), "deferred run must never fail")

	assertHCPGuardrailRecs(t, pool, orgID, clusterUUID)
}

// TestHCPGuardrails_NoFactsCreatesNoRow pins the #612 conservation
// property: a facts-less message must not bootstrap anything — row
// creation happens only when there is state to persist.
func TestHCPGuardrails_NoFactsCreatesNoRow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	pool := testutil.SetupTestDB(t)

	ctx := context.Background()
	orgID := "org-hcp-norow"
	clusterUUID := "88888888-9999-aaaa-bbbb-cccccccccccc"
	seedHCPDigests(t, pool, orgID, clusterUUID, hcpNS)

	kafkaMsg := types.KafkaMsg{}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = hcpContractSource
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "hcp-guardrails-test"
	kafkaMsg.Metadata.Manifest_id = "44444444-5555-6666-7777-888888888888"
	// No Topology facts: persist is a no-op, including the row bootstrap.

	persistTopologyFacts(ctx, pool, kafkaMsg)

	var rowCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM clusters WHERE cluster_uuid = $1`, clusterUUID).Scan(&rowCount))
	assert.Equal(t, 0, rowCount, "facts-less messages must create no rows")
}
