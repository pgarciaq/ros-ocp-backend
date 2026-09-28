package hcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

func TestHCPPlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var (
		_ plugin.Plugin       = (*HCPPlugin)(nil)
		_ plugin.IngestHook   = (*HCPPlugin)(nil)
		_ plugin.TermProvider = (*HCPPlugin)(nil)
	)

	// #630 ships no routes, no CSV ownership, no owned tables: these traits
	// must stay unimplemented until #626 (surface) says otherwise.
	p := &HCPPlugin{}
	_, ok := any(p).(plugin.APIProvider)
	assert.False(t, ok, "hcp must not implement APIProvider before #626")
	_, ok = any(p).(plugin.CSVIngestor)
	assert.False(t, ok, "hcp must not claim CSV types (hook only)")
	_, ok = any(p).(plugin.RetentionProvider)
	assert.False(t, ok, "hcp owns no tables (reuses container digests)")
}

func TestHCPPlugin_identity(t *testing.T) {
	t.Parallel()

	p := &HCPPlugin{}
	assert.Equal(t, "hcp", p.Name())
	assert.Equal(t, []string{"container"}, p.Requires())
	assert.Equal(t, []string{"container"}, p.HookAfterCSVTypes())
	assert.Equal(t, 90, p.MaxWindowDays())

	terms := p.DefaultTerms()
	require.Len(t, terms, 3)
	assert.Equal(t, "short", terms[0].Name)
	assert.Equal(t, 1, terms[0].WindowDays)
	assert.Equal(t, "medium", terms[1].Name)
	assert.Equal(t, 7, terms[1].WindowDays)
	assert.Equal(t, "long", terms[2].Name)
	assert.Equal(t, 15, terms[2].WindowDays)
}

func TestHCPPlugin_enabledByDefault(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	config.ResetForTest()
	_ = config.GetConfig()

	assert.True(t, (&HCPPlugin{}).Enabled(), "hcp is default-on with empty allowlist")

	t.Setenv("ROS_DISABLED_PLUGINS", "hcp")
	config.ResetForTest()
	_ = config.GetConfig()
	assert.False(t, (&HCPPlugin{}).Enabled(), "hcp honors the denylist")
}

func TestHCPPlugin_AfterIngest_NilPoolDegrades(t *testing.T) {
	t.Parallel()

	p := &HCPPlugin{}
	rows := []ingestion.MetricRow{{Namespace: "hc01-infra-hc01", WorkloadName: "etcd"}}
	assert.NoError(t, p.AfterIngest(context.Background(), nil, rows, "org1", "cluster1"),
		"nil pool must degrade to no observation, never fail")
	assert.NoError(t, p.AfterIngest(context.Background(), nil, nil, "org1", "cluster1"))
}
func TestHCPPlugin_noAPIProviderGuard(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	config.ResetForTest()
	_ = config.GetConfig()

	for _, ap := range plugin.APIProviders() {
		assert.NotEqual(t, "hcp", ap.Name(), "hcp must register no routes before #626")
	}
}

// TestObserveRows_CountsHCPOnly pins the pure counting core with adversarial
// values: distinct namespaces/workloads so a name join or ordering coincidence
// cannot green it, plus a blank workload (CSV owner-join gap) that must count
// but never trip.
func TestObserveRows_CountsHCPOnly(t *testing.T) {
	t.Parallel()

	set := map[string]bool{"hc01-infra-hc01": true}
	rows := []ingestion.MetricRow{
		{Namespace: "hc01-infra-hc01", WorkloadName: "etcd"},
		{Namespace: "hc01-infra-hc01", WorkloadName: "kube-apiserver"},
		{Namespace: "hc01-infra-hc01", WorkloadName: ""},
		{Namespace: "hc01-infra-hc01", WorkloadName: "tenant-nginx"},
		{Namespace: "ros-demo", WorkloadName: "tenant-nginx"},
		{Namespace: "openshift-etcd", WorkloadName: "etcd"},
	}

	hcpRows, unknown := observeRows(rows, set)
	assert.Equal(t, 4, hcpRows, "only HCP-namespace rows count, including blank-owner and tenant rows")
	assert.Equal(t, []string{"tenant-nginx"}, unknown, "only the unknown HCP workload trips; blanks skip by design")
}

func TestObserveRows_NilSetExcludesAll(t *testing.T) {
	t.Parallel()

	rows := []ingestion.MetricRow{{Namespace: "hc01-infra-hc01", WorkloadName: "etcd"}}
	hcpRows, unknown := observeRows(rows, nil)
	assert.Equal(t, 0, hcpRows)
	assert.Empty(t, unknown)
}
