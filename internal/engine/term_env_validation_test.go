package engine_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/container"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/gpu"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/namespace"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/node"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/pvc"
)

// #792: startup must refuse admin term configuration it cannot honour.
// Each error names the variable, its value, and the valid range (the
// acceptance criteria), so an operator can fix the deployment without
// digging through source.

func TestValidateTermEnvConfig_AcceptsCleanEnv(t *testing.T) {
	assert.NoError(t, engine.ValidateTermEnvConfig())
}

func TestValidateTermEnvConfig_AcceptsValidValues(t *testing.T) {
	t.Setenv("ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS", "45")
	t.Setenv("ROS_TERMS_CONTAINER_LONG_MIN_DATA_DAYS", "20")
	t.Setenv("ROS_TERMS_CONTAINER_LONG_DECAY_HALFLIFE_HOURS", "500")
	t.Setenv("ROS_TERMS_PVC_SHORT_WINDOW_DAYS", "14")
	t.Setenv("ROS_TERMS_NODE_MEDIUM_WINDOW_DAYS", "21")
	// The operative VM spelling — the resolver reads the ord names
	// (short/medium/long), not the VM plugin's DefaultTerms display names
	// (#792 review P1: this var must stay valid).
	t.Setenv("ROS_TERMS_VM_SHORT_WINDOW_DAYS", "21")
	assert.NoError(t, engine.ValidateTermEnvConfig())
}

// TestValidateTermEnvConfig_ReadsValuesLikeTheResolver pins the P2 fix: the
// validator must apply the resolver's read semantics (config.EnvString —
// trimmed, empty treated as unset), not the raw os.Environ value. A blank
// variable (the standard compose/K8s way to unset one) must not block
// startup, and whitespace-padded values the resolver applies must be
// accepted.
func TestValidateTermEnvConfig_ReadsValuesLikeTheResolver(t *testing.T) {
	t.Run("empty value is unset", func(t *testing.T) {
		t.Setenv("ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS", "")
		assert.NoError(t, engine.ValidateTermEnvConfig())
	})
	t.Run("whitespace is trimmed like EnvString", func(t *testing.T) {
		t.Setenv("ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS", " 45 ")
		assert.NoError(t, engine.ValidateTermEnvConfig())
	})
}

func TestValidateTermEnvConfig_RejectsOutOfRangeWindow(t *testing.T) {
	t.Setenv("ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS", "120")
	err := engine.ValidateTermEnvConfig()
	require.Error(t, err)
	msg := err.Error()
	for _, want := range []string{
		"ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS",
		"\"120\"",
		"container",
		"long",
		"between 1 and 90",
	} {
		assert.Contains(t, msg, want, "the startup error must name plugin, term, variable, value, and range")
	}
}

func TestValidateTermEnvConfig_RejectsUnparseableMinDataDays(t *testing.T) {
	t.Setenv("ROS_TERMS_CONTAINER_LONG_MIN_DATA_DAYS", "abc")
	err := engine.ValidateTermEnvConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ROS_TERMS_CONTAINER_LONG_MIN_DATA_DAYS")
	assert.Contains(t, err.Error(), "\"abc\"")
	assert.Contains(t, err.Error(), "integer >= 1")
}

func TestValidateTermEnvConfig_RejectsNegativeDecay(t *testing.T) {
	t.Setenv("ROS_TERMS_CONTAINER_LONG_DECAY_HALFLIFE_HOURS", "-1")
	err := engine.ValidateTermEnvConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ROS_TERMS_CONTAINER_LONG_DECAY_HALFLIFE_HOURS")
	assert.Contains(t, err.Error(), "\"-1\"")
	assert.Contains(t, err.Error(), "number >= 0")
}

// TestValidateTermEnvConfig_RejectsUnknownVariables pins the owner decision
// that vars for term-less or unknown plugins are REMOVED: hcp lost its terms
// (#793), quota/cluster-quota/snapshot never had any, vm was never a term
// provider (its vm/terms route is DB-backed) — and the unknown-name cases
// catch the typo class that is #792's failure story.
func TestValidateTermEnvConfig_RejectsUnknownVariables(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
	}{
		{"term-less plugin (hcp, terms removed by #793)", "ROS_TERMS_HCP_SHORT_WINDOW_DAYS"},
		// The VM plugin IS term-bearing, but the resolver reads the ord
		// names (short/medium/long) — the DefaultTerms spelling
		// (short_term) has never been read by anything. It was even
		// documented as an example in configurability.md (#792 review P1).
		{"dead spelling for a term-bearing plugin (vm)", "ROS_TERMS_VM_SHORT_TERM_WINDOW_DAYS"},
		{"unknown term for a term-bearing plugin", "ROS_TERMS_CONTAINER_EXTRA_WINDOW_DAYS"},
		{"unknown field for a known term", "ROS_TERMS_CONTAINER_LONG_FOO"},
		{"typo'd plugin name", "ROS_TERMS_CONTAINERS_LONG_WINDOW_DAYS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.env, "7")
			err := engine.ValidateTermEnvConfig()
			require.Error(t, err, "%s must be rejected at startup", tc.env)
			assert.Contains(t, err.Error(), tc.env)
			assert.Contains(t, err.Error(), "unknown variable")
			assert.Contains(t, err.Error(), "term-bearing plugins")
			assert.True(t, strings.Contains(err.Error(), "container"),
				"the error should list the term-bearing plugins so the operator can correct the name")
		})
	}
}
