package csv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Request-less containers are legitimate workloads (live case: ACM agent
// addons ship usage but no CPU request). Empty request cells are the
// unset-request convention (same as the namespace parser); a request-less
// row must parse as zero with usage preserved, not skip the row.
//
// Adversarial shape: paired rows identical except the request cells and a
// distinguishing usage value, so pre-fix the ONLY survival discriminator is
// the emptiness of the request cell — a same-value collision can't mask the
// drop as ordering or field-parsing luck.
func requestlessFixture(cpuRequest, memRequest string) string {
	return strings.Join([]string{
		"namespace,workload,workload_type,container_name,pod,interval_start,interval_end,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg",
		strings.Join([]string{
			"open-cluster-management-agent-addon", "klusterlet-addon-work", "ReplicaSet", "work",
			"work-0", "2026-10-20 00:00:00 +0000 UTC", "2026-10-20 01:00:00 +0000 UTC",
			cpuRequest, "0.05", memRequest, "52428800",
		}, ","),
	}, "\n")
}

func TestParseRows_RequestlessRowsParseAsZero(t *testing.T) {
	t.Parallel()
	t.Run("both requests empty", func(t *testing.T) {
		csvBody := requestlessFixture("", "")
		rows, skipped, err := ParseRows(strings.NewReader(csvBody))
		require.NoError(t, err)
		require.Len(t, rows, 1, "request-less row must parse, not skip (pre-fix: row dropped by unguarded empty-request parse)")
		assert.Zero(t, skipped, "skip counter must not absorb a request-less row (pre-fix: skipped=1)")
		assert.Equal(t, int64(0), rows[0].CPURequestMC, "empty CPU request is the unset sentinel (zero), not an error")
		assert.Equal(t, int64(0), rows[0].MemRequestKiB, "empty memory request is the unset sentinel (zero), not an error")
		assert.Equal(t, int64(50), rows[0].CPUUsageMC, "usage must survive alongside zeroed requests (pre-fix: nothing survives)")
		assert.Greater(t, rows[0].MemUsageKiB, int64(0), "memory usage must survive alongside zeroed requests")
	})

	// Independence: each guard must hold on its own field. The live
	// partial case is exactly this shape — memory-only requests.
	t.Run("cpu request empty, memory request set", func(t *testing.T) {
		csvBody := requestlessFixture("", "104857600")
		rows, skipped, err := ParseRows(strings.NewReader(csvBody))
		require.NoError(t, err)
		require.Len(t, rows, 1, "partial request-less row (CPU unset) must parse, not skip")
		assert.Zero(t, skipped)
		assert.Equal(t, int64(0), rows[0].CPURequestMC)
		assert.Equal(t, int64(102400), rows[0].MemRequestKiB, "present memory request keeps its value")
		assert.Equal(t, int64(50), rows[0].CPUUsageMC)
	})

	t.Run("memory request empty, cpu request set", func(t *testing.T) {
		csvBody := requestlessFixture("0.1", "")
		rows, skipped, err := ParseRows(strings.NewReader(csvBody))
		require.NoError(t, err)
		require.Len(t, rows, 1, "partial request-less row (memory unset) must parse, not skip")
		assert.Zero(t, skipped)
		assert.Equal(t, int64(100), rows[0].CPURequestMC, "present CPU request keeps its value")
		assert.Equal(t, int64(0), rows[0].MemRequestKiB)
	})

	// The fix must not weaken validation: a malformed NON-empty request
	// cell still errors and skips the row, as before.
	t.Run("malformed non-empty request still skipped", func(t *testing.T) {
		csvBody := requestlessFixture("abc", "")
		_, skipped, err := ParseRows(strings.NewReader(csvBody))
		require.NoError(t, err)
		assert.Equal(t, 1, skipped, "malformed non-empty request values must still skip the row")
	})
}
