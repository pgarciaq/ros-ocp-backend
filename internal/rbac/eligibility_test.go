package rbac

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// TestOpenShiftDataAccessEligible pins #753's core semantic, mirroring Koku's
// permission lattice (koku/koku/rbac.py): resources start EMPTY — an absent
// OpenShift grant is NOT unrestricted access. Each case's assertion names the
// pre-fix behavior it rules out.
func TestOpenShiftDataAccessEligible(t *testing.T) {
	t.Setenv("RBAC_ENABLE", "true")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest) // rebuild config from restored env — no RBACEnabled leak into later tests

	cases := []struct {
		name    string
		perms   map[string][]string
		want    bool
		ruleOut string
	}{
		{
			name:    "settings read only",
			perms:   map[string][]string{"settings.read": {"*"}},
			want:    false,
			ruleOut: "a settings-read-only role must not read infrastructure data (pre-fix: absent openshift grant treated as unrestricted)",
		},
		{
			name:    "settings write only",
			perms:   map[string][]string{"settings.write": {"*"}},
			want:    false,
			ruleOut: "a settings-write-only role must not read infrastructure data",
		},
		{
			name:    "settings both",
			perms:   map[string][]string{"settings.read": {"*"}, "settings.write": {"*"}},
			want:    false,
			ruleOut: "combined settings roles hold no data-read grant",
		},
		{
			name:    "empty permissions",
			perms:   map[string][]string{},
			want:    false,
			ruleOut: "no permissions at all means no data access",
		},
		{
			name:    "global wildcard",
			perms:   map[string][]string{"*": {}},
			want:    true,
			ruleOut: "the global wildcard stays unrestricted",
		},
		{
			name:    "cluster grant",
			perms:   map[string][]string{"openshift.cluster": {"11111111-1111-1111-1111-111111111111"}},
			want:    true,
			ruleOut: "an affirmative cluster grant is data-read eligible",
		},
		{
			name:    "node only grant",
			perms:   map[string][]string{"openshift.node": {"node-a"}},
			want:    true,
			ruleOut: "a node-level affirmative grant is eligible (helpers scope it)",
		},
		{
			name:    "project only grant",
			perms:   map[string][]string{"openshift.project": {"ns-a"}},
			want:    true,
			ruleOut: "a project-level affirmative grant is eligible (helpers scope it)",
		},
		{
			name:    "explicit empty cluster list",
			perms:   map[string][]string{"openshift.cluster": {}},
			want:    false,
			ruleOut: "an explicitly EMPTY grant is not eligibility (Koku: empty is no access; builders ignore other keys' grants, so empty must not authorize)",
		},
		{
			name:    "explicit empty node list",
			perms:   map[string][]string{"openshift.node": {}},
			want:    false,
			ruleOut: "an empty node grant must not authorize container/namespace reads (pre-fix: key presence alone granted eligibility)",
		},
		{
			name:    "explicit empty project list",
			perms:   map[string][]string{"openshift.project": {}},
			want:    false,
			ruleOut: "an empty project grant must not authorize reads",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, OpenShiftDataAccessEligible(tc.perms), tc.ruleOut)
		})
	}

	// RBAC disabled: everything stays open (dev/test mode).
	t.Run("rbac disabled", func(t *testing.T) {
		t.Setenv("RBAC_ENABLE", "false")
		config.ResetForTest()
		assert.True(t, OpenShiftDataAccessEligible(map[string][]string{}),
			"RBAC disabled: absent grants do not restrict (dev/test mode)")
	})
}
