package node

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsMasterNodeRole(t *testing.T) {
	assert.True(t, isMasterNodeRole("master"), "legacy label")
	assert.True(t, isMasterNodeRole("control-plane"), "current label; merge survivors vary")
	assert.False(t, isMasterNodeRole("worker"))
	assert.False(t, isMasterNodeRole("infra"))
	assert.False(t, isMasterNodeRole(""), "empty never matches")
	assert.False(t, isMasterNodeRole("Master"), "case-sensitive: Prometheus labels are lowercase")
}

// masterDigestRow builds one digest day carrying node role.
func masterDigestRow(node, role string, day int, cpu int64) DigestRow {
	r := makeDigestRow(node, day, cpu, cpu, 1024, 1024, 100, 2048, ptr64(4000), ptr64(8192))
	r.NodeRole = role
	return r
}

func TestRecommendNodes_MasterScopeFlag(t *testing.T) {
	var digests []DigestRow
	for _, n := range []string{"master-0", "master-1", "master-2"} {
		for d := 1; d <= 10; d++ {
			digests = append(digests, masterDigestRow(n, "master", d, 200))
		}
	}
	for d := 1; d <= 10; d++ {
		digests = append(digests, masterDigestRow("worker-0", "worker", d, 200))
		digests = append(digests, masterDigestRow("infra-0", "infra", d, 200))
	}
	recs := RecommendNodes(digests, defaultRecConfig(), defaultThresholdSettings, singleMediumTerm(), topology.TopologyUnknown)
	require.NotEmpty(t, recs)
	assert.True(t, hasCode(recs, "master-0", types.NotifNodeCPScope), "3-master cluster flags masters")
	assert.True(t, hasCode(recs, "master-1", types.NotifNodeCPScope))
	assert.True(t, hasCode(recs, "master-2", types.NotifNodeCPScope))
	assert.False(t, hasCode(recs, "worker-0", types.NotifNodeCPScope), "worker untouched")
	assert.False(t, hasCode(recs, "infra-0", types.NotifNodeCPScope), "infra untouched (84 is its own code)")
}

func TestRecommendNodes_MasterScopeSilentSingle(t *testing.T) {
	var digests []DigestRow
	for d := 1; d <= 10; d++ {
		digests = append(digests, masterDigestRow("sno-0", "master", d, 200))
	}
	recs := RecommendNodes(digests, defaultRecConfig(), defaultThresholdSettings, singleMediumTerm(), topology.TopologyUnknown)
	require.NotEmpty(t, recs, "single node still gets recs")
	assert.False(t, hasCode(recs, "sno-0", types.NotifNodeCPScope), "single master (SNO shape) stays silent by quorum-count gate")
}

func TestRecommendNodes_MasterScopeControlPlaneLabel(t *testing.T) {
	var digests []DigestRow
	for _, n := range []string{"cp-0", "cp-1", "cp-2"} {
		for d := 1; d <= 10; d++ {
			digests = append(digests, masterDigestRow(n, "control-plane", d, 200))
		}
	}
	recs := RecommendNodes(digests, defaultRecConfig(), defaultThresholdSettings, singleMediumTerm(), topology.TopologyUnknown)
	require.NotEmpty(t, recs)
	assert.True(t, hasCode(recs, "cp-0", types.NotifNodeCPScope), "control-plane label variant flags identically")
}
