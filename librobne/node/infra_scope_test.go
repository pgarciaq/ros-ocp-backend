package node

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsInfraMachineSet(t *testing.T) {
	assert.True(t, isInfraMachineSet("abc123-infra-us-east-1a"), "zone infix form")
	assert.True(t, isInfraMachineSet("abc123-infra"), "suffix form (vSphere-style)")
	assert.False(t, isInfraMachineSet("abc123-worker-a"))
	assert.False(t, isInfraMachineSet("master-0"), "masters never match: no machineset gate added")
	assert.False(t, isInfraMachineSet(""), "empty stays unflagged")
	assert.False(t, isInfraMachineSet("infra-team"), "no leading dash: user namespace, not platform")
	assert.True(t, isInfraMachineSet("my-infra-team"), "documented limitation: infix matches, INFO-only so acceptable")
}

// infraDigestRow builds one digest day carrying a machineset name.
func infraDigestRow(node, ms string, day int, cpu int64) DigestRow {
	r := makeDigestRow(node, day, cpu, cpu, 1024, 1024, 100, 2048, ptr64(4000), ptr64(8192))
	r.MachineSetName = ms
	return r
}

func hasCode(recs []Rec, node string, code int16) bool {
	for _, r := range recsForNode(recs, node) {
		for _, c := range r.NotificationCodes {
			if c == code {
				return true
			}
		}
	}
	return false
}

func TestRecommendNodes_InfraScopeFlag(t *testing.T) {
	var digests []DigestRow
	for d := 1; d <= 10; d++ {
		digests = append(digests, infraDigestRow("infra-0", "abc-infra-a", d, 200))
		digests = append(digests, infraDigestRow("worker-0", "abc-worker-a", d, 200))
		digests = append(digests, infraDigestRow("master-0", "", d, 200))
	}
	recs := RecommendNodes(digests, defaultRecConfig(), defaultThresholdSettings, singleMediumTerm(), topology.TopologyUnknown)
	require.NotEmpty(t, recs)
	assert.True(t, hasCode(recs, "infra-0", types.NotifNodeInfraScope), "infra machineset rec carries 84")
	assert.False(t, hasCode(recs, "worker-0", types.NotifNodeInfraScope), "worker rec untouched")
	assert.False(t, hasCode(recs, "master-0", types.NotifNodeInfraScope), "master rec untouched")
	// Numbers must not move: 84 is framing-only.
	byNode := recsByNodeEngine(recs)
	assert.Equal(t, byNode["worker-0/cost"].RecommendedCPUMC, byNode["infra-0/cost"].RecommendedCPUMC, "identical inputs must size identically regardless of flag")
}
