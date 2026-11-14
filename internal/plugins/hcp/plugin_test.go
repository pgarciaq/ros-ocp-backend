package hcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

func TestHCPPlugin_traitAssertions(t *testing.T) {
	t.Parallel()

	var (
		_ plugin.Plugin      = (*HCPPlugin)(nil)
		_ plugin.IngestHook  = (*HCPPlugin)(nil)
		_ plugin.APIProvider = (*HCPPlugin)(nil)
		_ plugin.CSVIngestor = (*HCPPlugin)(nil)
	)

	// #638 owns the routes; the W4 capacity CSV (#392) is claimed here.
	p := &HCPPlugin{}

	// #793: the HCP plugin is term-less by design (ADR-0346) — the
	// scaffold's stray TermProvider was removed. The negative assertion
	// pins it: re-adding the trait would re-open the inert knob.
	assert.NotImplements(t, (*plugin.TermProvider)(nil), p,
		"hcp must not implement TermProvider — term-less by design (ADR-0346, #793)")
	assert.Equal(t, []string{"hcp-capacity"}, p.SupportedCSVTypes(),
		"hcp claims exactly the W4 fleet-capacity snapshot type")
	_, ok := any(p).(plugin.RetentionProvider)
	assert.False(t, ok, "hcp owns no RetentionProvider: the core history sweep owns hcp_capacity_history (#701)")
}

func TestHCPPlugin_identity(t *testing.T) {
	t.Parallel()

	p := &HCPPlugin{}
	assert.Equal(t, "hcp", p.Name())
	assert.Equal(t, []string{"container"}, p.Requires())
	assert.Equal(t, []string{"container"}, p.HookAfterCSVTypes())
	// #793: the term-table assertions (MaxWindowDays 90, DefaultTerms
	// 1/7/15) were removed with the trait — the plugin is term-less by
	// design (ADR-0346); the negative NotImplements assertion in
	// TestHCPPlugin_traitAssertions pins it.
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
func TestHCPPlugin_RegistersRoutes(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	config.ResetForTest()
	_ = config.GetConfig()

	// #630's no-route guard retired in #638: the plugin now owns its surface.
	var found bool
	for _, ap := range plugin.APIProviders() {
		if ap.Name() == "hcp" {
			found = true
		}
	}
	assert.True(t, found, "hcp must register API routes once #638 lands")

	var _ plugin.APIProvider = (*HCPPlugin)(nil)
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
