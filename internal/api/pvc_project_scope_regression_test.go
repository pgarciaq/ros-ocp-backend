package api_test

// #873: PVCs are namespace-scoped resources — the same grain as containers,
// whose list already narrows by openshift.project grants. The PVC list must
// do the same: a project-restricted caller sees only their namespaces' PVCs,
// on every serving path (flat list, group-by, CSV), composing with — never
// replaced by — client filters and the cluster scope (#752's composition
// rule). node-utilization and GPU MIG are node-grain and stay unscoped by
// project (owner decision on #873, extending #789's grain split).

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func dataPVCNamespaces(t *testing.T, resp map[string]interface{}) []string {
	t.Helper()
	data, ok := resp["data"].([]interface{})
	require.True(t, ok, "data must be an array")
	ns := make([]string, 0, len(data))
	for _, row := range data {
		ns = append(ns, row.(map[string]interface{})["namespace"].(string))
	}
	return ns
}

// seedReaderScopePVCSecondNamespace adds one PVC row in a namespace the
// project-restricted caller must NOT see (the base fixture's rows all live
// in ns-rs).
func seedReaderScopePVCSecondNamespace(t *testing.T, ctx context.Context) {
	t.Helper()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `INSERT INTO pvc_recommendation_sets
		(org_id, cluster_uuid, namespace, persistentvolumeclaim, capacity_bytes, estimated_savings_cents)
		VALUES ($1, $2, 'ns-other', 'pvc-hidden', 555, 55)
		ON CONFLICT DO NOTHING`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)
}

func readerPVCProjectPerms() map[string][]string {
	return map[string][]string{
		"openshift.cluster": {"*"},
		"openshift.project": {"ns-rs"},
	}
}

func TestReaderScope_PVCListProjectGrants(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	seedReaderScopeOrg(t, ctx)
	seedReaderScopePVCSecondNamespace(t, ctx)

	// Project-restricted: sees only ns-rs rows, never ns-other — on the
	// flat list (pre-fix: all namespaces' rows served).
	resp := readerScopeJSON(t, readerPVCProjectPerms(),
		"/api/cost-management/v1/recommendations/openshift/pvcs")
	for _, ns := range dataPVCNamespaces(t, resp) {
		assert.Equal(t, "ns-rs", ns,
			"a project-restricted caller must see only their namespace's PVC rows (#873)")
	}

	// Group by project: no ns-other group for the restricted caller.
	resp = readerScopeJSON(t, readerPVCProjectPerms(),
		"/api/cost-management/v1/recommendations/openshift/pvcs?group_by[project]=*")
	groups, _ := resp["data"].([]interface{})
	for _, g := range groups {
		assert.NotContains(t, g, "ns-other",
			"a project-restricted caller must not enumerate ns-other's PVC group")
	}

	// CSV: no ns-other rows.
	_, body := readerScopeGET(t, readerPVCProjectPerms(),
		"/api/cost-management/v1/recommendations/openshift/pvcs?format=csv")
	assert.NotContains(t, body, "ns-other",
		"a project-restricted caller's CSV export must not contain other namespaces' rows")

	// Client filter composes: restricted caller filtering FOR ns-other gets
	// empty, not ns-other's rows (a client filter is not authorization).
	resp = readerScopeJSON(t, readerPVCProjectPerms(),
		"/api/cost-management/v1/recommendations/openshift/pvcs?filter[project]=ns-other")
	meta := resp["meta"].(map[string]interface{})
	assert.Equal(t, float64(0), meta["count"],
		"a project-restricted caller filtering for another namespace must get empty (#873)")

	// Cluster scope still composes: restricted to ns-rs AND cluster A —
	// only cluster A's ns-rs row.
	resp = readerScopeJSON(t, map[string][]string{
		"openshift.cluster": {testutil.TestClusterUUID},
		"openshift.project": {"ns-rs"},
	}, "/api/cost-management/v1/recommendations/openshift/pvcs")
	uuids := dataClusterUUIDs(t, resp)
	assert.Equal(t, []string{testutil.TestClusterUUID}, uuids,
		"project and cluster scopes compose (#752 + #873)")

	// Positive controls: project wildcard sees both namespaces; absent
	// project grants mean unscoped (the org-admin shape).
	resp = readerScopeJSON(t, map[string][]string{"openshift.project": {"*"}},
		"/api/cost-management/v1/recommendations/openshift/pvcs")
	assert.Len(t, dataPVCNamespaces(t, resp), 3,
		"project wildcard sees all three seeded PVC rows")
	resp = readerScopeJSON(t, map[string][]string{"openshift.cluster": {"*"}},
		"/api/cost-management/v1/recommendations/openshift/pvcs")
	assert.Len(t, dataPVCNamespaces(t, resp), 3,
		"absent project grants mean unscoped")

	// Empty project grant set denies via ANY('{}') matching nothing.
	resp = readerScopeJSON(t, map[string][]string{"openshift.project": {}},
		"/api/cost-management/v1/recommendations/openshift/pvcs")
	meta = resp["meta"].(map[string]interface{})
	assert.Equal(t, float64(0), meta["count"],
		"an empty project grant set denies")
}
