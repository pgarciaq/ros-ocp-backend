package api_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// Characterization goldens for the HCP capacity serve semantics (#700
// Task 2). These pin the CURRENT handler output on the non-time-derived
// fields so the serve-seam refactor (rules moving to librobne/hcp) can
// be verified behavior-identical: if any of these assertions changes
// after the refactor, the refactor changed behavior. Time-derived fields
// (sample_age_seconds, and staleness for rows seeded relative to now)
// are excluded — the handler calls time.Now() internally (round-9 W2b).
func TestHCPCapacityHandler_Characterization(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := "org-hcp-cap-golden"
	hcpCapCleanup(t, orgID)
	t.Cleanup(func() { hcpCapCleanup(t, orgID) })

	now := time.Now().UTC()
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }

	// One cluster per case: the latest endpoint always serves the newest
	// row of the (filtered) cluster.
	seedHCPCapCluster(t, orgID, testutil.TestClusterUUID) // healthy baseline (not asserted directly)
	seedHCPCapCluster(t, orgID, hcpCapClusterB)           // stale
	seedHCPCapCluster(t, orgID, hcpCapClusterC)           // pressured
	seedHCPCapCluster(t, orgID, hcpCapClusterD)           // incomplete inventory
	seedHCPCapCluster(t, orgID, hcpCapClusterE)           // below-floor
	seedHCPCapCluster(t, orgID, hcpCapClusterF)           // frozen MCE series

	seedHCPCapRow(t, orgID, testutil.TestClusterUUID, at(-30*time.Minute))
	seedHCPCapRow(t, orgID, hcpCapClusterB, at(-13*time.Hour)) // window 12h (60/360 seed) → stale
	seedHCPCapRow(t, orgID, hcpCapClusterC, at(-31*time.Minute))
	hcpCapUpdate(t, orgID, hcpCapClusterC, at(-31*time.Minute), "pressure_state = 'pressured'")
	seedHCPCapRow(t, orgID, hcpCapClusterD, at(-32*time.Minute))
	hcpCapUpdate(t, orgID, hcpCapClusterD, at(-32*time.Minute), "inventory_complete = false")
	seedHCPCapRow(t, orgID, hcpCapClusterE, at(-33*time.Minute))
	hcpCapUpdate(t, orgID, hcpCapClusterE, at(-33*time.Minute), "ocp_version = '4.16.9'")
	// Frozen-MCE series: three rows inside the 12h window (60/360 seed),
	// MCE refs frozen (8/NULL/8/4/2), native inputs moving (eligible cpu
	// varies per row).
	for i, age := range []time.Duration{5 * time.Hour, 2 * time.Hour, 30 * time.Minute} {
		seedHCPCapRow(t, orgID, hcpCapClusterF, at(-age))
		hcpCapUpdate(t, orgID, hcpCapClusterF, at(-age),
			fmt.Sprintf("eligible_worker_cpu_allocatable_millicores = %d", 6400000+i*100))
	}

	e := setupHCPCapEcho(t, nil)
	row := func(t *testing.T, clusterUUID string) map[string]interface{} {
		code, body := hcpCapGET(t, e, orgID, hcpCapBase+"?filter[cluster_uuid]="+clusterUUID)
		require.Equal(t, http.StatusOK, code)
		rows := hcpCapData(t, body)
		require.Len(t, rows, 1)
		return rows[0]
	}
	scenariosOf := func(t *testing.T, r map[string]interface{}) map[string]map[string]interface{} { return hcpCapScenarios(t, r) }
	warningsOf := func(r map[string]interface{}) map[string]interface{} { return r["warnings"].(map[string]interface{}) }
	headroomOf := func(s map[string]map[string]interface{}, name string) interface{} { return s[name]["headroom"] }

	t.Run("healthy baseline pins the scenario shape", func(t *testing.T) {
		r := row(t, testutil.TestClusterUUID)
		s := scenariosOf(t, r)
		assert.Equal(t, float64(7), s["request"]["max_hcs"])
		assert.Equal(t, "memory_request", s["request"]["limiting_resource"])
		assert.Equal(t, float64(3), headroomOf(s, "request"))
		assert.Equal(t, float64(-1), headroomOf(s, "high_qps"), "signed over-capacity")
		assert.Equal(t, "ok", s["request"]["status"])
		w := warningsOf(r)
		assert.False(t, w["stale"].(bool))
		assert.False(t, w["below_version_floor"].(bool))
		assert.False(t, w["pressured"].(bool))
		assert.True(t, w["pressure_unknown"].(bool))
		assert.False(t, w["shared_infrastructure"].(bool))
		assert.Nil(t, r["mce_references"].(map[string]interface{})["low_qps"], "absent MCE ref is null, never zero")
	})

	t.Run("stale row still serves data with the stale warning", func(t *testing.T) {
		r := row(t, hcpCapClusterB)
		assert.True(t, warningsOf(r)["stale"].(bool))
		assert.Equal(t, float64(7), scenariosOf(t, r)["request"]["max_hcs"], "warning never suppresses scenarios")
	})

	t.Run("pressured withholds headroom on every scenario", func(t *testing.T) {
		r := row(t, hcpCapClusterC)
		s := scenariosOf(t, r)
		for _, name := range []string{"request", "low_qps", "average_qps", "medium_qps", "high_qps"} {
			assert.Nil(t, headroomOf(s, name), "%s headroom withheld", name)
		}
		assert.True(t, warningsOf(r)["pressured"].(bool))
		assert.Equal(t, float64(7), s["request"]["max_hcs"], "maxima still serve")
	})

	t.Run("incomplete inventory withholds headroom, keeps estimates", func(t *testing.T) {
		r := row(t, hcpCapClusterD)
		s := scenariosOf(t, r)
		for _, name := range []string{"request", "low_qps", "average_qps", "medium_qps", "high_qps"} {
			assert.Nil(t, headroomOf(s, name), "%s headroom withheld", name)
		}
		assert.Equal(t, float64(7), s["request"]["max_hcs"], "estimates still serve")
		assert.Equal(t, "memory_request", s["request"]["limiting_resource"])
	})

	t.Run("below-floor version warns without suppression", func(t *testing.T) {
		r := row(t, hcpCapClusterE)
		assert.True(t, warningsOf(r)["below_version_floor"].(bool))
		assert.Equal(t, float64(7), scenariosOf(t, r)["request"]["max_hcs"])
		assert.Equal(t, float64(3), headroomOf(scenariosOf(t, r), "request"))
	})

	t.Run("frozen MCE refs with moving native inputs caution", func(t *testing.T) {
		r := row(t, hcpCapClusterF)
		w := warningsOf(r)
		require.NotNil(t, w["mce_may_be_stale"])
		assert.Equal(t, true, w["mce_may_be_stale"].(bool))
		mce := r["mce_references"].(map[string]interface{})
		assert.Equal(t, float64(8), mce["request"])
	})
}
