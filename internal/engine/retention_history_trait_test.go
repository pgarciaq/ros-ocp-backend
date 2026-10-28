package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// historyDeclPlugin is a test HistoryRetentionProvider declaring a scratch
// table, used to pin the #701 contract: a plugin-declared history table is
// swept by the core history sweep at the HistoryRetentionDays cutoff —
// never the digest month cutoff — by construction.
type historyDeclPlugin struct {
	plugin.BasePlugin
	table string
}

func (p *historyDeclPlugin) Name() string  { return "history-decl-test" }
func (p *historyDeclPlugin) Enabled() bool { return true }
func (p *historyDeclPlugin) Priority() int { return 0 }
func (p *historyDeclPlugin) HistoryRetentionTables() []string {
	return []string{p.table}
}

// TestHistoryRetention_PluginDeclarationUsesDaysCutoff (#701 Review Focus 1):
// with NON-DEFAULT config values (retentionMonths=3, historyDays=45 — the
// test pins which CONFIG feeds which class, not the default values), a
// plugin-declared history table drops its 2-month-old partition (outside
// 45 days, inside 3 months) — the months cutoff would keep it.
func TestHistoryRetention_PluginDeclarationUsesDaysCutoff(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	table := "history_decl_test_tbl"
	declPlugin := &historyDeclPlugin{table: table}
	plugin.RegisterForTest(t, declPlugin) // added by Task 1

	// The scratch parent, partitioned like the real history tables.
	_, err := pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (observed_at timestamptz NOT NULL) PARTITION BY RANGE (observed_at)`, table))
	require.NoError(t, err)

	// Two months old: outside historyDays=45, inside retentionMonths=3.
	old := time.Now().UTC().AddDate(0, -2, 0)
	monthStart := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("%s_%s", table, monthStart.Format("200601"))
	_, err = pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')`,
		partName, table, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02")))
	require.NoError(t, err)

	t.Setenv("ROS_HISTORY_RETENTION_DAYS", "45")
	config.ResetForTest()
	require.NoError(t, RunRetentionSweep(ctx, pool, 3))

	partitions, err := listPartitions(ctx, pool, table)
	require.NoError(t, err)
	for _, p := range partitions {
		assert.NotEqual(t, partName, p,
			"plugin-declared history table must sweep at the days-config cutoff (45d), not the months cutoff (3mo)")
	}
}

// TestHistoryRetention_ZeroPluginFallbackSweepsAllCoreTables (#701 Review
// Focus 2): the core historyRetainedTables list is the plugin-less-binary
// fallback and sweeps unconditionally (the code path is outside the
// len(retProviders) branch). Its contents are pinned so neither a fallback
// removal nor a silent addition can drift. (This package's test binary
// registers production plugins via an integration test's blank import —
// the unconditional core-list sweep is what guarantees plugin-less
// binaries, and that is structural, not registration-dependent.)
func TestHistoryRetention_ZeroPluginFallbackSweepsAllCoreTables(t *testing.T) {
	assert.ElementsMatch(t, []string{
		"recommendation_history",
		"recommendation_quality",
		"pvc_recommendation_quality",
		"vm_recommendation_quality",
		"gpu_mig_recommendation_quality",
		"snapshot_recommendation_quality",
		"hosted_api_bucket_rollups",
		"hosted_worker_pressure",
		"hosted_api_tax_rollups",
		"hosted_nodepool_rollups",
		"hcp_capacity_history",
	}, historyRetainedTables,
		"the core fallback list keeps every entry — plugin declarations ADD to it (overlap harmless), never replace it")
}

// TestHistoryRetention_HcpCapacityHistoryUsesDaysConfig (#701 Review Focus
// 4, the W4 trap fixture): the hcp plugin's declaration sweeps at the
// configured history cutoff under non-default values — the W4 near-miss
// (6-month retention via the month-cutoff trait), now impossible by
// construction.
func TestHistoryRetention_HcpCapacityHistoryUsesDaysConfig(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	old := time.Now().UTC().AddDate(0, -2, 0)
	monthStart := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("hcp_capacity_history_%s", monthStart.Format("200601"))
	_, err := pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF hcp_capacity_history FOR VALUES FROM ('%s') TO ('%s')`,
		partName, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02")))
	require.NoError(t, err)

	// The hcp plugin registers via the integration test's blank import of
	// internal/plugins in this package's test binary; its declaration must
	// ride the days config.
	t.Setenv("ROS_HISTORY_RETENTION_DAYS", "45")
	config.ResetForTest()
	require.NoError(t, RunRetentionSweep(ctx, pool, 3))

	partitions, err := listPartitions(ctx, pool, "hcp_capacity_history")
	require.NoError(t, err)
	for _, p := range partitions {
		assert.NotEqual(t, partName, p,
			"hcp_capacity_history sweeps at the days-config cutoff (45d) whether core-listed or plugin-declared")
	}
}

// TestHistoryRetention_NoCrossClassDrift (#701 Review Focus 3): digest
// tables and history tables are disjoint sets in both paths, and the two
// cutoffs derive from their respective configs — no table can be swept at
// the wrong class's cutoff.
func TestHistoryRetention_NoCrossClassDrift(t *testing.T) {
	assert.Empty(t, intersection(retainedTables, historyRetainedTables),
		"a table must never appear in both the digest-class and history-class core lists")
	for _, hp := range plugin.ByTrait[plugin.HistoryRetentionProvider]() {
		for _, table := range hp.HistoryRetentionTables() {
			assert.NotContains(t, retainedTables, table,
				"%s: a history-class declaration must not also sit in the digest-class list", hp.Name())
		}
	}
}

func intersection(a, b []string) []string {
	var out []string
	for _, x := range a {
		for _, y := range b {
			if x == y {
				out = append(out, x)
			}
		}
	}
	return out
}
