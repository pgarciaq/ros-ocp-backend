package csv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Live HCP namespaces run request-less (best-effort control-plane pods):
// empty request cells must parse as zero with usage preserved. Previously
// every such row was skipped, and a file of only request-less rows failed
// wholesale — zero namespace digests for the whole HCP namespace.
func TestParseNamespaceRows_RequestlessRowsParseAsZero(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"namespace,workload,workload_type,container_name,pod,interval_start,interval_end,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg",
		"hc01-infra-hc01,,ReplicaSet,,cluster-image-registry-operator-56bc688cd4-5fp6n,2026-09-15 00:00:01 +0000 UTC,2026-09-15 00:15:00 +0000 UTC,,0.000310,,22574641",
		"hc01-infra-hc01,etcd,StatefulSet,etcd,etcd-0,2026-09-15 00:00:01 +0000 UTC,2026-09-15 00:15:00 +0000 UTC,0.04,0.0002,209715200,22574641",
	}, "\n")
	rows, skipped, err := ParseNamespaceRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Len(t, rows, 2, "request-less rows must parse, not skip")
	assert.Zero(t, skipped)
	assert.Equal(t, int64(0), rows[0].CPURequestMC, "empty request cells become zero")
	assert.Equal(t, int64(0), rows[0].MemRequestKiB)
	assert.Greater(t, rows[0].MemUsageKiB, int64(0), "usage survives alongside zeroed requests")
	assert.Equal(t, "hc01-infra-hc01", rows[0].Namespace)
	assert.Equal(t, int64(40), rows[1].CPURequestMC, "present requests keep parsing")
}
